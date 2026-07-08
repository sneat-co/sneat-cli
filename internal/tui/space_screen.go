package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// spaceScreen shows a space's details and a menu (Members / Contacts).
type spaceScreen struct {
	space  spaceItem
	menu   list.Model
	count  int
	loaded bool
	err    error
}

func newSpaceScreen(space spaceItem) *spaceScreen {
	s := &spaceScreen{space: space}
	s.menu = newList(space.name(), s.menuItems())
	return s
}

func (s *spaceScreen) menuItems() []list.Item {
	contactsDesc := "all contacts"
	membersDesc := "space members"
	if s.loaded {
		contactsDesc = fmt.Sprintf("%d contacts", s.count)
	}
	return []list.Item{
		menuItem{label: "Members", desc: membersDesc, membersOnly: true},
		menuItem{label: "Contacts", desc: contactsDesc, membersOnly: false},
	}
}

func (s *spaceScreen) Title() string { return s.space.name() }

func (s *spaceScreen) headerLines() int { return 5 }

func (s *spaceScreen) Init(m *Model) tea.Cmd {
	s.menu.SetSize(m.width, m.listHeight(s.headerLines()))
	if cached, ok := m.cache[s.space.id]; ok {
		s.count = len(cached)
		s.loaded = true
		s.menu.SetItems(s.menuItems())
		return nil
	}
	return loadContacts(m.contacts, s.space.id)
}

func (s *spaceScreen) Update(m *Model, msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case contactsLoadedMsg:
		if msg.spaceID == s.space.id {
			m.cache[msg.spaceID] = msg.contacts
			s.count = len(msg.contacts)
			s.loaded = true
			s.menu.SetItems(s.menuItems())
		}
		return s, nil
	case errMsg:
		s.err = msg.err
		s.loaded = true
		return s, nil
	case tea.WindowSizeMsg:
		s.menu.SetSize(msg.Width, m.listHeight(s.headerLines()))
		return s, nil
	case tea.KeyMsg:
		if s.menu.FilterState() != list.Filtering {
			switch msg.String() {
			case "esc", "left":
				return s, pop()
			case "enter", "right":
				if it, ok := s.menu.SelectedItem().(menuItem); ok {
					return s, push(newContactsScreen(s.space, it.membersOnly))
				}
				return s, nil
			}
		}
	}
	var cmd tea.Cmd
	s.menu, cmd = s.menu.Update(msg)
	return s, cmd
}

func (s *spaceScreen) header() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(s.space.name()) + "\n")
	b.WriteString(labelStyle.Render("id:     ") + s.space.id + "\n")
	b.WriteString(labelStyle.Render("type:   ") + s.space.spaceType + "\n")
	b.WriteString(labelStyle.Render("status: ") + s.space.status + "\n")
	b.WriteString(labelStyle.Render("roles:  ") + joinRoles(s.space.roles))
	return headerStyle.Render(b.String())
}

func (s *spaceScreen) View(m *Model) string {
	parts := []string{s.header()}
	if s.err != nil {
		parts = append(parts, headerStyle.Render(errStyle.Render("Error: "+s.err.Error())))
	}
	parts = append(parts, s.menu.View(), footerStyle.Render(footerHelp))
	return strings.Join(parts, "\n")
}
