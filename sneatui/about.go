package sneatui

import (
	"os"
	"path/filepath"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// newAboutPage creates the about screen showing README content.
func newAboutPage(app *App) tview.Primitive {
	textView := tview.NewTextView()
	textView.SetBorder(true)
	textView.SetTitle(" About Sneat.app ")
	textView.SetDynamicColors(true)
	textView.SetScrollable(true)

	// Load README.md content
	path := filepath.Clean("../README.md")
	content, err := os.ReadFile(path)
	if err != nil {
		textView.SetText("Sneat.app\n\nAbout information is unavailable: " + err.Error() + "\n\n(ESC to return)")
	} else {
		textView.SetText(string(content) + "\n\n(ESC to return)")
	}

	// Set up keyboard shortcuts
	textView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc:
			app.ShowUnsigned()
			return nil
		}
		return event
	})

	return textView
}
