package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestContactsScreen_ErrMsgSetsErrAndView(t *testing.T) {
	s := newContactsScreen(spaceItem{id: "fam"}, false)
	m := &Model{}
	ns, cmd := s.Update(m, errMsg{err: errors.New("net down")})
	if cmd != nil {
		t.Errorf("errMsg produced a command: %v", cmd)
	}
	cs := ns.(*contactsScreen)
	if cs.err == nil || !cs.loaded {
		t.Errorf("err = %v, loaded = %v, want an error and loaded=true", cs.err, cs.loaded)
	}
	if v := cs.View(m); !contains(v, "net down") {
		t.Errorf("view = %q, want it to mention the error", v)
	}
}

func TestContactsScreen_WindowSizeMsg(t *testing.T) {
	s := newContactsScreen(spaceItem{id: "fam"}, false)
	m := &Model{height: 24}
	_, cmd := s.Update(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if cmd != nil {
		t.Errorf("resize produced a command: %v", cmd)
	}
	if s.list.Width() != 100 {
		t.Errorf("list width = %d, want 100", s.list.Width())
	}
}

func TestContactsScreen_EnterWithNoSelectionIsNoop(t *testing.T) {
	s := newContactsScreen(spaceItem{id: "fam"}, false)
	s.list.SetItems(nil)
	m := &Model{}
	ns, cmd := s.Update(m, key("enter"))
	if cmd != nil {
		t.Errorf("enter with no selection produced a command: %v", cmd)
	}
	if ns.(*contactsScreen) != s {
		t.Error("expected the same screen back")
	}
}

func TestContactsScreen_DeleteWithNoSelectionIsNoop(t *testing.T) {
	s := newContactsScreen(spaceItem{id: "fam"}, false)
	s.list.SetItems(nil)
	m := &Model{deleter: &fakeDeleter{}}
	ns, cmd := s.Update(m, key("delete"))
	if cmd != nil {
		t.Errorf("delete with no selection produced a command: %v", cmd)
	}
	if ns.(*contactsScreen) != s {
		t.Error("expected the same screen back")
	}
}

func TestContactsScreen_FallsThroughToListUpdate(t *testing.T) {
	s := newContactsScreen(spaceItem{id: "fam"}, false)
	m := &Model{}
	// A key that the list itself handles (e.g. "j" for down) falls past the
	// switch to the list's own Update, rather than being matched above.
	ns, _ := s.Update(m, key("j"))
	if ns.(*contactsScreen) != s {
		t.Error("expected the same screen back")
	}
}

func TestContactsScreen_LoadingView(t *testing.T) {
	s := newContactsScreen(spaceItem{id: "fam"}, false)
	if v := s.View(&Model{}); !contains(v, "Loading contacts") {
		t.Errorf("view before load = %q, want the loading message", v)
	}
}
