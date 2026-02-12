package sneatui

import (
	"testing"

	"github.com/rivo/tview"
)

func TestNewMenuUnsigned_NotNil(t *testing.T) {
	app := NewApp()
	menu := newMenuUnsigned(app)
	if menu == nil {
		t.Fatalf("newMenuUnsigned() returned nil")
	}
}

func TestNewMenuUnsigned_IsList(t *testing.T) {
	app := NewApp()
	menu := newMenuUnsigned(app)
	if _, ok := menu.(*tview.List); !ok {
		t.Fatalf("newMenuUnsigned() did not return a *tview.List")
	}
}

func TestMenuUnsigned_HasSignInItem(t *testing.T) {
	app := NewApp()
	menu := newMenuUnsigned(app).(*tview.List)
	if menu.GetItemCount() < 1 {
		t.Fatalf("menu has no items")
	}
	mainText, _ := menu.GetItemText(0)
	if mainText != "Sign-in" {
		t.Fatalf("first item = %q, want 'Sign-in'", mainText)
	}
}

func TestMenuUnsigned_HasAboutItem(t *testing.T) {
	app := NewApp()
	menu := newMenuUnsigned(app).(*tview.List)
	if menu.GetItemCount() < 2 {
		t.Fatalf("menu has less than 2 items")
	}
	mainText, _ := menu.GetItemText(1)
	if mainText != "About" {
		t.Fatalf("second item = %q, want 'About'", mainText)
	}
}
