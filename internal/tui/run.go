package tui

import tea "charm.land/bubbletea/v2"

// Run starts the interactive program on the Spaces screen and blocks until the
// user exits. It requires a real terminal (the caller checks that).
//
// The alternate screen is requested declaratively from Model.View() (its
// AltScreen field) rather than via a NewProgram option — bubbletea v2 removed
// tea.WithAltScreen() in favor of that field.
func Run(spaces SpacesReader, contacts ContactsReader, deleter ContactDeleter, uid string) error {
	p := tea.NewProgram(New(spaces, contacts, deleter, uid))
	_, err := p.Run()
	return err
}
