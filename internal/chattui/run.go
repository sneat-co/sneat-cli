package chattui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sneat-co/sneat-cli/internal/chat"
)

// Run starts the interactive chat session and blocks until the user exits. It
// requires a real terminal (the caller checks that).
//
// It takes the chat.Processor interface: the composition root that builds a
// concrete one is the RunChat closure in cmd/sneat/main.go
// (chat-messenger#req:processor-seam).
func Run(proc chat.Processor) error {
	_, err := newProgram(proc).Run()
	return err
}

// newProgram builds the chat session's terminal program.
//
// Deliberately absent: tea.WithAltScreen(), which internal/tui's Run does pass.
// The alternate screen is a separate buffer the terminal discards on exit,
// taking the transcript with it and leaving nothing in scrollback. The chat
// draws inline instead, in the normal buffer, so past turns stay selectable,
// copyable and searchable with the terminal's own tooling and survive the
// session (REQ: inline-rendering).
//
// Split out from Run so a test can inspect the program tea.NewProgram returned
// without starting one against a terminal.
func newProgram(proc chat.Processor) *tea.Program {
	return tea.NewProgram(New(proc))
}
