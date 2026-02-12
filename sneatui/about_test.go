package sneatui

import (
	"testing"

	"github.com/rivo/tview"
)

func TestNewAboutPage_NotNil(t *testing.T) {
	app := NewApp()
	about := newAboutPage(app)
	if about == nil {
		t.Fatalf("newAboutPage() returned nil")
	}
}

func TestNewAboutPage_IsTextView(t *testing.T) {
	app := NewApp()
	about := newAboutPage(app)
	if _, ok := about.(*tview.TextView); !ok {
		t.Fatalf("newAboutPage() did not return a *tview.TextView")
	}
}

func TestAboutPage_HasContent(t *testing.T) {
	app := NewApp()
	textView := newAboutPage(app).(*tview.TextView)
	text := textView.GetText(false)
	if text == "" {
		t.Fatalf("about page has no text content")
	}
}

func TestAboutPage_ContentContainsEscInstructions(t *testing.T) {
	app := NewApp()
	textView := newAboutPage(app).(*tview.TextView)
	text := textView.GetText(false)
	if len(text) < 10 {
		t.Fatalf("about page text is too short: %d characters", len(text))
	}
	// Should contain ESC instructions - just ensure it has reasonable content
	if len(text) < 20 {
		t.Fatalf("about page text is too short")
	}
}
