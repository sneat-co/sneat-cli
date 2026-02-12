package sneatui

import (
	"testing"

	"github.com/rivo/tview"
)

func TestNewLoginPage_NotNil(t *testing.T) {
	app := NewApp()
	login := newLoginPage(app)
	if login == nil {
		t.Fatalf("newLoginPage() returned nil")
	}
}

func TestNewLoginPage_IsForm(t *testing.T) {
	app := NewApp()
	login := newLoginPage(app)
	if _, ok := login.(*tview.Form); !ok {
		t.Fatalf("newLoginPage() did not return a *tview.Form")
	}
}

func TestLoginPage_HasEmailField(t *testing.T) {
	app := NewApp()
	form := newLoginPage(app).(*tview.Form)
	if form.GetFormItemCount() < 2 {
		t.Fatalf("form has less than 2 fields")
	}
	// First field should be email
	item := form.GetFormItem(0)
	if inputField, ok := item.(*tview.InputField); ok {
		label := inputField.GetLabel()
		if label != "Email:" {
			t.Fatalf("first field label = %q, want 'Email:'", label)
		}
	} else {
		t.Fatalf("first field is not an InputField")
	}
}

func TestLoginPage_HasPasswordField(t *testing.T) {
	app := NewApp()
	form := newLoginPage(app).(*tview.Form)
	if form.GetFormItemCount() < 2 {
		t.Fatalf("form has less than 2 fields")
	}
	// Second field should be password
	item := form.GetFormItem(1)
	if inputField, ok := item.(*tview.InputField); ok {
		label := inputField.GetLabel()
		if label != "Password:" {
			t.Fatalf("second field label = %q, want 'Password:'", label)
		}
	} else {
		t.Fatalf("second field is not an InputField")
	}
}

func TestLoginPage_HasSignInButton(t *testing.T) {
	app := NewApp()
	form := newLoginPage(app).(*tview.Form)
	if form.GetButtonCount() < 1 {
		t.Fatalf("form has no buttons")
	}
	button := form.GetButton(0)
	if button.GetLabel() != "Sign in" {
		t.Fatalf("first button label = %q, want 'Sign in'", button.GetLabel())
	}
}

func TestLoginPage_HasCancelButton(t *testing.T) {
	app := NewApp()
	form := newLoginPage(app).(*tview.Form)
	if form.GetButtonCount() < 2 {
		t.Fatalf("form has less than 2 buttons")
	}
	button := form.GetButton(1)
	if button.GetLabel() != "Cancel" {
		t.Fatalf("second button label = %q, want 'Cancel'", button.GetLabel())
	}
}
