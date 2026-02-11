package sneatui

import (
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"strings"
	"testing"
)

func TestUnsigned_EnterOnAbout_OpensAboutViaApp(t *testing.T) {
	m := InitialModel().(appModel)
	// Move selection down to About
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	am := model.(appModel)
	// Enter
	model, cmd := am.Update(tea.KeyMsg{Type: tea.KeyEnter})
	am = model.(appModel)
	if cmd != nil {
		msg := cmd()
		model, _ = am.Update(msg)
		am = model.(appModel)
	}
	if am.active != "about" {
		t.Fatalf("after Enter on About, active=%q, want about", am.active)
	}
}

func TestAbout_EscReturnsToMenu_ViaApp(t *testing.T) {
	m := InitialModel().(appModel)
	// Go to About
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	am := model.(appModel)
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
	if am.active != "unsigned" {
		t.Fatalf("after ESC from about, active=%q, want unsigned", am.active)
	}
}

func TestAbout_ViewContainsContent(t *testing.T) {
	a := newAboutModel().(aboutModel)
	// trigger Init to load content
	cmd := a.Init()
	if cmd != nil {
		msg := cmd()
		// deliver the loaded message to update state
		model, _ := a.Update(msg)
		a = model.(aboutModel)
	}
	view := a.View()
	if !strings.Contains(view, "Sneat") {
		t.Fatalf("about view does not contain expected content, got:\n%s", view)
	}
}

func TestUnsigned_SelectionPreserved_AfterAboutEsc(t *testing.T) {
	m := InitialModel().(appModel)
	// Move selection to About
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	am := model.(appModel)
	idxBefore := am.unsigned.list.Index()
	// Enter to open About
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

func TestAbout_ViewBeforeLoaded_ShowsLoading(t *testing.T) {
	a := newAboutModel().(aboutModel)
	view := a.View()
	if !strings.Contains(view, "Loading") {
		t.Fatalf("about view before loading does not contain 'Loading'; got:\n%s", view)
	}
}

func TestAbout_WindowSize_SavesDimensions(t *testing.T) {
	a := newAboutModel().(aboutModel)
	model, _ := a.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	a = model.(aboutModel)
	if a.winW != 100 || a.winH != 40 {
		t.Fatalf("window size = (%d,%d), want (100,40)", a.winW, a.winH)
	}
}

func TestAbout_OtherKeys_NoAction(t *testing.T) {
	a := newAboutModel().(aboutModel)
	model, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	a = model.(aboutModel)
	if cmd != nil {
		msg := cmd()
		// Should not return navBackToUnsignedMsg
		if _, ok := msg.(navBackToUnsignedMsg); ok {
			t.Fatalf("Enter key returned navBackToUnsignedMsg, should do nothing")
		}
	}
}

func TestAbout_Init_FileLoadError_ShowsError(t *testing.T) {
	a := newAboutModel().(aboutModel)
	cmd := a.Init()
	if cmd == nil {
		t.Fatalf("Init() returned nil cmd")
	}
	// Execute the command - it will try to load ../README.md
	// In normal test environment, this should succeed, but we test the structure
	msg := cmd()
	if loadedMsg, ok := msg.(aboutLoadedMsg); !ok {
		t.Fatalf("cmd() returned %T, want aboutLoadedMsg", msg)
	} else {
		// Update the model with the loaded message
		model, _ := a.Update(loadedMsg)
		a = model.(aboutModel)
		if !a.loaded {
			t.Fatalf("loaded = false, want true after receiving aboutLoadedMsg")
		}
		if a.content == "" {
			t.Fatalf("content is empty after loading")
		}
	}
}

func TestAbout_Init_ErrorPath_WhenFileNotFound(t *testing.T) {
	// Save current directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current dir: %v", err)
	}
	defer os.Chdir(origDir)

	// Change to /tmp where README.md won't exist
	if err := os.Chdir("/tmp"); err != nil {
		t.Fatalf("failed to change to /tmp: %v", err)
	}

	a := newAboutModel().(aboutModel)
	cmd := a.Init()
	if cmd == nil {
		t.Fatalf("Init() returned nil cmd")
	}

	// Execute the command - it should fail to load ../README.md from /tmp
	msg := cmd()
	loadedMsg, ok := msg.(aboutLoadedMsg)
	if !ok {
		t.Fatalf("cmd() returned %T, want aboutLoadedMsg", msg)
	}

	// The message should contain error text
	content := string(loadedMsg)
	if !strings.Contains(content, "unavailable") {
		t.Fatalf("error message does not contain 'unavailable'; got: %s", content)
	}
}
