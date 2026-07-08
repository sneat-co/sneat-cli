package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// contactCardScreen shows a read-only detail card for one contact.
type contactCardScreen struct {
	space   spaceItem
	contact contactItem
	flash   string // transient hint, e.g. when delete is refused
}

func newContactCardScreen(space spaceItem, contact contactItem) *contactCardScreen {
	return &contactCardScreen{space: space, contact: contact}
}

func (s *contactCardScreen) Title() string { return s.contact.title }

func (s *contactCardScreen) Init(*Model) tea.Cmd { return nil }

func (s *contactCardScreen) Update(m *Model, msg tea.Msg) (screen, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		s.flash = ""
		switch key.String() {
		case "esc", "left":
			return s, pop()
		case "delete", "backspace":
			if m.deleter == nil {
				return s, nil
			}
			if s.contact.isSelf {
				s.flash = "Cannot delete yourself"
				return s, nil
			}
			return s, push(newConfirmDeleteScreen(s.space, s.contact))
		}
	}
	return s, nil
}

func (s *contactCardScreen) View(m *Model) string {
	c := s.contact
	var b strings.Builder
	b.WriteString(titleStyle.Render(c.title) + "\n\n")
	row := func(label, value string) {
		if value == "" {
			value = "—"
		}
		b.WriteString(labelStyle.Render(pad(label)) + value + "\n")
	}
	row("id", c.id)
	row("type", c.ctype)
	row("gender", c.gender)
	row("age group", c.ageGroup)
	row("status", c.status)
	row("roles", joinRoles(c.roles))
	row("emails", strings.Join(c.emails, ", "))
	row("phones", strings.Join(c.phones, ", "))

	body := headerStyle.Render(b.String())
	help := "esc/← back · del delete · ^c quit"
	if s.flash != "" {
		help = errStyle.Render(s.flash) + " · " + help
	}
	footer := footerStyle.Render(help)
	// Pad the body so the footer sits at the bottom, matching the list screens.
	if h := m.height - lipgloss.Height(footer); h > lipgloss.Height(body) {
		body = lipgloss.NewStyle().Height(h).Render(body)
	}
	return body + "\n" + footer
}

// pad right-pads a label to a fixed width for aligned card rows.
func pad(label string) string {
	const w = 11
	if len(label) >= w {
		return label + " "
	}
	return label + strings.Repeat(" ", w-len(label))
}
