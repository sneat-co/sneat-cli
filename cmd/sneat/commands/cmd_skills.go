package commands

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/spf13/cobra"
	"github.com/strongo/cli-helpers/skillsync"
	skillscmd "github.com/strongo/cli-helpers/skillsync/cobracmd"

	"github.com/sneat-co/sneat-cli/ai"
)

const (
	sneatSkillsPluginVersion = "0.0.0"
	sneatSkillsUnknownSource = "0000000000000000000000000000000000000000"
)

var (
	sneatSkillsCLI    = skillsync.Identity{Publisher: "sneat", Name: "sneat"}
	sneatSkillsPlugin = skillsync.PluginIdentity{Publisher: "sneat", Name: "sneat"}
	sneatSkillsFS     = fs.FS(ai.SkillsFS)
)

type skillsErrors struct{}

func (skillsErrors) Failure(err error) error {
	return &ExitCodeError{Code: failureExitCode(err), Err: err}
}

func (skillsErrors) Conflict(report skillsync.Report) error {
	return &ExitCodeError{
		Code: 1,
		Err: fmt.Errorf(
			"skills sync: %d skill(s) could not be installed because another plugin or an unmanaged directory already owns the name; see %s",
			len(report.Names(skillsync.Conflict)), report.Dir,
		),
	}
}

// SkillsCommand returns the "skills" command tree.
func SkillsCommand(ver string) *cobra.Command {
	cfg, cfgErr := sneatSkillsSyncConfig(ver)
	options := skillscmd.CommandOptions{
		Short:  "Install sneat Agent Skills into a harness skills directory",
		Errors: skillsErrors{},
	}
	command := skillscmd.New(cfg, options)
	if cfgErr != nil {
		command.RunE = func(*cobra.Command, []string) error {
			return skillsErrors{}.Failure(fmt.Errorf("prepare embedded sneat skills: %w", cfgErr))
		}
	}
	return command
}

func sneatSkillsSyncConfig(ver string) (skillsync.Config, error) {
	source, err := fs.Sub(sneatSkillsFS, "skills")
	if err != nil {
		return skillsync.Config{}, err
	}
	digest, err := skillsync.Digest(source)
	if err != nil {
		return skillsync.Config{}, err
	}
	revision := sneatSkillsUnknownSource
	pluginVersion := strings.TrimPrefix(ver, "v")
	if _, err := skillsync.CompareVersions(pluginVersion, pluginVersion); err != nil {
		pluginVersion = sneatSkillsPluginVersion
	}
	bundle, err := skillsync.EmbeddedBundle(skillsync.BundleDescriptor{
		Plugin: sneatSkillsPlugin,
		Source: skillsync.Source{
			Repository: "github.com/sneat-co/sneat-cli",
			Path:       "ai/skills",
			Revision:   revision,
			Version:    pluginVersion,
			Digest:     digest,
		},
	}, source)
	if err != nil {
		return skillsync.Config{}, err
	}
	return skillsync.Config{
		CLI:            sneatSkillsCLI,
		CurrentVersion: ver,
		Bundles:        []skillsync.Bundle{bundle},
	}, nil
}
