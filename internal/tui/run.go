package tui

import tea "charm.land/bubbletea/v2"

// programRunner is the sliver of *tea.Program that Run needs: enough to drive
// the program and report how it ended.
type programRunner interface {
	Run() (tea.Model, error)
}

// newProgram is a seam over tea.NewProgram, so tests can run Run() against a
// fake program instead of one that requires a real terminal.
var newProgram = func(m tea.Model) programRunner {
	return tea.NewProgram(m)
}

// Run starts the interactive program on the Spaces screen and blocks until the
// user exits. It requires a real terminal (the caller checks that).
//
// The alternate screen is requested declaratively from Model.View() (its
// AltScreen field) rather than via a NewProgram option — bubbletea v2 removed
// tea.WithAltScreen() in favor of that field.
func Run(spaces SpacesReader, contacts ContactsReader, deleter ContactDeleter, uid string) error {
	p := newProgram(New(spaces, contacts, deleter, uid))
	_, err := p.Run()
	return err
}
