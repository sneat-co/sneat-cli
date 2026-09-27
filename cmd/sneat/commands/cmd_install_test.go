package commands

import (
	"errors"
	"testing"

	"github.com/strongo/cli-helpers/cliinstall/cobracmd"
	"github.com/strongo/cli-helpers/selfupdate"
)

func TestInstallCommand_Shape(t *testing.T) {
	t.Parallel()

	cmd := InstallCommand()
	if cmd.Name() != "install" {
		t.Errorf("Name() = %q, want %q", cmd.Name(), "install")
	}
	for _, name := range []string{"all", "yes", "dry-run", "dir", "format"} {
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

func TestInstallErrors_Failure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"usage error", &cobracmd.UsageError{Err: errors.New("invalid flag")}, 2},
		{"unknown target", &selfupdate.Failure{Kind: selfupdate.KindUnknownTarget, Err: errors.New("unknown target")}, 2},
		{"no install dir", &selfupdate.Failure{Kind: selfupdate.KindNoInstallDir, Err: errors.New("no install dir")}, 4},
		{"destination exists", &selfupdate.Failure{Kind: selfupdate.KindDestinationExists, Err: errors.New("destination exists")}, 4},
		{"plain error", errors.New("plain error"), 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := (installErrors{}).Failure(c.err)
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
