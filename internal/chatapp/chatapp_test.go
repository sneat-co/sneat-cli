package chatapp

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/chat"
)

type spaceIDFakeSpaces map[string]any

func (f spaceIDFakeSpaces) ListSpaces(context.Context, string) (map[string]any, error) {
	return f, nil
}

// TestSessionClock_UsesGivenZone_NotProcessLocal is m1 (fix round r4
// review): SneatExecutor.Now/Pipeline.Now/Resolver.Now must all resolve
// "now" in the SESSION's chosen zone (--tz/SNEAT_TZ), not the process's own
// -- otherwise --tz only relabels timestamps for display while "today"/
// "this week" keep being computed in whatever zone the server happens to
// run in. Pacific/Kiritimati (UTC+14) is used because it is never the
// process's own zone in CI/dev, so a wiring bug that silently fell back to
// time.Now()'s bare (process-local) Location() would show up as a location
// mismatch here regardless of where this test actually runs.
func TestSessionClock_UsesGivenZone_NotProcessLocal(t *testing.T) {
	loc, err := time.LoadLocation("Pacific/Kiritimati")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	now := sessionClock(loc)
	before := time.Now()
	got := now()
	after := time.Now()

	if got.Location().String() != loc.String() {
		t.Fatalf("Location = %q, want %q -- sessionClock must resolve \"now\" in the session's OWN zone, not the process's", got.Location(), loc)
	}
	// Same instant, just relocated -- not a frozen or offset clock.
	if got.Before(before.Add(-time.Second)) || got.After(after.Add(time.Second)) {
		t.Fatalf("got = %v, want within [%v, %v] (same instant as time.Now(), just in loc)", got, before, after)
	}
}

// TestLocationFromName covers S4/TIMEZONES: a real IANA zone resolves to
// itself, an unparseable/empty one falls back to time.Local rather than
// failing the whole session over a typo'd --tz/SNEAT_TZ.
func TestLocationFromName(t *testing.T) {
	if got := locationFromName(""); got != time.Local {
		t.Fatalf("locationFromName(\"\") = %v, want time.Local", got)
	}
	if got := locationFromName("not-a-real-zone"); got != time.Local {
		t.Fatalf("locationFromName(garbage) = %v, want time.Local fallback", got)
	}
	loc := locationFromName("Pacific/Kiritimati")
	if loc == nil || loc.String() != "Pacific/Kiritimati" {
		t.Fatalf("locationFromName(\"Pacific/Kiritimati\") = %v, want that zone resolved", loc)
	}
}

// TestSlashCommands_MapsNameAndSummary is a pure mapping regression: each
// chat.CommandInfo becomes a chatshell.Command with the same Name and its
// Summary as Help.
func TestSlashCommands_MapsNameAndSummary(t *testing.T) {
	in := []chat.CommandInfo{
		{Name: "/space", Summary: "switch space"},
		{Name: "/help", Summary: "show help"},
	}
	out := slashCommands(in)
	if len(out) != 2 || out[0].Name != "/space" || out[0].Help != "switch space" || out[1].Name != "/help" || out[1].Help != "show help" {
		t.Fatalf("slashCommands(%+v) = %+v", in, out)
	}
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

// TestDefaultSpaceID_PseudoSpace covers the PSEUDO-SPACE ruling (founder
// bug: a session's persisted CurrentSpace of literally "family" sent
// Firestore reads at spaces/family/..., which PermissionDenied's because
// that is essentially never a space the signed-in user is a member of):
// "family"/"private" (case-insensitively) must resolve to the user's real
// space of that type, not be returned verbatim the way any other
// currentSpace value is.
func TestDefaultSpaceID_PseudoSpace(t *testing.T) {
	spaces := spaceIDFakeSpaces{
		"space-c": map[string]any{"type": "private"},
		"space-a": map[string]any{"type": "custom"},
		"space-b": map[string]any{"type": "family"},
	}

	t.Run("pseudo family resolves to the real family space", func(t *testing.T) {
		got := defaultSpaceID(context.Background(), spaces, "u1", "family")
		if got != "space-b" {
			t.Fatalf("got %q, want space-b", got)
		}
	})

	t.Run("pseudo private resolves to the real private space", func(t *testing.T) {
		got := defaultSpaceID(context.Background(), spaces, "u1", "private")
		if got != "space-c" {
			t.Fatalf("got %q, want space-c", got)
		}
	})

	t.Run("pseudo id matched case-insensitively", func(t *testing.T) {
		got := defaultSpaceID(context.Background(), spaces, "u1", "Family")
		if got != "space-b" {
			t.Fatalf("got %q, want space-b", got)
		}
	})

	t.Run("unresolvable pseudo id falls back to family-then-sorted", func(t *testing.T) {
		// No space has type "private" here, so spaceIDByType finds zero
		// matches and defaultSpaceID must fall back to its ordinary
		// family-then-sorted default (space-b, the only family space)
		// rather than returning the unresolved pseudo id "private" itself.
		noPrivate := spaceIDFakeSpaces{
			"space-a": map[string]any{"type": "custom"},
			"space-b": map[string]any{"type": "family"},
		}
		got := defaultSpaceID(context.Background(), noPrivate, "u1", "private")
		if got != "space-b" {
			t.Fatalf("got %q, want space-b (family fallback)", got)
		}
	})

	t.Run("ambiguous pseudo match falls back rather than guessing", func(t *testing.T) {
		twoFamilies := spaceIDFakeSpaces{
			"space-a": map[string]any{"type": "family"},
			"space-b": map[string]any{"type": "family"},
		}
		got := defaultSpaceID(context.Background(), twoFamilies, "u1", "family")
		if got != "space-a" && got != "space-b" {
			t.Fatalf("got %q, want one of the two family spaces (fallback picks by isFamilySpace, not a guess)", got)
		}
	})
}

// TestIsPseudoSpaceID covers the pseudo/real id distinction directly.
func TestIsPseudoSpaceID(t *testing.T) {
	for _, id := range []string{"family", "Family", "FAMILY", "private", "Private"} {
		if !isPseudoSpaceID(id) {
			t.Errorf("isPseudoSpaceID(%q) = false, want true", id)
		}
	}
	for _, id := range []string{"", "space-a", "familyish", "myprivate"} {
		if isPseudoSpaceID(id) {
			t.Errorf("isPseudoSpaceID(%q) = true, want false", id)
		}
	}
}

// TestIsFamilySpace covers the family-type match, a non-family type, and
// the defensively-nil/wrong-shape info branches.
func TestIsFamilySpace(t *testing.T) {
	if !isFamilySpace(map[string]any{"type": "family"}) {
		t.Error("type=family should be a family space")
	}
	if !isFamilySpace(map[string]any{"type": "FAMILY"}) {
		t.Error("type match should be case-insensitive")
	}
	if isFamilySpace(map[string]any{"type": "private"}) {
		t.Error("type=private should not be a family space")
	}
	if isFamilySpace(map[string]any{}) {
		t.Error("a map with no type key should not be a family space")
	}
	if isFamilySpace("not a map") {
		t.Error("a non-map info value should not be a family space")
	}
	if isFamilySpace(nil) {
		t.Error("nil info should not be a family space")
	}
}

// TestSidebarRender_IconsAndFallbacks covers every icon branch (todo,
// contact, an unknown type's default bullet), the empty-title-falls-back-
// to-type branch, and the width<=0 passthrough.
func TestSidebarRender_IconsAndFallbacks(t *testing.T) {
	if got := sidebarRender(session.EntityRef{Type: "todo", Title: "Buy milk"}, 200); !strings.Contains(got, "Buy milk") {
		t.Fatalf("todo render = %q", got)
	}
	if got := sidebarRender(session.EntityRef{Type: "contact", Title: "Alice"}, 200); !strings.Contains(got, "Alice") {
		t.Fatalf("contact render = %q", got)
	}
	if got := sidebarRender(session.EntityRef{Type: "unknown-kind", Title: "X"}, 200); !strings.Contains(got, "X") {
		t.Fatalf("unknown-type render = %q", got)
	}
	if got := sidebarRender(session.EntityRef{Type: "happening"}, 200); !strings.Contains(got, "happening") {
		t.Fatalf("empty-title render = %q, want it to fall back to the type", got)
	}
	if got := sidebarRender(session.EntityRef{Type: "happening", Title: "X"}, 0); !strings.Contains(got, "X") {
		t.Fatalf("width<=0 render = %q, want the unclamped line", got)
	}
}

// TestSidebarRender_TruncatesByDisplayWidthNotBytes covers m8: a title long
// enough to need truncation must not be cut mid-rune (the icon prefix is a
// multi-byte emoji, and len() counts bytes, not the runes lipgloss actually
// renders).
func TestSidebarRender_TruncatesByDisplayWidthNotBytes(t *testing.T) {
	ref := session.EntityRef{Type: "happening", Title: "A very long happening title that will not fit"}
	got := sidebarRender(ref, 10)
	if !utf8.ValidString(got) {
		t.Fatalf("truncated line is not valid UTF-8: %q", got)
	}
	if strings.Contains(got, "�") {
		t.Fatalf("truncated line contains a replacement rune (cut mid-rune): %q", got)
	}
	// Untruncated (width larger than the content) is returned as-is.
	full := sidebarRender(ref, 200)
	if !strings.Contains(full, "A very long happening title") {
		t.Fatalf("untruncated render = %q, want the full title", full)
	}
}
