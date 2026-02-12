package sneatui

import (
	"testing"

	"github.com/rivo/tview"
)

func TestNewMenuSignedIn_NotNil(t *testing.T) {
	app := NewApp()
	menu := newMenuSignedIn(app)
	if menu == nil {
		t.Fatalf("newMenuSignedIn() returned nil")
	}
}

func TestNewMenuSignedIn_IsList(t *testing.T) {
	app := NewApp()
	menu := newMenuSignedIn(app)
	if _, ok := menu.(*tview.List); !ok {
		t.Fatalf("newMenuSignedIn() did not return a *tview.List")
	}
}

func TestMenuSignedIn_HasCalendarItem(t *testing.T) {
	app := NewApp()
	menu := newMenuSignedIn(app).(*tview.List)
	if menu.GetItemCount() < 1 {
		t.Fatalf("menu has no items")
	}
	mainText, _ := menu.GetItemText(0)
	if mainText != "Calendar" {
		t.Fatalf("first item = %q, want 'Calendar'", mainText)
	}
}

func TestMenuSignedIn_HasMembersItem(t *testing.T) {
	app := NewApp()
	menu := newMenuSignedIn(app).(*tview.List)
	if menu.GetItemCount() < 2 {
		t.Fatalf("menu has less than 2 items")
	}
	mainText, _ := menu.GetItemText(1)
	if mainText != "Members" {
		t.Fatalf("second item = %q, want 'Members'", mainText)
	}
}

func TestMenuSignedIn_HasListsItem(t *testing.T) {
	app := NewApp()
	menu := newMenuSignedIn(app).(*tview.List)
	if menu.GetItemCount() < 3 {
		t.Fatalf("menu has less than 3 items")
	}
	mainText, _ := menu.GetItemText(2)
	if mainText != "Lists" {
		t.Fatalf("third item = %q, want 'Lists'", mainText)
	}
}

func TestMenuSignedIn_HasSignOutItem(t *testing.T) {
	app := NewApp()
	menu := newMenuSignedIn(app).(*tview.List)
	if menu.GetItemCount() < 4 {
		t.Fatalf("menu has less than 4 items")
	}
	mainText, _ := menu.GetItemText(3)
	if mainText != "Sign-out" {
		t.Fatalf("fourth item = %q, want 'Sign-out'", mainText)
	}
}
