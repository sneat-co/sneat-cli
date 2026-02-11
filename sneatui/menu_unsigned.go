package sneatui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var docStyle = lipgloss.NewStyle().Margin(1, 2)

// calculateDefaultSize computes safe default dimensions for a list given frame margins.
// It ensures minimum sizes are respected for usability.
func calculateDefaultSize(horizontalMargin, verticalMargin int) (width, height int) {
	width = 80 - horizontalMargin
	height = 24 - verticalMargin
	if width < 20 {
		width = 20
	}
	if height < 5 {
		height = 5
	}
	return width, height
}

type item struct {
	title, desc string
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title }

type menuUnsigned struct {
	list list.Model
}

func newMenuUnassigned() tea.Model {
	items := []list.Item{
		item{title: "Sign-in", desc: "Authorize to get access to your data"},
		item{title: "About", desc: "Learn about the Sneat.app"},
	}
	// Provide safe default size so items render even before any WindowSizeMsg arrives.
	// Typical terminal is at least 80x24; subtract docStyle margins to size the list.
	h, v := docStyle.GetFrameSize()
	defaultW, defaultH := calculateDefaultSize(h, v)
	m := menuUnsigned{
		list: list.New(items, list.NewDefaultDelegate(), defaultW, defaultH),
	}
	// Ensure title is always shown and filter bar hidden (we don't use it),
	// so the title never gets replaced by the filter input line.
	m.list.SetShowTitle(true)
	m.list.SetShowFilter(false)
	// Disable filtering entirely and reset filter state defensively to avoid replacing the title bar.
	m.list.SetFilteringEnabled(false)
	m.list.SetFilterState(list.Unfiltered)
	m.list.Title = "Sneat.app - main menu"
	return m
}

func (m menuUnsigned) Init() tea.Cmd {
	return nil
}

func (m menuUnsigned) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			// Navigate based on the selected item by emitting app-level messages
			if sel := m.list.SelectedItem(); sel != nil {
				if it, ok := sel.(item); ok {
					switch it.title {
					case "Sign-in":
						return m, func() tea.Msg { return navToLoginMsg{} }
					case "About":
						return m, func() tea.Msg { return navToAboutMsg{} }
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

func (m menuUnsigned) View() string {
	return docStyle.Render(m.list.View())
}
