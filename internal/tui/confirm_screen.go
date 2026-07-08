package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// confirmDeleteScreen asks the user to confirm deleting a contact and then
// issues the delete against the sneat-go API. It is pushed onto the stack from
// the contacts list or a contact card.
type confirmDeleteScreen struct {
	space    spaceItem
	contact  contactItem
	deleting bool
	err      error
}

func newConfirmDeleteScreen(space spaceItem, contact contactItem) *confirmDeleteScreen {
	return &confirmDeleteScreen{space: space, contact: contact}
}

func (s *confirmDeleteScreen) Title() string { return "Delete contact" }

func (s *confirmDeleteScreen) Init(*Model) tea.Cmd { return nil }

func (s *confirmDeleteScreen) Update(m *Model, msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case deleteErrMsg:
		s.deleting = false
		s.err = msg.err
		return s, nil
	case tea.KeyMsg:
		if s.deleting {
			return s, nil // ignore input while the delete is in flight
		}
		switch msg.String() {
		case "enter", "delete", "backspace":
			if m.deleter == nil {
				return s, pop()
			}
			s.deleting = true
			s.err = nil
			return s, deleteContact(m.deleter, s.space.id, s.contact.id)
		case "esc", "left":
			return s, pop()
		}
	}
	return s, nil
}

func (s *confirmDeleteScreen) View(m *Model) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Delete contact") + "\n\n")
	b.WriteString("Delete \"" + s.contact.title + "\"?\n")
	b.WriteString(labelStyle.Render("This cannot be undone.") + "\n")
	if s.deleting {
		b.WriteString("\nDeleting…\n")
	}
	if s.err != nil {
		b.WriteString("\n" + errStyle.Render("Error: "+s.err.Error()) + "\n")
	}

	body := headerStyle.Render(b.String())
	footer := footerStyle.Render("enter/del confirm · esc cancel")
	// Pad the body so the footer sits at the bottom, matching the other screens.
	if h := m.height - lipgloss.Height(footer); h > lipgloss.Height(body) {
		body = lipgloss.NewStyle().Height(h).Render(body)
	}
	return body + "\n" + footer
}
