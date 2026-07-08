package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// contactsScreen lists a space's contacts, either members-only or all.
type contactsScreen struct {
	space       spaceItem
	membersOnly bool
	list        list.Model
	loaded      bool
	err         error
	flash       string // transient hint, e.g. when delete is refused
}

func newContactsScreen(space spaceItem, membersOnly bool) *contactsScreen {
	title := "Contacts"
	if membersOnly {
		title = "Members"
	}
	return &contactsScreen{
		space:       space,
		membersOnly: membersOnly,
		list:        newList(title, nil),
	}
}

func (s *contactsScreen) Title() string {
	if s.membersOnly {
		return "Members"
	}
	return "Contacts"
}

func (s *contactsScreen) Init(m *Model) tea.Cmd {
	s.list.SetSize(m.width, m.listHeight(1))
	if cached, ok := m.cache[s.space.id]; ok {
		s.loaded = true
		s.list.SetItems(contactItemsFrom(cached, s.membersOnly, m.uid))
		return nil
	}
	return loadContacts(m.contacts, s.space.id)
}

func (s *contactsScreen) Update(m *Model, msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case contactsLoadedMsg:
		if msg.spaceID == s.space.id {
			m.cache[msg.spaceID] = msg.contacts
			s.loaded = true
			s.list.SetItems(contactItemsFrom(msg.contacts, s.membersOnly, m.uid))
		}
		return s, nil
	case errMsg:
		s.err = msg.err
		s.loaded = true
		return s, nil
	case tea.WindowSizeMsg:
		s.list.SetSize(msg.Width, m.listHeight(1))
		return s, nil
	case tea.KeyMsg:
		if s.list.FilterState() != list.Filtering {
			s.flash = ""
			switch msg.String() {
			case "esc", "left":
				return s, pop()
			case "enter", "right":
				if it, ok := s.list.SelectedItem().(contactItem); ok {
					return s, push(newContactCardScreen(s.space, it))
				}
				return s, nil
			case "delete", "backspace":
				if m.deleter == nil {
					return s, nil
				}
				if it, ok := s.list.SelectedItem().(contactItem); ok {
					if it.isSelf {
						s.flash = "Cannot delete yourself"
						return s, nil
					}
					return s, push(newConfirmDeleteScreen(s.space, it))
				}
				return s, nil
			}
		}
	}
	var cmd tea.Cmd
	s.list, cmd = s.list.Update(msg)
	return s, cmd
}

func (s *contactsScreen) View(m *Model) string {
	if s.err != nil {
		return headerStyle.Render(errStyle.Render("Error: "+s.err.Error())) + "\n" + footerStyle.Render(footerHelpContacts)
	}
	if !s.loaded {
		return headerStyle.Render("Loading contacts…")
	}
	footer := footerHelpContacts
	if s.flash != "" {
		footer = errStyle.Render(s.flash) + " · " + footerHelpContacts
	}
	parts := []string{s.list.View(), footerStyle.Render(footer)}
	return strings.Join(parts, "\n")
}
