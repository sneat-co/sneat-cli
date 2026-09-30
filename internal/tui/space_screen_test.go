package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
	"github.com/tuigoff/tuigoff/pkg/nav"
	"github.com/tuigoff/tuigoff/pkg/widgets"
)

func TestSpaceScreen_WindowSizeMsg(t *testing.T) {
	s := newSpaceScreen(&app{cache: map[string][]firestoredb.Contact{}}, spaceItem{id: "fam"})
	_, cmd := s.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if cmd != nil {
		t.Errorf("resize produced a command: %v", cmd)
	}
	if s.w != 100 || s.h != 30 {
		t.Errorf("size = %dx%d, want 100x30", s.w, s.h)
	}
}

func TestSpaceScreen_EnterWithNoSelectionIsNoop(t *testing.T) {
	s := newSpaceScreen(&app{cache: map[string][]firestoredb.Contact{}}, spaceItem{id: "fam"})
	s.menu.SetItems()
	s.menu.Focus()
	ns, cmd := s.Update(widgets.ItemSelectedMsg{Item: nil})
	if cmd != nil {
		t.Errorf("enter with no selection produced a command: %v", cmd)
	}
	if ns.(*spaceScreen) != s {
		t.Error("expected the same screen back")
	}
}

func TestSpaceScreen_FocusAndEsc(t *testing.T) {
	s := newSpaceScreen(&app{cache: map[string][]firestoredb.Contact{}}, spaceItem{id: "fam", title: "Family"})
	s.Update(nav.ScreenFocusMsg{Focused: true})
	_, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should pop")
	}
	if _, ok := cmd().(nav.PopMsg); !ok {
		t.Fatalf("esc cmd = %T, want PopMsg", cmd())
	}
}

func TestSpaceScreen_CacheInit(t *testing.T) {
	a := &app{cache: map[string][]firestoredb.Contact{
		"fam": {contact("c1", "Al", "member")},
	}}
	s := newSpaceScreen(a, spaceItem{id: "fam"})
	if cmd := s.Init(); cmd != nil {
		t.Errorf("cached Init should not load, got %v", cmd)
	}
	if !s.loaded || s.count != 1 {
		t.Errorf("loaded=%v count=%d", s.loaded, s.count)
	}
}

func TestSpaceScreen_ErrMsg(t *testing.T) {
	s := newSpaceScreen(&app{cache: map[string][]firestoredb.Contact{}}, spaceItem{id: "fam"})
	s.w, s.h = 40, 20
	s.Update(errMsg{err: errors.New("net")})
	if s.err == nil || !s.loaded {
		t.Fatal("expected error state")
	}
	if v := s.View(); !contains(v, "net") {
		t.Errorf("view = %q", v)
	}
}

func TestSpaceScreen_LayoutWithError(t *testing.T) {
	s := newSpaceScreen(&app{cache: map[string][]firestoredb.Contact{}}, spaceItem{id: "fam"})
	s.err = errors.New("x")
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if s.h != 24 {
		t.Fatalf("h = %d", s.h)
	}
}

func TestSpaceScreen_SelectMembers(t *testing.T) {
	s := newSpaceScreen(&app{cache: map[string][]firestoredb.Contact{}}, spaceItem{id: "fam"})
	s.loaded = true
	s.menu.SetItems(s.menuItems()...)
	s.menu.Focus()
	_, cmd := s.Update(widgets.ItemSelectedMsg{
		Item: widgets.MenuItem{ID: "members", Label: "Members", Ref: true},
	})
	if cmd == nil {
		t.Fatal("selecting Members should push")
	}
	if _, ok := cmd().(nav.PushMsg); !ok {
		t.Fatalf("cmd = %T, want PushMsg", cmd())
	}
}
