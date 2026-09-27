package commands

import (
	"errors"
	"testing"

	"github.com/strongo/cli-helpers/cliinstall"
	"github.com/strongo/cli-helpers/cliinstall/cobracmd"
	"github.com/strongo/cli-helpers/selfupdate"
)

func TestUpgradeCommand_Shape(t *testing.T) {
	t.Parallel()

	cmd := UpgradeCommand("0.14.0")
	if cmd.Name() != "upgrade" {
		t.Errorf("Name() = %q, want %q", cmd.Name(), "upgrade")
	}
	for _, name := range []string{"all", "check", "yes", "dry-run", "format"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s flag", name)
		}
	}
	if f := cmd.Flags().Lookup("yes"); f.Shorthand != "y" {
		t.Errorf("--yes shorthand = %q, want y", f.Shorthand)
	}
	if cmd.Short == "" {
		t.Error("Short is empty, want a description")
	}
}

func TestUpgradeErrors_Failure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"usage error", &cobracmd.UsageError{Err: errors.New("invalid flag")}, 2},
		{"unknown target", &selfupdate.Failure{Kind: selfupdate.KindUnknownTarget, Err: errors.New("unknown target")}, 2},
		{"plain error", errors.New("plain error"), 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := (upgradeErrors{}).Failure(c.err)
			var exitErr *ExitCodeError
			if !errors.As(got, &exitErr) {
				t.Fatalf("Failure(%v) = %v (%T), want an *ExitCodeError", c.err, got, got)
			}
			if exitErr.Code != c.want {
				t.Errorf("Failure(%v) code = %d, want %d", c.err, exitErr.Code, c.want)
			}
		})
	}
}

func TestUpgradeErrors_UpgradesAvailable(t *testing.T) {
	t.Parallel()
	if err := (upgradeErrors{}).UpgradesAvailable([]cliinstall.UpgradeResult{}); err != nil {
		t.Errorf("UpgradesAvailable() = %v, want nil", err)
	}
}
