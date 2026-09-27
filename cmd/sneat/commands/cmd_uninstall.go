package commands

import (
	"github.com/spf13/cobra"
	"github.com/strongo/cli-helpers/cliinstall/cobracmd"
)

// UninstallCommand returns the "uninstall" command for fleet CLIs.
func UninstallCommand() *cobra.Command {
	return cobracmd.NewUninstall(cobracmd.UninstallCommandOptions{
		Errors: installErrors{},
		HostID: "sneat",
	})
}
