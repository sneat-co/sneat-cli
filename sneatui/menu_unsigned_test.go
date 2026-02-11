package sneatui

import "testing"

func TestItemMethods(t *testing.T) {
	i := item{title: "T", desc: "D"}
	if got := i.Title(); got != "T" {
		t.Fatalf("Title() = %q, want %q", got, "T")
	}
	if got := i.Description(); got != "D" {
		t.Fatalf("Description() = %q, want %q", got, "D")
	}
	if got := i.FilterValue(); got != "T" {
		t.Fatalf("FilterValue() = %q, want %q", got, "T")
	}
}

func TestMenuUnsigned_Init(t *testing.T) {
	m := newMenuUnassigned().(menuUnsigned)
	cmd := m.Init()
	if cmd != nil {
		t.Fatalf("Init() returned non-nil cmd, want nil")
	}
}
