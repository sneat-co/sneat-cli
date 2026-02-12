package sneatui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// App wraps the tview application with our navigation logic.
type App struct {
	*tview.Application
	pages *tview.Pages
}

// NewApp creates and initializes the sneat TUI application.
func NewApp() *App {
	app := tview.NewApplication()
	pages := tview.NewPages()

	// Create the root app wrapper
	tui := &App{
		Application: app,
		pages:       pages,
	}

	// Add screens to pages
	pages.AddPage("unsigned", newMenuUnsigned(tui), true, true)
	pages.AddPage("login", newLoginPage(tui), true, false)
	pages.AddPage("about", newAboutPage(tui), true, false)
	pages.AddPage("signed", newMenuSignedIn(tui), true, false)

	app.SetRoot(pages, true)
	return tui
}

// ShowLogin navigates to the login screen.
func (a *App) ShowLogin() {
	a.pages.SwitchToPage("login")
}

// ShowAbout navigates to the about screen.
func (a *App) ShowAbout() {
	a.pages.SwitchToPage("about")
}

// ShowUnsigned navigates to the unsigned menu.
func (a *App) ShowUnsigned() {
	a.pages.SwitchToPage("unsigned")
}

// ShowSigned navigates to the signed-in menu.
func (a *App) ShowSigned() {
	a.pages.SwitchToPage("signed")
}

// Quit stops the application.
func (a *App) Quit() {
	a.Stop()
}

// SetGlobalKeyHandler sets up global keyboard shortcuts.
func (a *App) SetGlobalKeyHandler() {
	a.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC {
			a.Quit()
			return nil
		}
		return event
	})
}
