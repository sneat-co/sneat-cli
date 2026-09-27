package commands

import (
	"testing"
)

func TestUninstallCommand_Shape(t *testing.T) {
	t.Parallel()

	cmd := UninstallCommand()
	if cmd.Name() != "uninstall" {
		t.Errorf("Name() = %q, want %q", cmd.Name(), "uninstall")
	}
	for _, name := range []string{"all", "yes", "dry-run", "format"} {
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
