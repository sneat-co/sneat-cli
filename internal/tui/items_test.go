package tui

import "testing"

func TestContactItem_DescriptionIncludesGender(t *testing.T) {
	i := contactItem{ctype: "person", gender: "female"}
	if got := i.detail(); got != "person · female" {
		t.Errorf("detail() = %q, want %q", got, "person · female")
	}
}

func TestCommChannelKeys_ReturnsSortedKeys(t *testing.T) {
	got := commChannelKeys(map[string]string{"b@x.com": "work", "a@x.com": "home"})
	want := []string{"a@x.com", "b@x.com"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("commChannelKeys = %v, want %v", got, want)
	}
}

func TestJoinRoles(t *testing.T) {
	if joinRoles(nil) != "—" {
		t.Error("empty roles")
	}
	if joinRoles([]string{"a", "b"}) != "a, b" {
		t.Error("joined roles")
	}
}

func TestStrHelpers(t *testing.T) {
	if str("x") != "x" || str(1) != "" {
		t.Error("str")
	}
	if got := strList([]any{"a", 1, "b"}); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("strList = %v", got)
	}
}

func TestMenuItemRef(t *testing.T) {
	if menuItemRef(nil) != nil {
		t.Error("nil item")
	}
}
