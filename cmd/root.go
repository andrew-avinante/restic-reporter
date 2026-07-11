// Package cmd wires up the restic-reporter command-line interface with cobra.
// The root command runs the configured backup jobs; subcommands provide
// version reporting and config validation.
package cmd

import (
	"context"

	"github.com/spf13/cobra"
)

// defaultConfigPath matches the path baked into deploy/restic-reporter.service.
const defaultConfigPath = "/etc/restic-reporter/config.yaml"

// configPath is populated by the persistent --config flag and shared by all
// subcommands.
var configPath string

// rootCmd runs the backup jobs when invoked with no subcommand, preserving the
// original `restic-reporter --config ...` invocation used by the systemd unit.
var rootCmd = &cobra.Command{
	Use:   "restic-reporter",
	Short: "Run restic backup jobs and publish metrics to Home Assistant via MQTT",
	Long: `restic-reporter runs configured restic backup jobs and publishes per-job
metrics to MQTT using Home Assistant discovery.

Invoked with no subcommand it runs every configured job, exiting non-zero if
any backup fails. MQTT reporting is best-effort and never affects the exit
code.`,
	// Silence cobra's own error/usage dump on RunE failures; main handles
	// exit codes and we log our own diagnostics.
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBackups(cmd.Context(), configPath)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", defaultConfigPath, "path to config file")
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(validateCmd)
}

// Execute runs the root command. main passes a cancellable context so an
// in-flight restic process is interrupted cleanly on SIGINT/SIGTERM.
func Execute(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}
