package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
	"github.com/strongo/strongo-tui/pkg/nav"
)

func TestConfirmScreen_Title(t *testing.T) {
	s := newConfirmDeleteScreen(&app{}, spaceItem{id: "fam"}, contactItem{id: "c1"}, false)
	if got := s.Title(); got != "Delete contact" {
		t.Errorf("Title() = %q, want %q", got, "Delete contact")
	}
}

func TestConfirmScreen_Init(t *testing.T) {
	s := newConfirmDeleteScreen(&app{}, spaceItem{id: "fam"}, contactItem{id: "c1"}, false)
	if cmd := s.Init(); cmd != nil {
		t.Errorf("Init() = %v, want nil", cmd)
	}
}

func TestConfirmScreen_IgnoresInputWhileDeleting(t *testing.T) {
	s := newConfirmDeleteScreen(&app{deleter: &fakeDeleter{}}, spaceItem{id: "fam"}, contactItem{id: "c1"}, false)
	s.deleting = true
	ns, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Errorf("Update while deleting produced a command: %v", cmd)
	}
	if ns.(*confirmDeleteScreen) != s || !s.deleting {
		t.Error("state should be unchanged while a delete is in flight")
	}
}

func TestConfirmScreen_NilDeleterPopsInstead(t *testing.T) {
	s := newConfirmDeleteScreen(&app{deleter: nil}, spaceItem{id: "fam"}, contactItem{id: "c1"}, false)
	_, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a pop command when the model has no deleter")
	}
	if _, ok := cmd().(nav.PopMsg); !ok {
		t.Errorf("cmd produced %T, want PopMsg", cmd())
	}
}

func TestConfirmScreen_UnmatchedKeyFallsThrough(t *testing.T) {
	s := newConfirmDeleteScreen(&app{deleter: &fakeDeleter{}}, spaceItem{id: "fam"}, contactItem{id: "c1"}, false)
	ns, cmd := s.Update(tea.KeyPressMsg{Text: "x"})
	if cmd != nil {
		t.Errorf("unmatched key produced a command: %v", cmd)
	}
	if ns.(*confirmDeleteScreen) != s {
		t.Error("unmatched key should return the same screen")
	}
}

func TestConfirmScreen_NonKeyMsgFallsThrough(t *testing.T) {
	s := newConfirmDeleteScreen(&app{deleter: &fakeDeleter{}}, spaceItem{id: "fam"}, contactItem{id: "c1"}, false)
	ns, cmd := s.Update(tea.WindowSizeMsg{Width: 10, Height: 10})
	if cmd != nil {
		t.Errorf("non-key msg produced a command: %v", cmd)
	}
	if ns.(*confirmDeleteScreen) != s {
		t.Error("non-key msg should return the same screen")
	}
	if s.w != 10 || s.h != 10 {
		t.Errorf("size = %dx%d", s.w, s.h)
	}
}

func TestConfirmScreen_ViewWhileDeleting(t *testing.T) {
	s := newConfirmDeleteScreen(&app{}, spaceItem{id: "fam"}, contactItem{id: "c1", title: "Alice"}, false)
	s.deleting = true
	s.w, s.h = 40, 20
	if v := s.View(); !contains(v, "Deleting…") {
		t.Errorf("view while deleting = %q, want it to mention Deleting…", v)
	}
}

func TestConfirmScreen_ViewWithError(t *testing.T) {
	s := newConfirmDeleteScreen(&app{}, spaceItem{id: "fam"}, contactItem{id: "c1", title: "Alice"}, false)
	s.err = errors.New("api down")
	s.w, s.h = 40, 20
	if v := s.View(); !contains(v, "api down") {
		t.Errorf("view with error = %q, want it to mention the error", v)
	}
}

func TestConfirmScreen_DeleteErrMsg(t *testing.T) {
	s := newConfirmDeleteScreen(&app{}, spaceItem{id: "fam"}, contactItem{id: "c1"}, false)
	s.deleting = true
	s.Update(deleteErrMsg{err: errors.New("fail")})
	if s.deleting || s.err == nil {
		t.Errorf("deleting=%v err=%v", s.deleting, s.err)
	}
}

func TestConfirmScreen_ContactDeletedClearsCache(t *testing.T) {
	a := &app{cache: map[string][]firestoredb.Contact{"fam": {contact("c1", "Al")}}, contacts: &fakeContacts{}}
	s := newConfirmDeleteScreen(a, spaceItem{id: "fam"}, contactItem{id: "c1"}, true)
	_, cmd := s.Update(contactDeletedMsg{spaceID: "fam", contactID: "c1"})
	if _, ok := a.cache["fam"]; ok {
		t.Error("cache should be cleared")
	}
	if cmd == nil {
		t.Fatal("expected pop+reload cmds")
	}
}

func TestConfirmScreen_ShortHelp(t *testing.T) {
	s := newConfirmDeleteScreen(&app{}, spaceItem{}, contactItem{}, false)
	if len(s.ShortHelp()) != 2 {
		t.Errorf("ShortHelp = %v", s.ShortHelp())
	}
}

func TestConfirmScreen_EscPops(t *testing.T) {
	s := newConfirmDeleteScreen(&app{}, spaceItem{}, contactItem{}, false)
	_, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if _, ok := cmd().(nav.PopMsg); !ok {
		t.Fatalf("cmd = %T", cmd())
	}
}

func TestConfirmScreen_EnterDeletes(t *testing.T) {
	del := &fakeDeleter{}
	s := newConfirmDeleteScreen(&app{deleter: del}, spaceItem{id: "fam"}, contactItem{id: "c1"}, false)
	_, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !s.deleting {
		t.Error("should be deleting")
	}
	msg := cmd()
	if _, ok := msg.(contactDeletedMsg); !ok {
		t.Fatalf("cmd = %T, want contactDeletedMsg", msg)
	}
	if len(del.calls) != 1 {
		t.Fatalf("calls = %v", del.calls)
	}
}
