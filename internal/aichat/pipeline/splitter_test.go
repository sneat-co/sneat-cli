package pipeline

import "testing"

func feedAll(s *Splitter, deltas []string) string {
	var out string
	for _, d := range deltas {
		out += s.Feed(d)
	}
	return out
}

func TestSplitter_PlainTextOnly(t *testing.T) {
	s := &Splitter{}
	visible := feedAll(s, []string{"You have ", "five things ", "scheduled."})
	trailing, action, err := s.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if action != nil {
		t.Fatalf("expected no action, got %+v", action)
	}
	if got := visible + trailing; got != "You have five things scheduled." {
		t.Fatalf("got %q", got)
	}
}

func TestSplitter_ActionBlockWholeInOneDelta(t *testing.T) {
	s := &Splitter{}
	visible := s.Feed(`Moved it. <sneat-action>{"kind":"calendar.reschedule_happening","reference":"dentist","slots":{"when":"Friday 16:00"}}</sneat-action>`)
	trailing, action, err := s.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if got := visible + trailing; got != "Moved it. " {
		t.Fatalf("visible text leaked action block: %q", got)
	}
	if action == nil || action.Kind != "calendar.reschedule_happening" || action.Reference != "dentist" || action.Slots["when"] != "Friday 16:00" {
		t.Fatalf("action = %+v", action)
	}
}

// TestSplitter_ActionBlockSplitAcrossDeltas is the scenario the MVP brief
// names explicitly: the start tag, the JSON body, and the end tag each
// arrive fragmented across several stream deltas, as a real token-by-token
// LLM stream would produce them.
func TestSplitter_ActionBlockSplitAcrossDeltas(t *testing.T) {
	deltas := []string{
		"Sure, moving it now.",
		" <sneat-a",
		"ction>{\"kind\":\"calendar",
		".reschedule_happening\",\"refer",
		"ence\":\"dentist appointment\",\"slots\":{\"when\":\"Friday 16",
		":00\"}}</sneat-ac",
		"tion>",
		" All done.",
	}
	s := &Splitter{}
	visible := feedAll(s, deltas)
	trailing, action, err := s.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	full := visible + trailing
	if full != "Sure, moving it now.  All done." {
		t.Fatalf("got %q", full)
	}
	if action == nil {
		t.Fatal("expected a parsed action")
	}
	if action.Kind != "calendar.reschedule_happening" || action.Reference != "dentist appointment" || action.Slots["when"] != "Friday 16:00" {
		t.Fatalf("action = %+v", action)
	}
}

func TestSplitter_NoTrailingTextAfterActionAtEnd(t *testing.T) {
	s := &Splitter{}
	visible := feedAll(s, []string{"Done. <sneat-action>{\"kind\":\"todo.complete_todo\"}", "</sneat-action>"})
	trailing, action, err := s.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if visible+trailing != "Done. " {
		t.Fatalf("got %q", visible+trailing)
	}
	if action == nil || action.Kind != "todo.complete_todo" {
		t.Fatalf("action = %+v", action)
	}
}

func TestSplitter_UnterminatedBlockIsAnError(t *testing.T) {
	s := &Splitter{}
	_ = feedAll(s, []string{"Working on it <sneat-action>{\"kind\":\"todo.complete_todo\""})
	_, action, err := s.Finish()
	if err == nil {
		t.Fatal("expected an error for an unterminated action block")
	}
	if action != nil {
		t.Fatalf("expected no action, got %+v", action)
	}
}

func TestSplitter_MalformedJSONIsAnError(t *testing.T) {
	s := &Splitter{}
	_ = feedAll(s, []string{"Text. <sneat-action>not json</sneat-action>"})
	_, action, err := s.Finish()
	if err == nil {
		t.Fatal("expected an error for malformed action JSON")
	}
	if action != nil {
		t.Fatalf("expected no action, got %+v", action)
	}
}

func TestSplitter_TagLookalikeIsNotConsumed(t *testing.T) {
	s := &Splitter{}
	visible := feedAll(s, []string{"Use <sneat-actio", "n-like> tags carefully."})
	trailing, action, err := s.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if got := visible + trailing; got != "Use <sneat-action-like> tags carefully." {
		t.Fatalf("got %q", got)
	}
	if action != nil {
		t.Fatalf("expected no action, got %+v", action)
	}
}
