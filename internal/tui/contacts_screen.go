package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/tuigoff/tuigoff/pkg/nav"
	"github.com/tuigoff/tuigoff/pkg/widgets"
)

// contactsScreen lists a space's contacts, either members-only or all.
type contactsScreen struct {
	app         *app
	space       spaceItem
	membersOnly bool
	list        widgets.List
	loaded      bool
	err         error
	w, h        int
	focused     bool
}

func newContactsScreen(a *app, space spaceItem, membersOnly bool) *contactsScreen {
	return &contactsScreen{
		app:         a,
		space:       space,
		membersOnly: membersOnly,
		list:        newFilterList("contacts"),
	}
}

func (s *contactsScreen) Title() string { return contactsTitle(s.membersOnly) }

func (s *contactsScreen) Init() tea.Cmd {
	if cached, ok := s.app.cache[s.space.id]; ok {
		s.loaded = true
		s.list.SetItems(contactItemsFrom(cached, s.membersOnly, s.app.uid)...)
		return nil
	}
	return loadContacts(s.app.contacts, s.space.id)
}

func (s *contactsScreen) ShortHelp() []key.Binding {
	help := s.list.ShortHelp()
	if s.app.deleter != nil {
		help = append(help, key.NewBinding(key.WithKeys("delete", "backspace"), key.WithHelp("del", "delete")))
	}
	return help
}

func (s *contactsScreen) Update(msg tea.Msg) (nav.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case contactsLoadedMsg:
		if msg.spaceID == s.space.id {
			s.app.cache[msg.spaceID] = msg.contacts
			s.loaded = true
			s.list.SetItems(contactItemsFrom(msg.contacts, s.membersOnly, s.app.uid)...)
		}
		return s, nil
	case errMsg:
		s.err = msg.err
		s.loaded = true
		return s, nil
	case tea.WindowSizeMsg:
		s.w, s.h = msg.Width, msg.Height
		s.list.SetSize(s.w, s.h)
		return s, nil
	case nav.ScreenFocusMsg:
		s.focused = msg.Focused
		if msg.Focused {
			s.list.Focus()
		} else {
			s.list.Blur()
		}
		return s, nil
	case widgets.ItemSelectedMsg:
		if ci, ok := menuItemRef(msg.Item).(contactItem); ok {
			return s, nav.Push(nav.Page{
				Title:   ci.title,
				Content: newContactCardScreen(s.app, s.space, ci),
			})
		}
		return s, nil
	case tea.KeyPressMsg:
		if !s.list.Editing() {
			switch msg.String() {
			case "esc", "left":
				return s, nav.Pop()
			case "delete", "backspace":
				return s, s.requestDelete()
			}
		}
	}
	var cmd tea.Cmd
	s.list, cmd = s.list.Update(msg)
	return s, cmd
}

func (s *contactsScreen) requestDelete() tea.Cmd {
	if s.app.deleter == nil {
		return nil
	}
	ci, ok := menuItemRef(s.list.SelectedItem()).(contactItem)
	if !ok {
		return nil
	}
	if ci.isSelf {
		return nav.Alert("Cannot delete", "Cannot delete yourself", 0, nav.FocusToContent)
	}
	return nav.Push(nav.Page{
		Title:   "Delete contact",
		Content: newConfirmDeleteScreen(s.app, s.space, ci, false),
	})
}

func (s *contactsScreen) View() string {
	if s.err != nil {
		return widgets.Fit("Error: "+s.err.Error(), s.w, s.h)
	}
	if !s.loaded {
		return widgets.Fit("Loading contacts…", s.w, s.h)
	}
	return s.list.View()
}
