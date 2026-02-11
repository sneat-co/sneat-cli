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

func TestCalculateDefaultSize_Normal(t *testing.T) {
	w, h := calculateDefaultSize(4, 2)
	if w != 76 || h != 22 {
		t.Fatalf("calculateDefaultSize(4, 2) = (%d, %d), want (76, 22)", w, h)
	}
}

func TestCalculateDefaultSize_SmallWidth(t *testing.T) {
	// Test when 80 - h < 20, should clamp to 20
	w, h := calculateDefaultSize(65, 2)
	if w != 20 {
		t.Fatalf("calculateDefaultSize(65, 2) width = %d, want 20", w)
	}
	if h != 22 {
		t.Fatalf("calculateDefaultSize(65, 2) height = %d, want 22", h)
	}
}

func TestCalculateDefaultSize_SmallHeight(t *testing.T) {
	// Test when 24 - v < 5, should clamp to 5
	w, h := calculateDefaultSize(4, 20)
	if w != 76 {
		t.Fatalf("calculateDefaultSize(4, 20) width = %d, want 76", w)
	}
	if h != 5 {
		t.Fatalf("calculateDefaultSize(4, 20) height = %d, want 5", h)
	}
}

func TestCalculateDefaultSize_BothSmall(t *testing.T) {
	// Test when both dimensions are below minimum
	w, h := calculateDefaultSize(100, 30)
	if w != 20 {
		t.Fatalf("calculateDefaultSize(100, 30) width = %d, want 20", w)
	}
	if h != 5 {
		t.Fatalf("calculateDefaultSize(100, 30) height = %d, want 5", h)
	}
}
