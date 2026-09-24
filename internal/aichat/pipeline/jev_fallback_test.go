package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/strongo/aichat/ai/decision"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/data"
	"github.com/sneat-co/sneat-cli/internal/aichat/rules"
)

// TestScenario13 covers brief §18 scenario 13 and §9's hard requirement that
// Jev is never a functional dependency: sneat chat must keep working when a
// decision.Provider standing in for Jev times out, returns a malformed
// (taxonomy-invalid) decision, or returns one below the confidence floor.
// decision.Chain itself classifies and falls through each of these (see
// ai/decision/decision.go's Decide); this test proves the PRODUCT chain --
// a fake Jev ahead of Sneat's own deterministic rules.Provider -- degrades
// transparently rather than erroring or hanging.

// fakeJev is a decision.Provider whose behaviour is set per test.
type fakeJev struct {
	timeout      bool
	malformed    bool
	lowConfident bool
}

func (f *fakeJev) Name() string { return "fake-jev" }

func (f *fakeJev) Decide(ctx context.Context, req decision.Request) (decision.Decision, bool, error) {
	if f.timeout {
		select {
		case <-ctx.Done():
			return decision.Decision{}, false, ctx.Err()
		case <-time.After(2 * time.Second):
			return decision.Decision{}, false, nil
		}
	}
	if f.malformed {
		// A module/intent the Sneat taxonomy never declared -- decision.Validate
		// rejects this, and the chain treats it as "invalid", not a crash.
		return decision.Decision{
			Module:      decision.Scored{Value: "not-a-real-module", Confidence: 1},
			Intent:      decision.Scored{Value: "not-a-real-intent", Confidence: 1},
			Interaction: decision.InteractionCommand, CanHandleDeterministically: true,
		}, true, nil
	}
	if f.lowConfident {
		return decision.Decision{
			Module:      decision.Scored{Value: "calendar", Confidence: 0.1},
			Intent:      decision.Scored{Value: "show_day", Confidence: 0.1},
			Interaction: decision.InteractionCommand, CanHandleDeterministically: true,
			Presentation: "day_calendar",
		}, true, nil
	}
	return decision.Decision{}, false, nil
}

// DecisionTimeout gives the fake a short per-provider timeout so the
// "timeout" case does not make this test slow (matches the real cloud-decision
// provider's own DecisionTimeout pattern).
func (f *fakeJev) DecisionTimeout() time.Duration { return 20 * time.Millisecond }

func pipelineWithJevAndRules(jev *fakeJev) (Pipeline, *session.State) {
	readers := contactsTestReaders()
	readers.Happenings = &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", Title: "Team standup", Start: fixedNow()},
	}}
	return Pipeline{
		Chain:    decision.Chain{Providers: []decision.Provider{jev, rules.New()}},
		Resolver: Resolver{Readers: readers},
		Readers:  readers,
		Now:      fixedNow,
	}, &session.State{}
}

func TestScenario13_JevTimeout_FallsBackToRules(t *testing.T) {
	p, st := pipelineWithJevAndRules(&fakeJev{timeout: true})
	out, err := p.Turn(context.Background(), "find contact bob", st, "sp1")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if out.NeedsLLM {
		t.Fatal("expected rules.Provider to decide once Jev times out")
	}
	if len(out.Entities) != 1 || out.Entities[0].Keys["contactID"] != "c3" {
		t.Fatalf("Entities = %+v, want Bob resolved via the rules fallback", out.Entities)
	}
	if out.Trace.DecidedBy != rules.Name {
		t.Fatalf("Trace.DecidedBy = %q, want %q", out.Trace.DecidedBy, rules.Name)
	}
	// The timed-out attempt is still recorded, for diagnostics (S11).
	if len(out.Trace.Attempts) < 2 || out.Trace.Attempts[0].Provider != "fake-jev" {
		t.Fatalf("Trace.Attempts = %+v, want the fake-jev attempt recorded first", out.Trace.Attempts)
	}
}

func TestScenario13_JevMalformed_FallsBackToRules(t *testing.T) {
	p, st := pipelineWithJevAndRules(&fakeJev{malformed: true})
	out, err := p.Turn(context.Background(), "show my calendar today", st, "sp1")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if out.NeedsLLM {
		t.Fatal("expected rules.Provider to decide once Jev's decision is malformed/invalid")
	}
	if out.Presentation != "day_calendar" {
		t.Fatalf("Presentation = %q, want the deterministic show_day answer", out.Presentation)
	}
	if out.Trace.Attempts[0].Outcome != "invalid" {
		t.Fatalf("first attempt outcome = %q, want \"invalid\"", out.Trace.Attempts[0].Outcome)
	}
}

func TestScenario13_JevLowConfidence_FallsBackToRules(t *testing.T) {
	p, st := pipelineWithJevAndRules(&fakeJev{lowConfident: true})
	out, err := p.Turn(context.Background(), "show my calendar today", st, "sp1")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if out.NeedsLLM {
		t.Fatal("expected rules.Provider to decide once Jev's confidence is below the floor")
	}
	if out.Trace.Attempts[0].Outcome != "low_confidence" {
		t.Fatalf("first attempt outcome = %q, want \"low_confidence\"", out.Trace.Attempts[0].Outcome)
	}
	if out.Trace.DecidedBy != rules.Name {
		t.Fatalf("DecidedBy = %q, want the rules fallback", out.Trace.DecidedBy)
	}
}

// TestScenario13_JevAndRulesBothAbstain_NeedsLLM covers the case neither
// deterministic provider can answer at all (e.g. Jev disabled/unavailable
// AND the text is outside the rules phrase table): Turn reports NeedsLLM
// rather than erroring, per brief §9's hard requirement.
func TestScenario13_JevAndRulesBothAbstain_NeedsLLM(t *testing.T) {
	p, st := pipelineWithJevAndRules(&fakeJev{}) // abstains (ok=false)
	out, err := p.Turn(context.Background(), "what is the meaning of life", st, "sp1")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if !out.NeedsLLM {
		t.Fatal("expected NeedsLLM when both deterministic providers abstain")
	}
}

// TestNoJev_SneatChatStillWorks covers brief §9's "sneat chat --no-jev"
// requirement directly: a chain with ONLY Sneat's own rules.Provider (no
// Jev/cloud-decision at all) still answers deterministic turns.
func TestNoJev_SneatChatStillWorks(t *testing.T) {
	readers := contactsTestReaders()
	readers.Happenings = &data.FakeHappenings{Items: []data.Happening{
		{ID: "h1", SpaceID: "sp1", Title: "Team standup", Start: fixedNow()},
	}}
	p := Pipeline{
		Chain:    decision.Chain{Providers: []decision.Provider{rules.New()}}, // no Jev
		Resolver: Resolver{Readers: readers},
		Readers:  readers,
		Now:      fixedNow,
	}
	st := &session.State{}
	out, err := p.Turn(context.Background(), "show my calendar today", st, "sp1")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if out.NeedsLLM {
		t.Fatal("--no-jev must not lose deterministic handling")
	}
}
