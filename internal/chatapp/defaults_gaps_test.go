package chatapp

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/oauth2"

	"github.com/sneat-co/sneat-cli/internal/config"
)

// TestNewReaders_DefaultIsLazy covers newReaders' own real default (every
// other test overrides it): firestoredb.NewSession and the three
// data.NewFirestore* constructors are all lazy -- they open no network
// connection until a reader is actually read from -- so calling the real
// default directly is safe in a unit test and needs no fake server.
func TestNewReaders_DefaultIsLazy(t *testing.T) {
	readers, closer := newReaders(config.Config{Project: "p1"}, fakeTokenSource{tok: &oauth2.Token{AccessToken: "t"}}, time.UTC)
	if readers.Happenings == nil || readers.Todos == nil || readers.Contacts == nil {
		t.Fatalf("newReaders() readers = %+v, want all three populated", readers)
	}
	if closer == nil {
		t.Fatal("newReaders() closer = nil")
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("closer.Close() = %v, want nil (never opened a real connection)", err)
	}
}

// probeQuitModel is a minimal tea.Model that quits on Init, used only to
// drive runProgram's own real default to completion quickly.
type probeQuitModel struct{}

func (probeQuitModel) Init() tea.Cmd                         { return tea.Quit }
func (m probeQuitModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (probeQuitModel) View() tea.View                        { return tea.NewView("") }

// TestRunProgram_DefaultReturnsWithoutHanging covers runProgram's own real
// default. tea.NewProgram(model).Run() opens /dev/tty for its controlling
// terminal, which a non-interactive test process -- sandboxed locally or a
// headless CI runner -- never has, so this returns a non-nil error quickly
// and deterministically in both places; a bounded select guards against
// ever hanging the suite if that assumption stops holding on some future
// runner (e.g. one that allocates a real pty to the test binary).
func TestRunProgram_DefaultReturnsWithoutHanging(t *testing.T) {
	done := make(chan error, 1)
	go func() { done <- runProgram(probeQuitModel{}) }()
	select {
	case err := <-done:
		if err == nil {
			t.Log("runProgram(default) returned nil: this runner has a real controlling terminal")
			return
		}
		t.Logf("runProgram(default) returned the expected no-TTY error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("runProgram's default hung instead of returning")
	}
}
