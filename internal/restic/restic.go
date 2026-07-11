// Package restic runs `restic backup --json` and extracts the summary metrics
// that Home Assistant sensors report. restic-reporter shells out to the restic
// binary rather than importing restic as a library: restic keeps its
// functionality in internal/ packages that external modules cannot import, so
// the binary + --json output is the only stable integration surface.
package restic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// Summary mirrors the fields of restic's message_type=="summary" JSON object
// that we surface to Home Assistant. Unknown fields are ignored.
type Summary struct {
	FilesNew            int64  `json:"files_new"`
	FilesChanged        int64  `json:"files_changed"`
	FilesUnmodified     int64  `json:"files_unmodified"`
	DataAdded           int64  `json:"data_added"`
	TotalFilesProcessed int64  `json:"total_files_processed"`
	TotalBytesProcessed int64  `json:"total_bytes_processed"`
	SnapshotID          string `json:"snapshot_id"`
}

// Result is the outcome of a single backup run.
type Result struct {
	Summary  Summary
	Duration time.Duration
	ExitCode int
	Success  bool
	// ErrorMsg holds the tail of restic output when the run failed.
	ErrorMsg string
}

// Runner executes restic backups.
type Runner struct {
	Binary       string
	PasswordFile string
	// Log, if non-nil, receives all restic stdout/stderr for diagnostics.
	Log io.Writer
}

// Backup snapshots source into repo and returns the parsed summary metrics.
// It always returns a Result (even on failure); a non-nil error indicates a
// problem starting the process, not a non-zero restic exit code.
func (r *Runner) Backup(ctx context.Context, repo, source string) (*Result, error) {
	args := []string{"-r", repo, "--password-file", r.PasswordFile, "backup", source, "--json"}
	cmd := exec.CommandContext(ctx, r.Binary, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	// Capture stderr so a failed run can report a useful tail. Also mirror it
	// to the log if configured.
	var stderr bytes.Buffer
	if r.Log != nil {
		cmd.Stderr = io.MultiWriter(&stderr, r.Log)
	} else {
		cmd.Stderr = &stderr
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start restic: %w", err)
	}

	summary, parseErr := r.scanSummary(stdout)
	waitErr := cmd.Wait()
	duration := time.Since(start)

	res := &Result{Duration: duration}
	if waitErr == nil {
		res.Success = true
		res.ExitCode = 0
		if summary != nil {
			res.Summary = *summary
		} else if parseErr != nil {
			// restic succeeded but we could not read its summary; surface it.
			res.ErrorMsg = "backup succeeded but summary was not parsed: " + parseErr.Error()
		}
		return res, nil
	}

	res.Success = false
	res.ExitCode = exitCode(waitErr)
	res.ErrorMsg = errorTail(&stderr)
	return res, nil
}

// scanSummary streams restic's newline-delimited JSON, tees every line to the
// log, and returns the last summary object seen.
func (r *Runner) scanSummary(stdout io.Reader) (*Summary, error) {
	var last *Summary
	sc := bufio.NewScanner(stdout)
	// restic status lines are small, but raise the limit to be safe.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if r.Log != nil {
			r.Log.Write(append(append([]byte{}, line...), '\n'))
		}
		var probe struct {
			MessageType string `json:"message_type"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			continue // non-JSON line, ignore
		}
		if probe.MessageType != "summary" {
			continue
		}
		var s Summary
		if err := json.Unmarshal(line, &s); err != nil {
			continue
		}
		s2 := s
		last = &s2
	}
	if err := sc.Err(); err != nil {
		return last, err
	}
	return last, nil
}

func errorTail(stderr *bytes.Buffer) string {
	lines := strings.Split(strings.TrimRight(stderr.String(), "\n"), "\n")
	if len(lines) > 5 {
		lines = lines[len(lines)-5:]
	}
	return strings.TrimSpace(strings.Join(lines, " "))
}

func exitCode(err error) int {
	if e, ok := err.(*exec.ExitError); ok {
		return e.ExitCode()
	}
	return 1
}
