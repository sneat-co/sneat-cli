package sneatui

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestUnsigned_EnterOnSignIn_OpensLoginViaApp(t *testing.T) {
	m := InitialModel().(appModel)
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	am := model.(appModel)
	if cmd != nil {
		msg := cmd()
		model, _ = am.Update(msg)
		am = model.(appModel)
	}
	if am.active != "login" {
		t.Fatalf("after Enter on Sign-in, active=%q, want login", am.active)
	}
}

func TestLogin_EscReturnsToUnsignedMenu_ViaApp(t *testing.T) {
	m := InitialModel().(appModel)
	// Go to login
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	am := model.(appModel)
	if cmd != nil {
		msg := cmd()
		model, _ = am.Update(msg)
		am = model.(appModel)
	}
	// Send ESC from login
	model, cmd = am.Update(tea.KeyMsg{Type: tea.KeyEsc})
	am = model.(appModel)
	if cmd != nil {
		msg := cmd()
		model, _ = am.Update(msg)
		am = model.(appModel)
	}
	if am.active != "unsigned" {
		t.Fatalf("after ESC from login, active=%q, want unsigned", am.active)
	}
}

func TestLogin_ViewNotEmpty(t *testing.T) {
	m := newLoginModel().(loginModel)
	if m.View() == "" {
		t.Fatalf("login view is empty")
	}
}

func TestUnsigned_SelectionPreserved_AfterLoginEsc(t *testing.T) {
	am := InitialModel().(appModel)
	// Move selection down
	model, _ := am.Update(tea.KeyMsg{Type: tea.KeyDown})
	am = model.(appModel)
	idxBefore := am.unsigned.list.Index()
	// Enter to open login
	model, cmd := am.Update(tea.KeyMsg{Type: tea.KeyEnter})
	am = model.(appModel)
	if cmd != nil {
		msg := cmd()
		model, _ = am.Update(msg)
		am = model.(appModel)
	}
	// ESC back
	model, cmd = am.Update(tea.KeyMsg{Type: tea.KeyEsc})
	am = model.(appModel)
	if cmd != nil {
		msg := cmd()
		model, _ = am.Update(msg)
		am = model.(appModel)
	}
	if am.unsigned.list.Index() != idxBefore {
		t.Fatalf("selection index after return = %d, want %d", am.unsigned.list.Index(), idxBefore)
	}
	view := am.View()
	if !strings.Contains(view, "Sneat.app - main menu") || !strings.Contains(view, "Sign-in") {
		t.Fatalf("menu view after return missing expected content; view=\n%s", view)
	}
}

func TestLogin_Init_ReturnsBlink(t *testing.T) {
	m := newLoginModel().(loginModel)
	cmd := m.Init()
	if cmd == nil {
		t.Fatalf("Init() returned nil, want textinput.Blink")
	}
}

func TestLogin_Tab_SwitchesFromEmailToPassword(t *testing.T) {
	m := newLoginModel().(loginModel)
	if m.focused != 0 {
		t.Fatalf("initial focused = %d, want 0", m.focused)
	}
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = model.(loginModel)
	if m.focused != 1 {
		t.Fatalf("focused after Tab = %d, want 1", m.focused)
	}
}

func TestLogin_ShiftTab_SwitchesFromPasswordToEmail(t *testing.T) {
	m := newLoginModel().(loginModel)
	// First move to password
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = model.(loginModel)
	if m.focused != 1 {
		t.Fatalf("focused after Tab = %d, want 1", m.focused)
	}
	// Now shift+tab back
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = model.(loginModel)
	if m.focused != 0 {
		t.Fatalf("focused after Shift+Tab = %d, want 0", m.focused)
	}
}

func TestLogin_WindowSize_AdjustsInputWidth(t *testing.T) {
	m := newLoginModel().(loginModel)
	model, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = model.(loginModel)
	if m.winW != 100 || m.winH != 40 {
		t.Fatalf("window size = (%d,%d), want (100,40)", m.winW, m.winH)
	}
}

func TestLogin_WindowSize_SmallWidth_UsesMinimum(t *testing.T) {
	m := newLoginModel().(loginModel)
	model, _ := m.Update(tea.WindowSizeMsg{Width: 10, Height: 40})
	m = model.(loginModel)
	if m.email.Width < 20 || m.password.Width < 20 {
		t.Fatalf("input widths (%d,%d) should be at least 20", m.email.Width, m.password.Width)
	}
}

func TestLogin_OtherKeys_ForwardedToActiveInput(t *testing.T) {
	m := newLoginModel().(loginModel)
	// Type something in email
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = model.(loginModel)
	if m.email.Value() == "" {
		t.Fatalf("email value is empty, want 'a'")
	}
	// Move to password and type
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = model.(loginModel)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = model.(loginModel)
	if m.password.Value() == "" {
		t.Fatalf("password value is empty, want 'b'")
	}
}
