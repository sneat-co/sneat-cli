package tui

import "testing"

func TestSpacesScreen_EnterWithNoSelectionIsNoop(t *testing.T) {
	s := newSpacesScreen()
	s.list.SetItems(nil)
	m := &Model{}
	ns, cmd := s.Update(m, key("enter"))
	if cmd != nil {
		t.Errorf("enter with no selection produced a command: %v", cmd)
	}
	if ns.(*spacesScreen) != s {
		t.Error("expected the same screen back")
	}
}

func TestSpacesScreen_LoadingView(t *testing.T) {
	s := newSpacesScreen()
	if v := s.View(&Model{}); !contains(v, "Loading spaces") {
		t.Errorf("view before load = %q, want the loading message", v)
	}
}
