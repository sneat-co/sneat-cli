package commands

import "github.com/spf13/cobra"

// Version prints build metadata as JSON. Values are injected via -ldflags.
func Version(ver, commit, date string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build version, commit hash, and build date",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeJSON(cmd.OutOrStdout(), map[string]string{
				"version": ver, "commit": commit, "date": date,
			})
		},
	}
}
