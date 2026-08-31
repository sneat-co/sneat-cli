package commands

import (
	"github.com/spf13/cobra"
	"github.com/strongo/buildinfo"
	buildinfocmd "github.com/strongo/buildinfo/cobracmd"
)

// Version returns the `version` subcommand. It delegates to
// github.com/strongo/buildinfo/cobracmd.VersionCommand, which prints
// info.Long() ("<name> <version> (<commit>) <date>") — the exact Info the
// root command's --version/-v flag is also fed (see main.go), so the two
// surfaces can never report different versions.
func Version(info buildinfo.Info) *cobra.Command {
	return buildinfocmd.VersionCommand(info)
}
