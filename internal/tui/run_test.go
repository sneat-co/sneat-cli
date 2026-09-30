package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/strongo/strongo-tui/pkg/nav"
)

func TestRun_WiresShellResult(t *testing.T) {
	orig := runShell
	t.Cleanup(func() { runShell = orig })

	t.Run("success", func(t *testing.T) {
		runShell = func(nav.Model, ...tea.ProgramOption) error { return nil }
		if err := Run(fakeSpaces{}, &fakeContacts{}, nil, "uid"); err != nil {
			t.Errorf("Run() = %v, want nil", err)
		}
	})

	t.Run("propagates the program's error", func(t *testing.T) {
		boom := errors.New("boom")
		runShell = func(nav.Model, ...tea.ProgramOption) error { return boom }
		if err := Run(fakeSpaces{}, &fakeContacts{}, nil, "uid"); !errors.Is(err, boom) {
			t.Errorf("Run() = %v, want %v", err, boom)
		}
	})
}

func TestNew_BuildsShell(t *testing.T) {
	m := New(fakeSpaces{}, &fakeContacts{}, nil, "uid")
	if m.Depth() != 1 {
		t.Fatalf("Depth = %d", m.Depth())
	}
}
