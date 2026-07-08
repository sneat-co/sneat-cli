package tui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// spacesScreen is the root screen: a selectable list of the user's spaces.
type spacesScreen struct {
	list   list.Model
	loaded bool
	err    error
}

func newSpacesScreen() *spacesScreen {
	return &spacesScreen{list: newList("Spaces", nil)}
}

func (s *spacesScreen) Title() string { return "Spaces" }

func (s *spacesScreen) Init(m *Model) tea.Cmd {
	s.list.SetSize(m.width, m.listHeight(1))
	return loadSpaces(m.spaces, m.uid)
}

func (s *spacesScreen) Update(m *Model, msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case spacesLoadedMsg:
		s.loaded = true
		cmd := s.list.SetItems(spaceItemsFrom(msg.spaces))
		return s, cmd
	case errMsg:
		s.err = msg.err
		s.loaded = true
		return s, nil
	case tea.WindowSizeMsg:
		s.list.SetSize(msg.Width, m.listHeight(1))
		return s, nil
	case tea.KeyMsg:
		if s.list.FilterState() != list.Filtering {
			switch msg.String() {
			case "q", "esc", "left":
				return s, tea.Quit // Spaces is the root screen: back exits the app.
			case "enter", "right":
				if it, ok := s.list.SelectedItem().(spaceItem); ok {
					return s, push(newSpaceScreen(it))
				}
				return s, nil
			}
		}
	}
	var cmd tea.Cmd
	s.list, cmd = s.list.Update(msg)
	return s, cmd
}

func (s *spacesScreen) View(m *Model) string {
	if s.err != nil {
		return headerStyle.Render(errStyle.Render("Error: "+s.err.Error())) + "\n" + footerStyle.Render(footerHelp)
	}
	if !s.loaded {
		return headerStyle.Render("Loading spaces…")
	}
	return s.list.View() + "\n" + footerStyle.Render(footerHelp)
}
