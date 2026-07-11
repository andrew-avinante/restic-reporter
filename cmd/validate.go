package cmd

import (
	"fmt"

	"github.com/andrew-avinante/restic-reporter/internal/config"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Load and validate the config file without running any backups",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s is valid: %d job(s) configured\n", configPath, len(cfg.Jobs))
		return nil
	},
}
