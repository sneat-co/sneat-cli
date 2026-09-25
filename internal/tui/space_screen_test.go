package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSpaceScreen_WindowSizeMsg(t *testing.T) {
	s := newSpaceScreen(spaceItem{id: "fam"})
	m := &Model{height: 24}
	_, cmd := s.Update(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if cmd != nil {
		t.Errorf("resize produced a command: %v", cmd)
	}
	if s.menu.Width() != 100 {
		t.Errorf("menu width = %d, want 100", s.menu.Width())
	}
}

func TestSpaceScreen_EnterWithNoSelectionIsNoop(t *testing.T) {
	s := newSpaceScreen(spaceItem{id: "fam"})
	s.menu.SetItems(nil)
	m := &Model{}
	ns, cmd := s.Update(m, key("enter"))
	if cmd != nil {
		t.Errorf("enter with no selection produced a command: %v", cmd)
	}
	if ns.(*spaceScreen) != s {
		t.Error("expected the same screen back")
	}
}
