package sneatui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// menuSignedIn represents the main menu for a signed-in user.
type menuSignedIn struct {
	list list.Model
}

func newMenuSignedIn() tea.Model {
	items := []list.Item{
		item{title: "Calendar", desc: "View your calendar"},
		item{title: "Members", desc: "Manage members"},
		item{title: "Lists", desc: "See your lists"},
		item{title: "Sign-out", desc: "Return to unsigned menu"},
	}
	// Use similar safe defaults and styling as unsigned menu
	h, v := docStyle.GetFrameSize()
	defaultW, defaultH := calculateDefaultSize(h, v)
	m := menuSignedIn{list: list.New(items, list.NewDefaultDelegate(), defaultW, defaultH)}
	m.list.SetShowTitle(true)
	m.list.SetShowFilter(false)
	m.list.SetFilteringEnabled(false)
	m.list.SetFilterState(list.Unfiltered)
	m.list.Title = "Sneat.app - menu"
	return m
}

func (m menuSignedIn) Init() tea.Cmd { return nil }

func (m menuSignedIn) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			if sel := m.list.SelectedItem(); sel != nil {
				if it, ok := sel.(item); ok {
					if it.title == "Sign-out" {
						return m, func() tea.Msg { return navSignOutMsg{} }
					}
				}
			}
		}
	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m menuSignedIn) View() string {
	return docStyle.Render(m.list.View())
}
