package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/tuigoff/tuigoff/pkg/nav"
	"github.com/tuigoff/tuigoff/pkg/widgets"
)

func TestSpacesScreen_EnterWithNoSelectionIsNoop(t *testing.T) {
	s := newSpacesScreen(&app{})
	s.list.SetItems()
	s.list.Focus()
	ns, cmd := s.Update(widgets.ItemSelectedMsg{Item: nil})
	if cmd != nil {
		t.Errorf("enter with no selection produced a command: %v", cmd)
	}
	if ns.(*spacesScreen) != s {
		t.Error("expected the same screen back")
	}
}

func TestSpacesScreen_LoadingView(t *testing.T) {
	s := newSpacesScreen(&app{})
	s.w, s.h = 40, 10
	if v := s.View(); !contains(v, "Loading spaces") {
		t.Errorf("view before load = %q, want the loading message", v)
	}
}

func TestSpacesScreen_ErrView(t *testing.T) {
	s := newSpacesScreen(&app{})
	s.w, s.h = 40, 10
	s.Update(errMsg{err: errors.New("boom")})
	if v := s.View(); !contains(v, "boom") {
		t.Errorf("error view = %q", v)
	}
}

func TestSpacesScreen_FocusAndResize(t *testing.T) {
	s := newSpacesScreen(&app{})
	s.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	s.Update(nav.ScreenFocusMsg{Focused: true})
	if !s.list.Focused() {
		t.Error("list should be focused")
	}
	s.Update(nav.ScreenFocusMsg{Focused: false})
	if s.list.Focused() {
		t.Error("list should be blurred")
	}
}

func TestSpacesScreen_InitLoads(t *testing.T) {
	s := newSpacesScreen(&app{spaces: fakeSpaces{spaces: twoSpaces()}, uid: "u"})
	cmd := s.Init()
	if cmd == nil {
		t.Fatal("Init should load spaces")
	}
	msg := cmd()
	if _, ok := msg.(spacesLoadedMsg); !ok {
		t.Fatalf("Init cmd = %T, want spacesLoadedMsg", msg)
	}
}
