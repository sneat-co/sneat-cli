package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// fakeProgram stands in for *tea.Program so Run can be exercised without a
// real terminal.
type fakeProgram struct {
	err error
}

func (f fakeProgram) Run() (tea.Model, error) { return nil, f.err }

// TestNewProgram_DefaultBuildsARealProgram exercises the seam's default value
// (real tea.NewProgram construction), without running it — running would need
// a real terminal.
func TestNewProgram_DefaultBuildsARealProgram(t *testing.T) {
	p := newProgram(New(fakeSpaces{}, &fakeContacts{}, nil, "uid"))
	if p == nil {
		t.Fatal("newProgram returned nil")
	}
}

func TestRun_WiresProgramResult(t *testing.T) {
	orig := newProgram
	t.Cleanup(func() { newProgram = orig })

	t.Run("success", func(t *testing.T) {
		newProgram = func(tea.Model) programRunner { return fakeProgram{} }
		if err := Run(fakeSpaces{}, &fakeContacts{}, nil, "uid"); err != nil {
			t.Errorf("Run() = %v, want nil", err)
		}
	})

	t.Run("propagates the program's error", func(t *testing.T) {
		boom := errors.New("boom")
		newProgram = func(tea.Model) programRunner { return fakeProgram{err: boom} }
		if err := Run(fakeSpaces{}, &fakeContacts{}, nil, "uid"); !errors.Is(err, boom) {
			t.Errorf("Run() = %v, want %v", err, boom)
		}
	})
}
