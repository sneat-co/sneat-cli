package sneatui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// newMenuUnsigned creates the main menu for unsigned users.
func newMenuUnsigned(app *App) tview.Primitive {
	list := tview.NewList()
	list.SetBorder(true)
	list.SetTitle(" Sneat.app - main menu ")

	// Add menu items
	list.AddItem("Sign-in", "Authorize to get access to your data", '1', func() {
		app.ShowLogin()
	})
	list.AddItem("About", "Learn about the Sneat.app", '2', func() {
		app.ShowAbout()
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
