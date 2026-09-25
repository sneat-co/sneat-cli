package browserauth

import (
	"errors"
	"testing"
)

func TestBrowserCommand(t *testing.T) {
	cases := map[string]struct {
		wantName string
		wantErr  bool
	}{
		"darwin":  {"open", false},
		"linux":   {"xdg-open", false},
		"windows": {"rundll32", false},
		"plan9":   {"", true},
	}
	for goos, want := range cases {
		name, args, err := browserCommand(goos, "http://x/")
		if want.wantErr {
			if err == nil {
				t.Errorf("%s: expected error", goos)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", goos, err)
		}
		if name != want.wantName {
			t.Errorf("%s: name = %q, want %q", goos, name, want.wantName)
		}
		if len(args) == 0 {
			t.Errorf("%s: no args", goos)
		}
	}
}

func TestOpenBrowser_UnsupportedPlatform(t *testing.T) {
	prevGoos := goos
	goos = "plan9"
	t.Cleanup(func() { goos = prevGoos })

	if err := OpenBrowser("http://x/"); err == nil {
		t.Fatalf("expected error for unsupported platform")
	}
}

func TestOpenBrowser_StartsCommand(t *testing.T) {
	prevGoos, prevStart := goos, startCommand
	goos = "darwin"
	var gotName string
	var gotArgs []string
	startCommand = func(name string, args ...string) error {
		gotName, gotArgs = name, args
		return nil
	}
	t.Cleanup(func() { goos, startCommand = prevGoos, prevStart })

	if err := OpenBrowser("http://x/"); err != nil {
		t.Fatalf("OpenBrowser: %v", err)
	}
	if gotName != "open" || len(gotArgs) != 1 || gotArgs[0] != "http://x/" {
		t.Fatalf("start command = %q %v", gotName, gotArgs)
	}
}

func TestStartCommand_Default(t *testing.T) {
	// Exercises the real (non-seamed) implementation with a harmless,
	// always-present command instead of actually opening a browser.
	if err := startCommand("true"); err != nil {
		t.Fatalf("startCommand(\"true\") = %v", err)
	}
}

func TestOpenBrowser_StartCommandError(t *testing.T) {
	prevGoos, prevStart := goos, startCommand
	goos = "darwin"
	startCommand = func(string, ...string) error { return errors.New("start failed") }
	t.Cleanup(func() { goos, startCommand = prevGoos, prevStart })

	if err := OpenBrowser("http://x/"); err == nil {
		t.Fatalf("expected start error")
	}
}
