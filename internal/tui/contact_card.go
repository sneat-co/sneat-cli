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

// contactCardScreen shows a read-only detail card for one contact.
type contactCardScreen struct {
	app     *app
	space   spaceItem
	contact contactItem
	w, h    int
}

func newContactCardScreen(a *app, space spaceItem, contact contactItem) *contactCardScreen {
	return &contactCardScreen{app: a, space: space, contact: contact}
}

func (s *contactCardScreen) Title() string { return s.contact.title }

func (s *contactCardScreen) Init() tea.Cmd { return nil }

func (s *contactCardScreen) ShortHelp() []key.Binding {
	help := []key.Binding{
		key.NewBinding(key.WithKeys("esc", "left"), key.WithHelp("esc", "back")),
	}
	if s.app.deleter != nil {
		help = append(help, key.NewBinding(key.WithKeys("delete", "backspace"), key.WithHelp("del", "delete")))
	}
	return help
}

func (s *contactCardScreen) Update(msg tea.Msg) (nav.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.w, s.h = msg.Width, msg.Height
		return s, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "left":
			return s, nav.Pop()
		case "delete", "backspace":
			if s.app.deleter == nil {
				return s, nil
			}
			if s.contact.isSelf {
				return s, nav.Alert("Cannot delete", "Cannot delete yourself", 0, nav.FocusToContent)
			}
			return s, nav.Push(nav.Page{
				Title:   "Delete contact",
				Content: newConfirmDeleteScreen(s.app, s.space, s.contact, true),
			})
		}
	}
	return s, nil
}

func (s *contactCardScreen) View() string {
	c := s.contact
	label := lipgloss.NewStyle().Foreground(theme.MutedColor())
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.AccentColor())
	var b strings.Builder
	b.WriteString(title.Render(c.title) + "\n\n")
	row := func(name, value string) {
		if value == "" {
			value = "—"
		}
		b.WriteString(label.Render(pad(name)) + value + "\n")
	}
	row("id", c.id)
	row("type", c.ctype)
	row("gender", c.gender)
	row("age group", c.ageGroup)
	row("status", c.status)
	row("roles", joinRoles(c.roles))
	row("emails", strings.Join(c.emails, ", "))
	row("phones", strings.Join(c.phones, ", "))
	return widgets.Fit(b.String(), s.w, s.h)
}

// pad right-pads a label to a fixed width for aligned card rows.
func pad(label string) string {
	const w = 11
	if len(label) >= w {
		return label + " "
	}
	return label + strings.Repeat(" ", w-len(label))
}
