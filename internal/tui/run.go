package tui

import tea "github.com/charmbracelet/bubbletea"

// Run starts the interactive program on the Spaces screen and blocks until the
// user exits. It requires a real terminal (the caller checks that).
func Run(spaces SpacesReader, contacts ContactsReader, uid string) error {
	p := tea.NewProgram(New(spaces, contacts, uid), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
