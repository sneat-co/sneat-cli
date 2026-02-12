package sneatui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// newMenuSignedIn creates the main menu for signed-in users.
func newMenuSignedIn(app *App) tview.Primitive {
	list := tview.NewList()
	list.SetBorder(true)
	list.SetTitle(" Sneat.app - menu ")

	// Add menu items
	list.AddItem("Calendar", "View your calendar", '1', nil)
	list.AddItem("Members", "Manage members", '2', nil)
	list.AddItem("Lists", "See your lists", '3', nil)
	list.AddItem("Sign-out", "Return to unsigned menu", '4', func() {
		app.ShowUnsigned()
	})

	// Set up keyboard shortcuts
	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyCtrlC:
			app.Quit()
			return nil
		}
		return event
	})

	return list
}
