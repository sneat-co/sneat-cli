package commands

import (
	"errors"

	installcobracmd "github.com/strongo/cli-helpers/cliinstall/cobracmd"
	"github.com/strongo/cli-helpers/selfupdate"
	selfupdatecobracmd "github.com/strongo/cli-helpers/selfupdate/cobracmd"
	skillscobracmd "github.com/strongo/cli-helpers/skillsync/cobracmd"
)

// failureExitCode maps a self-update/cli-install/skills-sync failure onto standard CLI exit codes:
// 2 invalid arguments, 3 not found, 4 connection/I/O failure, 1 generic catch-all.
func failureExitCode(err error) int {
	var selfUsage *selfupdatecobracmd.UsageError
	if errors.As(err, &selfUsage) {
		return 2
	}
	var installUsage *installcobracmd.UsageError
	if errors.As(err, &installUsage) {
		return 2
	}
	var skillsUsage *skillscobracmd.UsageError
	if errors.As(err, &skillsUsage) {
		return 2
	}
	switch selfupdate.KindOf(err) {
	case selfupdate.KindUnknownTarget, selfupdate.KindDowngrade, selfupdate.KindNonInteractive:
		return 2
	case selfupdate.KindUnknownTag, selfupdate.KindUnsupportedPlatform:
		return 3
	case selfupdate.KindReleaseLookup, selfupdate.KindDownload, selfupdate.KindPermission, selfupdate.KindNoInstallDir, selfupdate.KindDestinationExists:
		return 4
	default:
		return 1
	}
}
