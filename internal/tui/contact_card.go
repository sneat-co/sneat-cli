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
}

func newContactCardScreen(space spaceItem, contact contactItem) *contactCardScreen {
	return &contactCardScreen{space: space, contact: contact}
}

func (s *contactCardScreen) Title() string { return s.contact.title }

func (s *contactCardScreen) Init(*Model) tea.Cmd { return nil }

func (s *contactCardScreen) Update(m *Model, msg tea.Msg) (screen, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "q":
			return s, tea.Quit
		case "esc", "left", "backspace":
			return s, pop()
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
	footer := footerStyle.Render("esc/← back · q quit")
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
