package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestConfirmScreen_Title(t *testing.T) {
	s := newConfirmDeleteScreen(spaceItem{id: "fam"}, contactItem{id: "c1"})
	if got := s.Title(); got != "Delete contact" {
		t.Errorf("Title() = %q, want %q", got, "Delete contact")
	}
}

func TestConfirmScreen_Init(t *testing.T) {
	s := newConfirmDeleteScreen(spaceItem{id: "fam"}, contactItem{id: "c1"})
	if cmd := s.Init(&Model{}); cmd != nil {
		t.Errorf("Init() = %v, want nil", cmd)
	}
}

func TestConfirmScreen_IgnoresInputWhileDeleting(t *testing.T) {
	s := newConfirmDeleteScreen(spaceItem{id: "fam"}, contactItem{id: "c1"})
	s.deleting = true
	m := &Model{deleter: &fakeDeleter{}}
	ns, cmd := s.Update(m, key("enter"))
	if cmd != nil {
		t.Errorf("Update while deleting produced a command: %v", cmd)
	}
	if ns.(*confirmDeleteScreen) != s || !s.deleting {
		t.Error("state should be unchanged while a delete is in flight")
	}
}

func TestConfirmScreen_NilDeleterPopsInstead(t *testing.T) {
	s := newConfirmDeleteScreen(spaceItem{id: "fam"}, contactItem{id: "c1"})
	m := &Model{deleter: nil}
	_, cmd := s.Update(m, key("enter"))
	if cmd == nil {
		t.Fatal("expected a pop command when the model has no deleter")
	}
	if _, ok := runCmd(cmd).(popMsg); !ok {
		t.Errorf("cmd produced %T, want popMsg", runCmd(cmd))
	}
}

func TestConfirmScreen_UnmatchedKeyFallsThrough(t *testing.T) {
	s := newConfirmDeleteScreen(spaceItem{id: "fam"}, contactItem{id: "c1"})
	m := &Model{deleter: &fakeDeleter{}}
	ns, cmd := s.Update(m, key("x"))
	if cmd != nil {
		t.Errorf("unmatched key produced a command: %v", cmd)
	}
	if ns.(*confirmDeleteScreen) != s {
		t.Error("unmatched key should return the same screen")
	}
}

func TestConfirmScreen_NonKeyMsgFallsThrough(t *testing.T) {
	s := newConfirmDeleteScreen(spaceItem{id: "fam"}, contactItem{id: "c1"})
	m := &Model{deleter: &fakeDeleter{}}
	ns, cmd := s.Update(m, tea.WindowSizeMsg{Width: 10, Height: 10})
	if cmd != nil {
		t.Errorf("non-key msg produced a command: %v", cmd)
	}
	if ns.(*confirmDeleteScreen) != s {
		t.Error("non-key msg should return the same screen")
	}
}

func TestConfirmScreen_ViewWhileDeleting(t *testing.T) {
	s := newConfirmDeleteScreen(spaceItem{id: "fam"}, contactItem{id: "c1", title: "Alice"})
	s.deleting = true
	m := &Model{height: 24}
	if v := s.View(m); !contains(v, "Deleting…") {
		t.Errorf("view while deleting = %q, want it to mention Deleting…", v)
	}
}

func TestConfirmScreen_ViewWithError(t *testing.T) {
	s := newConfirmDeleteScreen(spaceItem{id: "fam"}, contactItem{id: "c1", title: "Alice"})
	s.err = errors.New("api down")
	m := &Model{height: 24}
	if v := s.View(m); !contains(v, "api down") {
		t.Errorf("view with error = %q, want it to mention the error", v)
	}
}
