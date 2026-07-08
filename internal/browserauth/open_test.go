package browserauth

import "testing"

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
