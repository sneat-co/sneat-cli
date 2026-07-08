package firestoredb

import "testing"

func TestSpacesFromUser(t *testing.T) {
	got := spacesFromUser(map[string]any{
		"spaces": map[string]any{"s1": map[string]any{"type": "family"}},
	})
	if len(got) != 1 || got["s1"] == nil {
		t.Fatalf("got %+v", got)
	}
}

func TestSpacesFromUser_MissingOrWrongType(t *testing.T) {
	for _, user := range []map[string]any{
		{},
		{"spaces": "not-a-map"},
		{"spaces": nil},
	} {
		got := spacesFromUser(user)
		if got == nil {
			t.Fatalf("nil map for %+v", user)
		}
		if len(got) != 0 {
			t.Fatalf("expected empty map, got %+v", got)
		}
	}
}
