// Command restic-reporter runs configured restic backup jobs and publishes
// per-job metrics to MQTT using Home Assistant discovery. It is a drop-in
// replacement for gaming-server/scripts/restic-backup.sh: same repos, same
// topics, same Home Assistant entities, but a single static binary with no
// jq/mosquitto-clients dependency on the host.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/andrew-avinante/restic-reporter/internal/config"
	"github.com/andrew-avinante/restic-reporter/internal/mqtt"
	"github.com/andrew-avinante/restic-reporter/internal/restic"
)

func main() {
	configPath := flag.String("config", "/etc/restic-reporter/config.yaml", "path to config file")
	flag.Parse()

	if err := run(*configPath); err != nil {
		log.Fatalf("restic-reporter: %v", err)
	}
}

func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	logw, closeLog, err := openLog(cfg.LogFile)
	if err != nil {
		return err
	}
	defer closeLog()

	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	fmt.Fprintf(logw, "=== Backup started: %s ===\n", time.Now().Format(time.RFC1123))

	// MQTT is telemetry only: a broker that is down must never prevent or
	// fail a backup. Connect best-effort; if it fails, pub is nil and jobs
	// still run, matching the original script's independent, non-fatal
	// mosquitto_pub calls.
	pub, err := mqtt.Connect(mqtt.Config{
		Host:            cfg.MQTT.Host,
		Port:            cfg.MQTT.Port,
		Username:        cfg.MQTT.Username,
		Password:        cfg.MQTT.Password,
		DiscoveryPrefix: cfg.MQTT.DiscoveryPrefix,
		TopicPrefix:     cfg.MQTT.TopicPrefix,
	})
	if err != nil {
		fmt.Fprintf(logw, "WARNING: MQTT connect failed, backups will run without reporting: %v\n", err)
		pub = nil
	} else {
		defer pub.Close()
	}

	runner := &restic.Runner{
		Binary:       cfg.Restic.Binary,
		PasswordFile: cfg.Restic.PasswordFile,
		Log:          logw,
	}

	// Interrupts cancel the in-flight restic process cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The exit code follows restic outcomes only; reporting failures are
	// logged but never change it (a failed publish must not look like a failed
	// backup).
	allOK := true
	for _, job := range cfg.Jobs {
		if ok := runJob(ctx, cfg, runner, pub, logw, job); !ok {
			allOK = false
		}
	}

	fmt.Fprintf(logw, "=== Backup finished: %s ===\n", time.Now().Format(time.RFC1123))

	if !allOK {
		return fmt.Errorf("one or more backup jobs failed")
	}
	return nil
}

// runJob runs one backup, updates its last-success marker, and publishes the HA
// discovery configs and resulting state (best-effort). It returns true only
// when the restic backup itself succeeded; reporting failures are logged but do
// not affect the result.
func runJob(ctx context.Context, cfg *config.Config, runner *restic.Runner, pub *mqtt.Publisher, logw io.Writer, job config.Job) bool {
	fmt.Fprintf(logw, "Backing up %s...\n", job.Name)

	res, err := runner.Backup(ctx, job.Repo, job.Source)
	if err != nil {
		// Failed to even start restic. Still try to report a failed state.
		res = &restic.Result{Success: false, ExitCode: 1, ErrorMsg: err.Error()}
	}

	nowISO := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	successFile := filepath.Join(cfg.StateDir, job.ID+"_last_success")

	status := "failed"
	if res.Success {
		status = "success"
		if werr := os.WriteFile(successFile, []byte(nowISO+"\n"), 0o644); werr != nil {
			fmt.Fprintf(logw, "WARNING: could not write last-success file for %q: %v\n", job.ID, werr)
		}
	}

	lastSuccess := readLastSuccess(successFile)

	state := mqtt.State{
		Status:              status,
		Timestamp:           nowISO,
		DurationSeconds:     int64(res.Duration.Round(time.Second).Seconds()),
		FilesNew:            res.Summary.FilesNew,
		FilesChanged:        res.Summary.FilesChanged,
		FilesUnmodified:     res.Summary.FilesUnmodified,
		DataAddedBytes:      res.Summary.DataAdded,
		TotalFilesProcessed: res.Summary.TotalFilesProcessed,
		TotalBytesProcessed: res.Summary.TotalBytesProcessed,
		SnapshotID:          res.Summary.SnapshotID,
		LastSuccess:         lastSuccess,
		Error:               res.ErrorMsg,
	}

	// Reporting is best-effort: log failures, never let them affect the
	// job's success. Skip entirely if the broker was unreachable at startup.
	if pub != nil {
		if derr := pub.PublishDiscovery(job.ID, job.Name); derr != nil {
			fmt.Fprintf(logw, "WARNING: publish discovery for %q failed: %v\n", job.ID, derr)
		}
		if serr := pub.PublishState(job.ID, state); serr != nil {
			fmt.Fprintf(logw, "WARNING: publish state for %q failed: %v\n", job.ID, serr)
		}
	}

	if !res.Success {
		fmt.Fprintf(logw, "ERROR: backup of %q failed (restic exit %d): %s\n", job.ID, res.ExitCode, res.ErrorMsg)
	}
	return res.Success
}

// readLastSuccess returns the stored ISO timestamp, or "" if none.
func readLastSuccess(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(trimTrailingNewline(data))
}

func trimTrailingNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

// openLog returns a writer that appends to path (mirrored to stderr). When path
// is empty it returns stderr alone.
func openLog(path string) (io.Writer, func(), error) {
	if path == "" {
		return os.Stderr, func() {}, nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}
	w := io.MultiWriter(f, os.Stderr)
	return w, func() { f.Close() }, nil
}
