package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/sneat-co/sneat-cli/internal/firestoredb"
	"github.com/strongo/strongo-tui/pkg/nav"
	"github.com/strongo/strongo-tui/pkg/widgets"
)

func TestContactsScreen_ErrMsgSetsErrAndView(t *testing.T) {
	s := newContactsScreen(&app{}, spaceItem{id: "fam"}, false)
	s.w, s.h = 40, 10
	ns, cmd := s.Update(errMsg{err: errors.New("net down")})
	if cmd != nil {
		t.Errorf("errMsg produced a command: %v", cmd)
	}
	cs := ns.(*contactsScreen)
	if cs.err == nil || !cs.loaded {
		t.Errorf("err = %v, loaded = %v, want an error and loaded=true", cs.err, cs.loaded)
	}
	if v := cs.View(); !contains(v, "net down") {
		t.Errorf("view = %q, want it to mention the error", v)
	}
}

func TestContactsScreen_WindowSizeMsg(t *testing.T) {
	s := newContactsScreen(&app{}, spaceItem{id: "fam"}, false)
	_, cmd := s.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if cmd != nil {
		t.Errorf("resize produced a command: %v", cmd)
	}
	if s.w != 100 || s.h != 30 {
		t.Errorf("size = %dx%d", s.w, s.h)
	}
}

func TestContactsScreen_EnterWithNoSelectionIsNoop(t *testing.T) {
	s := newContactsScreen(&app{}, spaceItem{id: "fam"}, false)
	s.list.SetItems()
	s.list.Focus()
	ns, cmd := s.Update(widgets.ItemSelectedMsg{Item: nil})
	if cmd != nil {
		t.Errorf("enter with no selection produced a command: %v", cmd)
	}
	if ns.(*contactsScreen) != s {
		t.Error("expected the same screen back")
	}
}

func TestContactsScreen_DeleteWithNoSelectionIsNoop(t *testing.T) {
	s := newContactsScreen(&app{deleter: &fakeDeleter{}}, spaceItem{id: "fam"}, false)
	s.list.SetItems()
	s.list.Focus()
	ns, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if cmd != nil {
		t.Errorf("delete with no selection produced a command: %v", cmd)
	}
	if ns.(*contactsScreen) != s {
		t.Error("expected the same screen back")
	}
}

func TestContactsScreen_LoadingView(t *testing.T) {
	s := newContactsScreen(&app{}, spaceItem{id: "fam"}, false)
	s.w, s.h = 40, 10
	if v := s.View(); !contains(v, "Loading contacts") {
		t.Errorf("view before load = %q, want the loading message", v)
	}
}

func TestContactsScreen_CacheInit(t *testing.T) {
	a := &app{
		uid: "u",
		cache: map[string][]firestoredb.Contact{
			"fam": {contact("c1", "Al", "member")},
		},
	}
	s := newContactsScreen(a, spaceItem{id: "fam"}, false)
	if cmd := s.Init(); cmd != nil {
		t.Errorf("cached Init should not load, got %v", cmd)
	}
	if !s.loaded || len(s.list.Items()) != 1 {
		t.Errorf("loaded=%v items=%d", s.loaded, len(s.list.Items()))
	}
}

func TestContactsScreen_FocusAndShortHelp(t *testing.T) {
	s := newContactsScreen(&app{deleter: &fakeDeleter{}}, spaceItem{id: "fam"}, false)
	s.Update(nav.ScreenFocusMsg{Focused: true})
	if !s.list.Focused() {
		t.Error("list should be focused")
	}
	if len(s.ShortHelp()) < 2 {
		t.Errorf("ShortHelp = %v", s.ShortHelp())
	}
	s.Update(nav.ScreenFocusMsg{Focused: false})
	if s.list.Focused() {
		t.Error("list should be blurred")
	}
}

func TestContactsScreen_InitLoadsWhenUncached(t *testing.T) {
	fc := &fakeContacts{bySpace: map[string][]firestoredb.Contact{
		"fam": {contact("c1", "Al", "member")},
	}}
	s := newContactsScreen(&app{contacts: fc}, spaceItem{id: "fam"}, false)
	cmd := s.Init()
	if cmd == nil {
		t.Fatal("uncached Init should load")
	}
	msg := cmd()
	loaded, ok := msg.(contactsLoadedMsg)
	if !ok || loaded.spaceID != "fam" || len(loaded.contacts) != 1 {
		t.Fatalf("msg = %#v", msg)
	}
}

func TestContactsScreen_SelectOpensCard(t *testing.T) {
	s := newContactsScreen(&app{}, spaceItem{id: "fam"}, false)
	s.list.Focus()
	ci := contactItem{id: "c1", title: "Alice"}
	_, cmd := s.Update(widgets.ItemSelectedMsg{
		Item: widgets.MenuItem{ID: "c1", Label: "Alice", Ref: ci},
	})
	if cmd == nil {
		t.Fatal("selecting a contact should push")
	}
	if _, ok := cmd().(nav.PushMsg); !ok {
		t.Fatalf("cmd = %T, want PushMsg", cmd())
	}
}
