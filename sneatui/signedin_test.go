package sneatui

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestLogin_EnterOnPassword_NavigatesToSignedMenu(t *testing.T) {
	m := InitialModel().(appModel)
	// Enter on Sign-in to go to login
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	am := model.(appModel)
	if cmd != nil {
		msg := cmd()
		model, _ = am.Update(msg)
		am = model.(appModel)
	}
	if am.active != "login" {
		t.Fatalf("expected to be on login, got %q", am.active)
	}
	// Press Enter twice: first moves to password, second submits
	model, _ = am.Update(tea.KeyMsg{Type: tea.KeyEnter})
	am = model.(appModel)
	model, cmd = am.Update(tea.KeyMsg{Type: tea.KeyEnter})
	am = model.(appModel)
	if cmd != nil {
		msg := cmd()
		model, _ = am.Update(msg)
		am = model.(appModel)
	}
	if am.active != "signed" {
		t.Fatalf("after login submit, active=%q, want signed", am.active)
	}
	if !strings.Contains(am.View(), "Sign-out") {
		t.Fatalf("signed-in menu view does not contain Sign-out")
	}
}

func TestSignedIn_SignOut_ReturnsUnsigned(t *testing.T) {
	// Move to signed-in first
	m := InitialModel().(appModel)
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	am := model.(appModel)
	if cmd != nil {
		msg := cmd()
		model, _ = am.Update(msg)
		am = model.(appModel)
	}
	// Enter twice to submit login
	model, _ = am.Update(tea.KeyMsg{Type: tea.KeyEnter})
	am = model.(appModel)
	model, cmd = am.Update(tea.KeyMsg{Type: tea.KeyEnter})
	am = model.(appModel)
	if cmd != nil {
		msg := cmd()
		model, _ = am.Update(msg)
		am = model.(appModel)
	}
	if am.active != "signed" {
		t.Fatalf("expected signed before sign-out, got %q", am.active)
	}
	// Now select Sign-out (default index 0 is Calendar; move cursor to last index)
	// We'll repeatedly send down keys to reach Sign-out
	for i := 0; i < 3; i++ { // 3 moves from 0 -> 3
		model, _ = am.Update(tea.KeyMsg{Type: tea.KeyDown})
		am = model.(appModel)
	}
	model, cmd = am.Update(tea.KeyMsg{Type: tea.KeyEnter})
	am = model.(appModel)
	if cmd != nil {
		msg := cmd()
		model, _ = am.Update(msg)
		am = model.(appModel)
	}
	if am.active != "unsigned" {
		t.Fatalf("after Sign-out, active=%q, want unsigned", am.active)
	}
}

func TestMenuSignedIn_Init(t *testing.T) {
	m := newMenuSignedIn().(menuSignedIn)
	cmd := m.Init()
	if cmd != nil {
		t.Fatalf("Init() returned non-nil cmd, want nil")
	}
}

func TestMenuSignedIn_CtrlC_Quits(t *testing.T) {
	m := newMenuSignedIn().(menuSignedIn)
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if _, ok := model.(menuSignedIn); !ok {
		t.Fatalf("Update returned %T, want menuSignedIn", model)
	}
	if cmd == nil {
		t.Fatalf("cmd is nil, want tea.Quit")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("cmd() returned %T, want tea.QuitMsg", msg)
	}
}

func TestMenuSignedIn_EnterOnNonSignOut_DoesNotNavigate(t *testing.T) {
	m := newMenuSignedIn().(menuSignedIn)
	// Select first item (Calendar)
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if _, ok := model.(menuSignedIn); !ok {
		t.Fatalf("Update returned %T, want menuSignedIn", model)
	}
	// cmd should not be a navigation command (would return navSignOutMsg if Sign-out was selected)
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(navSignOutMsg); ok {
			t.Fatalf("selecting Calendar returned navSignOutMsg")
		}
	}
}

func TestMenuSignedIn_WindowSize_AdjustsListSize(t *testing.T) {
	m := newMenuSignedIn().(menuSignedIn)
	hMargin, vMargin := docStyle.GetFrameSize()
	msg := tea.WindowSizeMsg{Width: 100, Height: 40}
	model, _ := m.Update(msg)
	sm := model.(menuSignedIn)
	wantW := msg.Width - hMargin
	wantH := msg.Height - vMargin
	if sm.list.Width() != wantW || sm.list.Height() != wantH {
		t.Fatalf("list size = (%d,%d), want (%d,%d)", sm.list.Width(), sm.list.Height(), wantW, wantH)
	}
}
