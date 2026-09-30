package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/strongo/strongo-tui/pkg/nav"
)

func TestContactCard_DeleteWithNilDeleterIsNoop(t *testing.T) {
	s := newContactCardScreen(&app{deleter: nil}, spaceItem{id: "fam"}, contactItem{id: "c1"})
	ns, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if cmd != nil {
		t.Errorf("delete with nil deleter produced a command: %v", cmd)
	}
	if ns.(*contactCardScreen) != s {
		t.Error("expected the same screen back")
	}
}

func TestContactCard_DeleteSelfAlerts(t *testing.T) {
	s := newContactCardScreen(&app{deleter: &fakeDeleter{}}, spaceItem{id: "fam"}, contactItem{id: "c1", isSelf: true})
	_, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if cmd == nil {
		t.Fatal("self delete should alert")
	}
	if _, ok := cmd().(nav.AlertMsg); !ok {
		t.Fatalf("cmd = %T, want AlertMsg", cmd())
	}
}

func TestContactCard_UnmatchedKeyFallsThrough(t *testing.T) {
	s := newContactCardScreen(&app{deleter: &fakeDeleter{}}, spaceItem{id: "fam"}, contactItem{id: "c1"})
	ns, cmd := s.Update(tea.KeyPressMsg{Text: "x"})
	if cmd != nil {
		t.Errorf("unmatched key produced a command: %v", cmd)
	}
	if ns.(*contactCardScreen) != s {
		t.Error("unmatched key should return the same screen")
	}
}

func TestPad_LongLabelIsNotTruncated(t *testing.T) {
	label := "verylonglabel"
	if got := pad(label); got != label+" " {
		t.Errorf("pad(%q) = %q, want %q", label, got, label+" ")
	}
}

func TestContactCard_EscPops(t *testing.T) {
	s := newContactCardScreen(&app{}, spaceItem{id: "fam"}, contactItem{id: "c1", title: "Alice"})
	_, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should pop")
	}
	if _, ok := cmd().(nav.PopMsg); !ok {
		t.Fatalf("cmd = %T, want PopMsg", cmd())
	}
}

func TestContactCard_DeleteOpensConfirm(t *testing.T) {
	s := newContactCardScreen(&app{deleter: &fakeDeleter{}}, spaceItem{id: "fam"}, contactItem{id: "c1", title: "Alice"})
	_, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if cmd == nil {
		t.Fatal("delete should push confirm")
	}
	msg := cmd().(nav.PushMsg)
	if _, ok := msg.Page.Content.(*confirmDeleteScreen); !ok {
		t.Fatalf("pushed %T, want *confirmDeleteScreen", msg.Page.Content)
	}
}

func TestContactCard_ViewAndHelp(t *testing.T) {
	s := newContactCardScreen(&app{deleter: &fakeDeleter{}}, spaceItem{id: "fam"}, contactItem{
		id: "c1", title: "Alice", ctype: "person", roles: []string{"parent"},
	})
	s.w, s.h = 40, 20
	if v := s.View(); !contains(v, "Alice") || !contains(v, "parent") {
		t.Errorf("view = %q", v)
	}
	if len(s.ShortHelp()) < 2 {
		t.Errorf("ShortHelp = %v", s.ShortHelp())
	}
	if cmd := s.Init(); cmd != nil {
		t.Errorf("Init = %v", cmd)
	}
}
