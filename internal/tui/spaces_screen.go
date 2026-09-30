package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/strongo/strongo-tui/pkg/nav"
	"github.com/strongo/strongo-tui/pkg/widgets"
)

// spacesScreen is the root screen: a selectable list of the user's spaces.
type spacesScreen struct {
	app     *app
	list    widgets.List
	loaded  bool
	err     error
	w, h    int
	focused bool
}

func newSpacesScreen(a *app) *spacesScreen {
	return &spacesScreen{app: a, list: newFilterList("spaces")}
}

func (s *spacesScreen) Title() string { return "Spaces" }

func (s *spacesScreen) Init() tea.Cmd {
	return loadSpaces(s.app.spaces, s.app.uid)
}

func (s *spacesScreen) ShortHelp() []key.Binding { return s.list.ShortHelp() }

func (s *spacesScreen) Update(msg tea.Msg) (nav.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case spacesLoadedMsg:
		s.loaded = true
		s.list.SetItems(spaceItemsFrom(msg.spaces)...)
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
		if sp, ok := menuItemRef(msg.Item).(spaceItem); ok {
			return s, nav.Push(nav.Page{Title: sp.name(), Content: newSpaceScreen(s.app, sp)})
		}
		return s, nil
	case tea.KeyPressMsg:
		if !s.list.Editing() {
			switch msg.String() {
			case "esc", "left":
				return s, tea.Quit // Spaces is the root screen: back exits the app.
			}
		}
	}
	var cmd tea.Cmd
	s.list, cmd = s.list.Update(msg)
	return s, cmd
}

func (s *spacesScreen) View() string {
	if s.err != nil {
		return widgets.Fit("Error: "+s.err.Error(), s.w, s.h)
	}
	if !s.loaded {
		return widgets.Fit("Loading spaces…", s.w, s.h)
	}
	return s.list.View()
}
