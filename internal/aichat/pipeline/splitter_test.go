package pipeline

import (
	"errors"
	"strings"
	"testing"
)

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
// LLM stream would produce them. The block is the LAST thing in the reply
// (nothing follows its closing tag), so it is trailing and executes.
func TestSplitter_ActionBlockSplitAcrossDeltas(t *testing.T) {
	deltas := []string{
		"Sure, moving it now.",
		" <sneat-a",
		"ction>{\"kind\":\"calendar",
		".reschedule_happening\",\"refer",
		"ence\":\"dentist appointment\",\"slots\":{\"when\":\"Friday 16",
		":00\"}}</sneat-ac",
		"tion>",
	}
	s := &Splitter{}
	visible := feedAll(s, deltas)
	trailing, action, err := s.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	full := visible + trailing
	if full != "Sure, moving it now. " {
		t.Fatalf("got %q", full)
	}
	if action == nil {
		t.Fatal("expected a parsed action")
	}
	if action.Kind != "calendar.reschedule_happening" || action.Reference != "dentist appointment" || action.Slots["when"] != "Friday 16:00" {
		t.Fatalf("action = %+v", action)
	}
}

// TestSplitter_TextAfterActionBlockMeansNotTrailing_NoDataLoss covers S6
// coordinator ruling SPLITTER: a block followed by non-whitespace prose
// (here, "All done." after the closing tag) is NOT the trailing content of
// the reply, so it must NOT execute -- but every byte of that prose still
// reaches the visible transcript, unlike simply discarding the whole
// message.
func TestSplitter_TextAfterActionBlockMeansNotTrailing_NoDataLoss(t *testing.T) {
	deltas := []string{
		"Sure, moving it now.",
		" <sneat-action>{\"kind\":\"calendar.reschedule_happening\",\"reference\":\"dentist\",\"slots\":{\"when\":\"Friday 16:00\"}}</sneat-action>",
		" All done.",
	}
	s := &Splitter{}
	visible := feedAll(s, deltas)
	trailing, action, err := s.Finish()
	if !errors.Is(err, ErrActionNotTrailing) {
		t.Fatalf("Finish err = %v, want ErrActionNotTrailing", err)
	}
	if action != nil {
		t.Fatalf("expected no action once trailing prose invalidates it, got %+v", action)
	}
	full := visible + trailing
	if full != "Sure, moving it now.  All done." {
		t.Fatalf("got %q, want every byte preserved (no data loss)", full)
	}
}

// TestSplitter_WhitespaceOnlyAfterActionBlockStillCounts_AsTrailing covers
// the flip side: whitespace-only content (a trailing newline, say) after
// the closing tag does not disqualify the block -- only non-whitespace
// prose does.
func TestSplitter_WhitespaceOnlyAfterActionBlockStillCounts_AsTrailing(t *testing.T) {
	s := &Splitter{}
	visible := s.Feed(`Done. <sneat-action>{"kind":"todo.complete_todo"}</sneat-action>` + "\n")
	trailing, action, err := s.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if action == nil || action.Kind != "todo.complete_todo" {
		t.Fatalf("action = %+v, want it to still be trailing", action)
	}
	_ = visible + trailing
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

// TestSplitter_UnterminatedBlockIsAnError_NoDataLoss covers S6 coordinator
// ruling SPLITTER: the text since the (never-closed) start tag must be
// shown as prose, not silently discarded, even though the block itself is
// unusable (no action).
func TestSplitter_UnterminatedBlockIsAnError_NoDataLoss(t *testing.T) {
	s := &Splitter{}
	visible := feedAll(s, []string{"Working on it <sneat-action>{\"kind\":\"todo.complete_todo\""})
	trailing, action, err := s.Finish()
	if err == nil {
		t.Fatal("expected an error for an unterminated action block")
	}
	if action != nil {
		t.Fatalf("expected no action, got %+v", action)
	}
	if !strings.Contains(trailing, `{"kind":"todo.complete_todo"`) {
		t.Fatalf("trailing = %q, want the unterminated block's content shown as prose (no data loss)", trailing)
	}
	_ = visible
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

// TestSplitter_LastActionBlockWins covers coordinator ruling SPLITTER: only
// the block at the END of the reply counts. A model that emits two blocks
// (e.g. it reconsiders mid-answer) must not have the FIRST one win.
func TestSplitter_LastActionBlockWins(t *testing.T) {
	s := &Splitter{}
	visible := feedAll(s, []string{
		`First guess. <sneat-action>{"kind":"todo.complete_todo","reference":"milk"}</sneat-action>`,
		` Actually, <sneat-action>{"kind":"todo.complete_todo","reference":"bread"}</sneat-action>`,
	})
	trailing, action, err := s.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if got := visible + trailing; got != "First guess.  Actually, " {
		t.Fatalf("got %q", got)
	}
	if action == nil || action.Reference != "bread" {
		t.Fatalf("action = %+v, want the LAST block (bread)", action)
	}
}

// TestSplitter_LastBlockWinsOverEarlierMalformed covers the same ruling for
// the error case: an earlier malformed block must not poison a later
// well-formed one, since the well-formed one is the model's final answer.
func TestSplitter_LastBlockWinsOverEarlierMalformed(t *testing.T) {
	s := &Splitter{}
	_ = feedAll(s, []string{
		`<sneat-action>not json</sneat-action>`,
		`<sneat-action>{"kind":"todo.complete_todo","reference":"bread"}</sneat-action>`,
	})
	_, action, err := s.Finish()
	if err != nil {
		t.Fatalf("Finish: %v, want the later well-formed block to clear the earlier error", err)
	}
	if action == nil || action.Reference != "bread" {
		t.Fatalf("action = %+v, want the LAST (well-formed) block", action)
	}
}

// TestSplitter_LaterMalformedBlockOverridesEarlierGood mirrors the above in
// the other direction: a LATER malformed block still wins the "last block"
// rule, reporting the error rather than silently keeping the earlier good
// action.
func TestSplitter_LaterMalformedBlockOverridesEarlierGood(t *testing.T) {
	s := &Splitter{}
	_ = feedAll(s, []string{
		`<sneat-action>{"kind":"todo.complete_todo","reference":"bread"}</sneat-action>`,
		`<sneat-action>not json</sneat-action>`,
	})
	_, action, err := s.Finish()
	if err == nil {
		t.Fatal("expected the later malformed block to win and report an error")
	}
	if action != nil {
		t.Fatalf("expected no action once the last block is malformed, got %+v", action)
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
