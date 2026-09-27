package commands

import (
	"github.com/spf13/cobra"
	"github.com/strongo/cli-helpers/cliinstall/cobracmd"
)

type installErrors struct{}

func (installErrors) Failure(err error) error {
	return &ExitCodeError{Code: failureExitCode(err), Err: err}
}

// InstallCommand returns the "install" command for fleet CLIs relevant to sneat.
func InstallCommand() *cobra.Command {
	return cobracmd.New(cobracmd.CommandOptions{
		Short:  "List and install fleet CLIs relevant to sneat",
		Errors: installErrors{},
		HostID: "sneat",
	})
}
