package sneatui

import (
	"testing"
)

func TestNewApp_NotNil(t *testing.T) {
	app := NewApp()
	if app == nil {
		t.Fatalf("NewApp() returned nil")
	}
}

func TestApp_PagesNotNil(t *testing.T) {
	app := NewApp()
	if app.pages == nil {
		t.Fatalf("app.pages is nil")
	}
}

func TestApp_ShowLogin(t *testing.T) {
	app := NewApp()
	app.ShowLogin()
	name, _ := app.pages.GetFrontPage()
	if name != "login" {
		t.Fatalf("front page = %q, want 'login'", name)
	}
}

func TestApp_ShowAbout(t *testing.T) {
	app := NewApp()
	app.ShowAbout()
	name, _ := app.pages.GetFrontPage()
	if name != "about" {
		t.Fatalf("front page = %q, want 'about'", name)
	}
}

func TestApp_ShowUnsigned(t *testing.T) {
	app := NewApp()
	// First navigate away
	app.ShowLogin()
	// Then back to unsigned
	app.ShowUnsigned()
	name, _ := app.pages.GetFrontPage()
	if name != "unsigned" {
		t.Fatalf("front page = %q, want 'unsigned'", name)
	}
}

func TestApp_ShowSigned(t *testing.T) {
	app := NewApp()
	app.ShowSigned()
	name, _ := app.pages.GetFrontPage()
	if name != "signed" {
		t.Fatalf("front page = %q, want 'signed'", name)
	}
}

func TestApp_InitialPageIsUnsigned(t *testing.T) {
	app := NewApp()
	name, _ := app.pages.GetFrontPage()
	if name != "unsigned" {
		t.Fatalf("initial front page = %q, want 'unsigned'", name)
	}
}

func TestApp_NavigationFlow_UnsignedToLoginToSigned(t *testing.T) {
	app := NewApp()

	// Start at unsigned
	name, _ := app.pages.GetFrontPage()
	if name != "unsigned" {
		t.Fatalf("start page = %q, want 'unsigned'", name)
	}

	// Navigate to login
	app.ShowLogin()
	name, _ = app.pages.GetFrontPage()
	if name != "login" {
		t.Fatalf("after ShowLogin, page = %q, want 'login'", name)
	}

	// Navigate to signed
	app.ShowSigned()
	name, _ = app.pages.GetFrontPage()
	if name != "signed" {
		t.Fatalf("after ShowSigned, page = %q, want 'signed'", name)
	}

	// Navigate back to unsigned
	app.ShowUnsigned()
	name, _ = app.pages.GetFrontPage()
	if name != "unsigned" {
		t.Fatalf("after ShowUnsigned, page = %q, want 'unsigned'", name)
	}
}

func TestApp_NavigationFlow_UnsignedToAboutToUnsigned(t *testing.T) {
	app := NewApp()

	// Start at unsigned
	name, _ := app.pages.GetFrontPage()
	if name != "unsigned" {
		t.Fatalf("start page = %q, want 'unsigned'", name)
	}

	// Navigate to about
	app.ShowAbout()
	name, _ = app.pages.GetFrontPage()
	if name != "about" {
		t.Fatalf("after ShowAbout, page = %q, want 'about'", name)
	}

	// Navigate back to unsigned
	app.ShowUnsigned()
	name, _ = app.pages.GetFrontPage()
	if name != "unsigned" {
		t.Fatalf("after ShowUnsigned, page = %q, want 'unsigned'", name)
	}
}

func TestApp_NavigationFlow_SignedToUnsigned(t *testing.T) {
	app := NewApp()

	// Navigate to signed
	app.ShowSigned()
	name, _ := app.pages.GetFrontPage()
	if name != "signed" {
		t.Fatalf("after ShowSigned, page = %q, want 'signed'", name)
	}

	// Navigate back to unsigned (sign out)
	app.ShowUnsigned()
	name, _ = app.pages.GetFrontPage()
	if name != "unsigned" {
		t.Fatalf("after ShowUnsigned, page = %q, want 'unsigned'", name)
	}
}
