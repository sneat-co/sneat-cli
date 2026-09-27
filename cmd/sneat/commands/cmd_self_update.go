package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/strongo/cli-helpers/cliinstall"
	"github.com/strongo/cli-helpers/selfupdate"
	selfupdatecobracmd "github.com/strongo/cli-helpers/selfupdate/cobracmd"
)

var catalogByID = cliinstall.ByID

type selfUpdateErrors struct{}

func (selfUpdateErrors) Failure(err error) error {
	return &ExitCodeError{Code: failureExitCode(err), Err: err}
}

func (selfUpdateErrors) UpdateAvailable(_ selfupdate.CheckResult) error {
	return nil
}

func sneatSelfUpdateConfig(ver string) selfupdate.Config {
	entry, ok := catalogByID("sneat")
	if !ok {
		panic(fmt.Sprintf("cliinstall: no catalog entry for %q", "sneat"))
	}
	return entry.Config(ver)
}

// SelfUpdateCommand returns the "self-update" command.
func SelfUpdateCommand(ver string) *cobra.Command {
	return selfupdatecobracmd.New(sneatSelfUpdateConfig(ver), selfupdatecobracmd.CommandOptions{
		Short:      "Update the installed sneat binary in place",
		JSONFormat: true,
		Errors:     selfUpdateErrors{},
	})
}
