package sneatui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// newLoginPage creates the login form screen.
func newLoginPage(app *App) tview.Primitive {
	form := tview.NewForm()
	form.SetBorder(true)
	form.SetTitle(" Sneat.app - Sign in ")

	// Add email and password fields
	form.AddInputField("Email:", "", 40, nil, nil)
	form.AddPasswordField("Password:", "", 40, '*', nil)

	// Add buttons
	form.AddButton("Sign in", func() {
		app.ShowSigned()
	})
	form.AddButton("Cancel", func() {
		app.ShowUnsigned()
	})

	// Set up keyboard shortcuts
	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc:
			app.ShowUnsigned()
			return nil
		}
		return event
	})

	return form
}
