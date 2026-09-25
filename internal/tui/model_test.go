package tui

import "testing"

// An empty stack is not reachable through New (which always seeds
// *spacesScreen), but Model's own guards must still hold for a zero-value
// Model, so these construct one directly.

func TestModel_EmptyStack_Init(t *testing.T) {
	m := Model{}
	if cmd := m.Init(); cmd != nil {
		t.Errorf("Init() on an empty stack = %v, want nil", cmd)
	}
}

func TestModel_EmptyStack_Update(t *testing.T) {
	m := Model{}
	got, cmd := m.Update(nil)
	if cmd != nil {
		t.Errorf("Update() on an empty stack produced a command: %v", cmd)
	}
	if got.(Model).top() != nil {
		t.Error("expected the model to remain stackless")
	}
}

func TestModel_EmptyStack_Top(t *testing.T) {
	m := Model{}
	if got := m.top(); got != nil {
		t.Errorf("top() on an empty stack = %v, want nil", got)
	}
}
