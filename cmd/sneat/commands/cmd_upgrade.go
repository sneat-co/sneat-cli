package commands

import (
	"github.com/spf13/cobra"
	"github.com/strongo/cli-helpers/cliinstall"
	"github.com/strongo/cli-helpers/cliinstall/cobracmd"
)

type upgradeErrors struct{ installErrors }

func (upgradeErrors) UpgradesAvailable(_ []cliinstall.UpgradeResult) error {
	return nil
}

// UpgradeCommand returns the "upgrade" command.
func UpgradeCommand(ver string) *cobra.Command {
	return cobracmd.NewUpgrade(cobracmd.UpgradeCommandOptions{
		Short:      "Upgrade installed fleet CLIs, including sneat itself",
		Errors:     upgradeErrors{},
		HostID:     "sneat",
		HostConfig: sneatSelfUpdateConfig(ver),
	})
}
