package sneatui

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestAppModel_Init(t *testing.T) {
	m := newAppModel().(appModel)
	cmd := m.Init()
	if cmd != nil {
		t.Fatalf("Init() returned non-nil cmd, want nil")
	}
}

func TestAppModel_Update_WindowSize_WithoutPriorSize(t *testing.T) {
	m := newAppModel().(appModel)
	msg := tea.WindowSizeMsg{Width: 100, Height: 40}
	model, _ := m.Update(msg)
	am := model.(appModel)
	if am.winW != 100 || am.winH != 40 {
		t.Fatalf("window size = (%d,%d), want (100,40)", am.winW, am.winH)
	}
}

func TestAppModel_Update_NavToLogin_WithWindowSize(t *testing.T) {
	m := newAppModel().(appModel)
	// Set window size first
	model, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	am := model.(appModel)
	// Navigate to login
	model, cmd := am.Update(navToLoginMsg{})
	am = model.(appModel)
	if am.active != "login" {
		t.Fatalf("active = %q, want login", am.active)
	}
	// Should return a cmd to propagate window size
	if cmd == nil {
		t.Fatalf("cmd is nil, want window size cmd")
	}
	msg := cmd()
	if wsm, ok := msg.(tea.WindowSizeMsg); !ok || wsm.Width != 100 || wsm.Height != 40 {
		t.Fatalf("cmd returned %v, want WindowSizeMsg{100,40}", msg)
	}
}

func TestAppModel_Update_NavToLogin_WithoutWindowSize(t *testing.T) {
	m := newAppModel().(appModel)
	model, cmd := m.Update(navToLoginMsg{})
	am := model.(appModel)
	if am.active != "login" {
		t.Fatalf("active = %q, want login", am.active)
	}
	if cmd != nil {
		t.Fatalf("cmd is not nil when no window size set, want nil")
	}
}

func TestAppModel_Update_NavToAbout_WithWindowSize(t *testing.T) {
	m := newAppModel().(appModel)
	model, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	am := model.(appModel)
	model, cmd := am.Update(navToAboutMsg{})
	am = model.(appModel)
	if am.active != "about" {
		t.Fatalf("active = %q, want about", am.active)
	}
	if cmd == nil {
		t.Fatalf("cmd is nil, want window size cmd")
	}
	// Execute the command to cover the function literal
	msg := cmd()
	if wsm, ok := msg.(tea.WindowSizeMsg); !ok || wsm.Width != 100 || wsm.Height != 40 {
		t.Fatalf("cmd returned %v, want WindowSizeMsg{100,40}", msg)
	}
}

func TestAppModel_Update_NavToAbout_WithoutWindowSize(t *testing.T) {
	m := newAppModel().(appModel)
	model, cmd := m.Update(navToAboutMsg{})
	am := model.(appModel)
	if am.active != "about" {
		t.Fatalf("active = %q, want about", am.active)
	}
	if cmd != nil {
		t.Fatalf("cmd is not nil, want nil")
	}
}

func TestAppModel_Update_NavBackToUnsigned_WithWindowSize(t *testing.T) {
	m := newAppModel().(appModel)
	model, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	am := model.(appModel)
	// Go to login first
	model, _ = am.Update(navToLoginMsg{})
	am = model.(appModel)
	// Go back
	model, cmd := am.Update(navBackToUnsignedMsg{})
	am = model.(appModel)
	if am.active != "unsigned" {
		t.Fatalf("active = %q, want unsigned", am.active)
	}
	if cmd == nil {
		t.Fatalf("cmd is nil, want window size cmd")
	}
	// Execute the command
	msg := cmd()
	if wsm, ok := msg.(tea.WindowSizeMsg); !ok || wsm.Width != 100 || wsm.Height != 40 {
		t.Fatalf("cmd returned %v, want WindowSizeMsg{100,40}", msg)
	}
}

func TestAppModel_Update_NavBackToUnsigned_WithoutWindowSize(t *testing.T) {
	m := newAppModel().(appModel)
	model, cmd := m.Update(navBackToUnsignedMsg{})
	am := model.(appModel)
	if am.active != "unsigned" {
		t.Fatalf("active = %q, want unsigned", am.active)
	}
	if cmd != nil {
		t.Fatalf("cmd is not nil, want nil")
	}
}

func TestAppModel_Update_NavSignedIn_WithWindowSize(t *testing.T) {
	m := newAppModel().(appModel)
	model, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	am := model.(appModel)
	model, cmd := am.Update(navSignedInMsg{})
	am = model.(appModel)
	if am.active != "signed" {
		t.Fatalf("active = %q, want signed", am.active)
	}
	if cmd == nil {
		t.Fatalf("cmd is nil, want window size cmd")
	}
	// Execute the command
	msg := cmd()
	if wsm, ok := msg.(tea.WindowSizeMsg); !ok || wsm.Width != 100 || wsm.Height != 40 {
		t.Fatalf("cmd returned %v, want WindowSizeMsg{100,40}", msg)
	}
}

func TestAppModel_Update_NavSignedIn_WithoutWindowSize(t *testing.T) {
	m := newAppModel().(appModel)
	model, cmd := m.Update(navSignedInMsg{})
	am := model.(appModel)
	if am.active != "signed" {
		t.Fatalf("active = %q, want signed", am.active)
	}
	if cmd != nil {
		t.Fatalf("cmd is not nil, want nil")
	}
}

func TestAppModel_Update_NavSignOut_WithWindowSize(t *testing.T) {
	m := newAppModel().(appModel)
	model, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	am := model.(appModel)
	// Go to signed first
	model, _ = am.Update(navSignedInMsg{})
	am = model.(appModel)
	// Sign out
	model, cmd := am.Update(navSignOutMsg{})
	am = model.(appModel)
	if am.active != "unsigned" {
		t.Fatalf("active = %q, want unsigned", am.active)
	}
	if cmd == nil {
		t.Fatalf("cmd is nil, want window size cmd")
	}
	// Execute the command
	msg := cmd()
	if wsm, ok := msg.(tea.WindowSizeMsg); !ok || wsm.Width != 100 || wsm.Height != 40 {
		t.Fatalf("cmd returned %v, want WindowSizeMsg{100,40}", msg)
	}
}

func TestAppModel_Update_NavSignOut_WithoutWindowSize(t *testing.T) {
	m := newAppModel().(appModel)
	model, cmd := m.Update(navSignOutMsg{})
	am := model.(appModel)
	if am.active != "unsigned" {
		t.Fatalf("active = %q, want unsigned", am.active)
	}
	if cmd != nil {
		t.Fatalf("cmd is not nil, want nil")
	}
}

func TestAppModel_View_Login(t *testing.T) {
	m := newAppModel().(appModel)
	model, _ := m.Update(navToLoginMsg{})
	am := model.(appModel)
	view := am.View()
	if !strings.Contains(view, "Sign in") {
		t.Fatalf("login view does not contain 'Sign in'; got: %s", view)
	}
}

func TestAppModel_View_About(t *testing.T) {
	m := newAppModel().(appModel)
	model, _ := m.Update(navToAboutMsg{})
	am := model.(appModel)
	view := am.View()
	if !strings.Contains(view, "About") {
		t.Fatalf("about view does not contain 'About'; got: %s", view)
	}
}

func TestAppModel_View_Signed(t *testing.T) {
	m := newAppModel().(appModel)
	model, _ := m.Update(navSignedInMsg{})
	am := model.(appModel)
	view := am.View()
	if !strings.Contains(view, "Sneat.app") {
		t.Fatalf("signed view does not contain 'Sneat.app'; got: %s", view)
	}
}

func TestAppModel_View_Unsigned_Default(t *testing.T) {
	m := newAppModel().(appModel)
	view := m.View()
	if !strings.Contains(view, "Sneat.app") {
		t.Fatalf("unsigned view does not contain 'Sneat.app'; got: %s", view)
	}
}

func TestAppModel_Update_ForwardToActive_Default(t *testing.T) {
	// Test forwarding a message to the active child (default case)
	m := newAppModel().(appModel)
	// Send a key down message which should be forwarded to unsigned menu
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	am := model.(appModel)
	// The list in unsigned menu should have moved selection
	if am.unsigned.list.Index() != 1 {
		t.Fatalf("unsigned list index = %d, want 1", am.unsigned.list.Index())
	}
}

func TestAppModel_Update_ForwardToActive_LoginScreen(t *testing.T) {
	m := newAppModel().(appModel)
	// Navigate to login
	model, cmd := m.Update(navToLoginMsg{})
	am := model.(appModel)
	if cmd != nil {
		msg := cmd()
		model, _ = am.Update(msg)
		am = model.(appModel)
	}
	// Send a key message to login screen
	model, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	am = model.(appModel)
	// Verify the login model received the key
	if am.login.email.Value() != "t" {
		t.Fatalf("login email value = %q, want 't'", am.login.email.Value())
	}
}

func TestAppModel_Update_ForwardToActive_AboutScreen(t *testing.T) {
	m := newAppModel().(appModel)
	// Navigate to about
	model, _ := m.Update(navToAboutMsg{})
	am := model.(appModel)
	// Send a window size message
	model, _ = am.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	am = model.(appModel)
	// Verify the about model received the window size
	if am.about.winW != 100 || am.about.winH != 40 {
		t.Fatalf("about window size = (%d,%d), want (100,40)", am.about.winW, am.about.winH)
	}
}

func TestAppModel_Update_ForwardToActive_SignedScreen(t *testing.T) {
	m := newAppModel().(appModel)
	// Navigate to signed
	model, _ := m.Update(navSignedInMsg{})
	am := model.(appModel)
	// Send a key down message
	model, _ = am.Update(tea.KeyMsg{Type: tea.KeyDown})
	am = model.(appModel)
	// Verify the signed model received the message
	if am.signed.list.Index() != 1 {
		t.Fatalf("signed list index = %d, want 1", am.signed.list.Index())
	}
}
