package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/strongo/strongo-tui/pkg/nav"
	"github.com/strongo/strongo-tui/pkg/theme"
	"github.com/strongo/strongo-tui/pkg/widgets"
)

// spaceScreen shows a space's details and a menu (Members / Contacts).
type spaceScreen struct {
	app     *app
	space   spaceItem
	menu    widgets.List
	count   int
	loaded  bool
	err     error
	w, h    int
	focused bool
}

func newSpaceScreen(a *app, space spaceItem) *spaceScreen {
	s := &spaceScreen{app: a, space: space}
	s.menu = newFilterList("space-menu", s.menuItems()...)
	return s
}

func (s *spaceScreen) menuItems() []list.Item {
	contactsDesc := "all contacts"
	membersDesc := "space members"
	if s.loaded {
		contactsDesc = fmt.Sprintf("%d contacts", s.count)
	}
	return []list.Item{
		widgets.MenuItem{ID: "members", Label: "Members", Detail: membersDesc, Ref: true},
		widgets.MenuItem{ID: "contacts", Label: "Contacts", Detail: contactsDesc, Ref: false},
	}
}

func (s *spaceScreen) Title() string { return s.space.name() }

func (s *spaceScreen) Init() tea.Cmd {
	if cached, ok := s.app.cache[s.space.id]; ok {
		s.count = len(cached)
		s.loaded = true
		s.menu.SetItems(s.menuItems()...)
		return nil
	}
	return loadContacts(s.app.contacts, s.space.id)
}

func (s *spaceScreen) ShortHelp() []key.Binding { return s.menu.ShortHelp() }

func (s *spaceScreen) Update(msg tea.Msg) (nav.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case contactsLoadedMsg:
		if msg.spaceID == s.space.id {
			s.app.cache[msg.spaceID] = msg.contacts
			s.count = len(msg.contacts)
			s.loaded = true
			s.menu.SetItems(s.menuItems()...)
		}
		return s, nil
	case errMsg:
		s.err = msg.err
		s.loaded = true
		return s, nil
	case tea.WindowSizeMsg:
		s.w, s.h = msg.Width, msg.Height
		s.layout()
		return s, nil
	case nav.ScreenFocusMsg:
		s.focused = msg.Focused
		if msg.Focused {
			s.menu.Focus()
		} else {
			s.menu.Blur()
		}
		return s, nil
	case widgets.ItemSelectedMsg:
		if membersOnly, ok := menuItemRef(msg.Item).(bool); ok {
			return s, nav.Push(nav.Page{
				Title:   contactsTitle(membersOnly),
				Content: newContactsScreen(s.app, s.space, membersOnly),
			})
		}
		return s, nil
	case tea.KeyPressMsg:
		if !s.menu.Editing() {
			switch msg.String() {
			case "esc", "left":
				return s, nav.Pop()
			}
		}
	}
	var cmd tea.Cmd
	s.menu, cmd = s.menu.Update(msg)
	return s, cmd
}

func (s *spaceScreen) layout() {
	headerH := lipgloss.Height(s.header())
	if s.err != nil {
		headerH += 1
	}
	s.menu.SetSize(s.w, max(s.h-headerH, 1))
}

func (s *spaceScreen) header() string {
	label := lipgloss.NewStyle().Foreground(theme.MutedColor())
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(theme.AccentColor()).Render(s.space.name()) + "\n")
	b.WriteString(label.Render("id:     ") + s.space.id + "\n")
	b.WriteString(label.Render("type:   ") + s.space.spaceType + "\n")
	b.WriteString(label.Render("status: ") + s.space.status + "\n")
	b.WriteString(label.Render("roles:  ") + joinRoles(s.space.roles))
	return b.String()
}

func (s *spaceScreen) View() string {
	parts := []string{s.header()}
	if s.err != nil {
		parts = append(parts, lipgloss.NewStyle().Foreground(theme.ErrorColor()).Render("Error: "+s.err.Error()))
	}
	parts = append(parts, s.menu.View())
	return strings.Join(parts, "\n")
}

func contactsTitle(membersOnly bool) string {
	if membersOnly {
		return "Members"
	}
	return "Contacts"
}
