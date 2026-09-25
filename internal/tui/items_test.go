package tui

import "testing"

func TestContactItem_DescriptionIncludesGender(t *testing.T) {
	i := contactItem{ctype: "person", gender: "female"}
	if got := i.Description(); got != "person · female" {
		t.Errorf("Description() = %q, want %q", got, "person · female")
	}
}

func TestCommChannelKeys_ReturnsSortedKeys(t *testing.T) {
	got := commChannelKeys(map[string]string{"b@x.com": "work", "a@x.com": "home"})
	want := []string{"a@x.com", "b@x.com"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("commChannelKeys = %v, want %v", got, want)
	}
}
