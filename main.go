// Command restic-reporter runs configured restic backup jobs and publishes
// per-job metrics to MQTT using Home Assistant discovery. It is a drop-in
// replacement for gaming-server/scripts/restic-backup.sh: same repos, same
// topics, same Home Assistant entities, but a single static binary with no
// jq/mosquitto-clients dependency on the host.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/andrew-avinante/restic-reporter/cmd"
)

func main() {
	// Interrupts cancel the in-flight restic process cleanly; the context
	// flows through cobra to the backup runner.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cmd.Execute(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "restic-reporter: %v\n", err)
		os.Exit(1)
	}
}
