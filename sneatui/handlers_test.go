package sneatui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// frontPage returns the name of the currently visible page.
func frontPage(app *App) string {
	name, _ := app.pages.GetFrontPage()
	return name
}

func TestApp_Quit_Stops(t *testing.T) {
	app := NewApp()
	// Quit delegates to Stop(). On a never-started application this is a no-op
	// but must not panic.
	app.Quit()
}

func TestApp_SetGlobalKeyHandler_CtrlCQuits(t *testing.T) {
	app := NewApp()
	app.SetGlobalKeyHandler()

	capture := app.GetInputCapture()
	if capture == nil {
		t.Fatalf("SetGlobalKeyHandler did not install an input capture")
	}

	// Ctrl-C should be consumed (returns nil) after quitting.
	if ev := capture(tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModNone)); ev != nil {
		t.Fatalf("Ctrl-C event was not consumed, got %v", ev)
	}

	// Any other key should pass through unchanged.
	in := tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone)
	if out := capture(in); out != in {
		t.Fatalf("non-Ctrl-C event was not passed through")
	}
}

func TestAboutPage_EscReturnsToUnsigned(t *testing.T) {
	app := NewApp()
	about := newAboutPage(app).(*tview.TextView)
	app.ShowAbout()

	capture := about.GetInputCapture()
	if capture == nil {
		t.Fatalf("about page has no input capture")
	}

	if ev := capture(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone)); ev != nil {
		t.Fatalf("Esc event was not consumed")
	}
	if got := frontPage(app); got != "unsigned" {
		t.Fatalf("after Esc, front page = %q, want 'unsigned'", got)
	}

	// Unrelated keys pass through.
	in := tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone)
	if out := capture(in); out != in {
		t.Fatalf("non-Esc event was not passed through")
	}
}

func TestLoginPage_EscReturnsToUnsigned(t *testing.T) {
	app := NewApp()
	form := newLoginPage(app).(*tview.Form)
	app.ShowLogin()

	capture := form.GetInputCapture()
	if capture == nil {
		t.Fatalf("login page has no input capture")
	}

	if ev := capture(tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone)); ev != nil {
		t.Fatalf("Esc event was not consumed")
	}
	if got := frontPage(app); got != "unsigned" {
		t.Fatalf("after Esc, front page = %q, want 'unsigned'", got)
	}

	in := tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone)
	if out := capture(in); out != in {
		t.Fatalf("non-Esc event was not passed through")
	}
}

func TestLoginPage_SignInButtonShowsSigned(t *testing.T) {
	app := NewApp()
	form := newLoginPage(app).(*tview.Form)

	// Index 0 is "Sign in".
	form.GetButton(0).InputHandler()(
		tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), nil)

	if got := frontPage(app); got != "signed" {
		t.Fatalf("after Sign in, front page = %q, want 'signed'", got)
	}
}

func TestLoginPage_CancelButtonShowsUnsigned(t *testing.T) {
	app := NewApp()
	form := newLoginPage(app).(*tview.Form)
	app.ShowLogin()

	// Index 1 is "Cancel".
	form.GetButton(1).InputHandler()(
		tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), nil)

	if got := frontPage(app); got != "unsigned" {
		t.Fatalf("after Cancel, front page = %q, want 'unsigned'", got)
	}
}

func TestMenuUnsigned_CtrlCConsumed(t *testing.T) {
	app := NewApp()
	list := newMenuUnsigned(app).(*tview.List)

	capture := list.GetInputCapture()
	if capture == nil {
		t.Fatalf("unsigned menu has no input capture")
	}
	if ev := capture(tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModNone)); ev != nil {
		t.Fatalf("Ctrl-C event was not consumed")
	}

	in := tcell.NewEventKey(tcell.KeyRune, 'z', tcell.ModNone)
	if out := capture(in); out != in {
		t.Fatalf("non-Ctrl-C event was not passed through")
	}
}

func TestMenuUnsigned_SignInItemShowsLogin(t *testing.T) {
	app := NewApp()
	list := newMenuUnsigned(app).(*tview.List)

	selectItem(list, 0)
	if got := frontPage(app); got != "login" {
		t.Fatalf("after Sign-in select, front page = %q, want 'login'", got)
	}
}

func TestMenuUnsigned_AboutItemShowsAbout(t *testing.T) {
	app := NewApp()
	list := newMenuUnsigned(app).(*tview.List)

	selectItem(list, 1)
	if got := frontPage(app); got != "about" {
		t.Fatalf("after About select, front page = %q, want 'about'", got)
	}
}

func TestMenuSignedIn_CtrlCConsumed(t *testing.T) {
	app := NewApp()
	list := newMenuSignedIn(app).(*tview.List)

	capture := list.GetInputCapture()
	if capture == nil {
		t.Fatalf("signed-in menu has no input capture")
	}
	if ev := capture(tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModNone)); ev != nil {
		t.Fatalf("Ctrl-C event was not consumed")
	}

	in := tcell.NewEventKey(tcell.KeyRune, 'z', tcell.ModNone)
	if out := capture(in); out != in {
		t.Fatalf("non-Ctrl-C event was not passed through")
	}
}

func TestMenuSignedIn_SignOutItemShowsUnsigned(t *testing.T) {
	app := NewApp()
	list := newMenuSignedIn(app).(*tview.List)
	app.ShowSigned()

	// Index 3 is "Sign-out".
	selectItem(list, 3)
	if got := frontPage(app); got != "unsigned" {
		t.Fatalf("after Sign-out select, front page = %q, want 'unsigned'", got)
	}
}

// selectItem selects the list item at index and fires its per-item handler
// (registered via AddItem) by driving an Enter key through the list's input
// handler.
func selectItem(list *tview.List, index int) {
	list.SetCurrentItem(index)
	list.InputHandler()(
		tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), nil)
}
