package tui

import "testing"

func TestContactCard_DeleteWithNilDeleterIsNoop(t *testing.T) {
	s := newContactCardScreen(spaceItem{id: "fam"}, contactItem{id: "c1"})
	m := &Model{deleter: nil}
	ns, cmd := s.Update(m, key("delete"))
	if cmd != nil {
		t.Errorf("delete with nil deleter produced a command: %v", cmd)
	}
	if ns.(*contactCardScreen) != s {
		t.Error("expected the same screen back")
	}
}

func TestContactCard_DeleteSelfSetsFlash(t *testing.T) {
	s := newContactCardScreen(spaceItem{id: "fam"}, contactItem{id: "c1", isSelf: true})
	m := &Model{deleter: &fakeDeleter{}}
	ns, cmd := s.Update(m, key("delete"))
	if cmd != nil {
		t.Errorf("self delete produced a command: %v", cmd)
	}
	card := ns.(*contactCardScreen)
	if card.flash != "Cannot delete yourself" {
		t.Errorf("flash = %q, want the cannot-delete-yourself message", card.flash)
	}
	if v := card.View(&Model{height: 24}); !contains(v, "Cannot delete yourself") {
		t.Errorf("view = %q, want it to render the flash", v)
	}
}

func TestContactCard_UnmatchedKeyFallsThrough(t *testing.T) {
	s := newContactCardScreen(spaceItem{id: "fam"}, contactItem{id: "c1"})
	m := &Model{deleter: &fakeDeleter{}}
	ns, cmd := s.Update(m, key("x"))
	if cmd != nil {
		t.Errorf("unmatched key produced a command: %v", cmd)
	}
	if ns.(*contactCardScreen) != s {
		t.Error("expected the same screen back")
	}
}

func TestPad_LongLabelIsNotTruncated(t *testing.T) {
	label := "verylonglabel"
	if got := pad(label); got != label+" " {
		t.Errorf("pad(%q) = %q, want %q", label, got, label+" ")
	}
}
