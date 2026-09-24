package chatapp

import (
	"context"
	"testing"
)

type spaceIDFakeSpaces map[string]any

func (f spaceIDFakeSpaces) ListSpaces(context.Context, string) (map[string]any, error) {
	return f, nil
}

// TestDefaultSpaceID covers the SPACE ruling: session.CurrentSpace wins
// outright when set; else the user's family space if one exists; else the
// lowest ID after sorting -- never map iteration order.
func TestDefaultSpaceID(t *testing.T) {
	spaces := spaceIDFakeSpaces{
		"space-c": map[string]any{"type": "private"},
		"space-a": map[string]any{"type": "private"},
		"space-b": map[string]any{"type": "family"},
	}

	t.Run("current space wins outright", func(t *testing.T) {
		got := defaultSpaceID(context.Background(), spaces, "u1", "space-c")
		if got != "space-c" {
			t.Fatalf("got %q, want space-c", got)
		}
	})

	t.Run("family space wins when no current space", func(t *testing.T) {
		got := defaultSpaceID(context.Background(), spaces, "u1", "")
		if got != "space-b" {
			t.Fatalf("got %q, want space-b (family)", got)
		}
	})

	t.Run("lowest id when no family space", func(t *testing.T) {
		noFamily := spaceIDFakeSpaces{
			"space-c": map[string]any{"type": "private"},
			"space-a": map[string]any{"type": "private"},
			"space-b": map[string]any{"type": "private"},
		}
		got := defaultSpaceID(context.Background(), noFamily, "u1", "")
		if got != "space-a" {
			t.Fatalf("got %q, want space-a (lowest sorted id)", got)
		}
	})

	t.Run("no spaces returns empty", func(t *testing.T) {
		got := defaultSpaceID(context.Background(), spaceIDFakeSpaces{}, "u1", "")
		if got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})

	t.Run("nil reader returns empty", func(t *testing.T) {
		got := defaultSpaceID(context.Background(), nil, "u1", "")
		if got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})
}
