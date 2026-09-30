package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/tuigoff/tuigoff/pkg/nav"
	"github.com/tuigoff/tuigoff/pkg/theme"
	"github.com/tuigoff/tuigoff/pkg/widgets"
)

// confirmDeleteScreen asks the user to confirm deleting a contact and then
// issues the delete against the sneat-go API. It is pushed onto the stack from
// the contacts list or a contact card.
type confirmDeleteScreen struct {
	app      *app
	space    spaceItem
	contact  contactItem
	fromCard bool
	deleting bool
	err      error
	w, h     int
}

func newConfirmDeleteScreen(a *app, space spaceItem, contact contactItem, fromCard bool) *confirmDeleteScreen {
	return &confirmDeleteScreen{app: a, space: space, contact: contact, fromCard: fromCard}
}

func (s *confirmDeleteScreen) Title() string { return "Delete contact" }

func (s *confirmDeleteScreen) Init() tea.Cmd { return nil }

func (s *confirmDeleteScreen) ShortHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter", "delete", "backspace"), key.WithHelp("enter", "confirm")),
		key.NewBinding(key.WithKeys("esc", "left"), key.WithHelp("esc", "cancel")),
	}
}

func (s *confirmDeleteScreen) Update(msg tea.Msg) (nav.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.w, s.h = msg.Width, msg.Height
		return s, nil
	case deleteErrMsg:
		s.deleting = false
		s.err = msg.err
		return s, nil
	case contactDeletedMsg:
		delete(s.app.cache, msg.spaceID)
		cmds := []tea.Cmd{nav.Pop()}
		if s.fromCard {
			cmds = append(cmds, nav.Pop())
		}
		cmds = append(cmds, loadContacts(s.app.contacts, msg.spaceID))
		return s, tea.Batch(cmds...)
	case tea.KeyPressMsg:
		if s.deleting {
			return s, nil // ignore input while the delete is in flight
		}
		switch msg.String() {
		case "enter", "delete", "backspace":
			if s.app.deleter == nil {
				return s, nav.Pop()
			}
			s.deleting = true
			s.err = nil
			return s, deleteContact(s.app.deleter, s.space.id, s.contact.id)
		case "esc", "left":
			return s, nav.Pop()
		}
	}
	return s, nil
}

func (s *confirmDeleteScreen) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.AccentColor())
	muted := lipgloss.NewStyle().Foreground(theme.MutedColor())
	errStyle := lipgloss.NewStyle().Foreground(theme.ErrorColor())
	var b strings.Builder
	b.WriteString(title.Render("Delete contact") + "\n\n")
	b.WriteString("Delete \"" + s.contact.title + "\"?\n")
	b.WriteString(muted.Render("This cannot be undone.") + "\n")
	if s.deleting {
		b.WriteString("\nDeleting…\n")
	}
	if s.err != nil {
		b.WriteString("\n" + errStyle.Render("Error: "+s.err.Error()) + "\n")
	}
	return widgets.Fit(b.String(), s.w, s.h)
}
