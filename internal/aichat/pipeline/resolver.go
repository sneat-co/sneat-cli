package pipeline

import (
	"context"
	"fmt"

	"github.com/strongo/aichat/ai/decision"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/data"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

// Resolver turns a decision.Reference (or a splitter.Action's equivalent
// reference) into the real entity/entities it names. Jev/the main LLM may
// identify "my dentist appointment tomorrow", but only Resolver decides what
// that actually points at -- it never trusts a model-supplied entity ID
// because none exists in a Reference to begin with.
type Resolver struct {
	Readers data.Readers
}

// Outcome is the result of resolving one reference.
type Outcome int

const (
	// OutcomeNone means no candidate was found -- the caller should ask the
	// user to clarify rather than guess.
	OutcomeNone Outcome = iota
	// OutcomeOne means exactly one candidate was found -- the caller may
	// proceed (subject to its own confirmation policy for destructive/
	// ambiguous actions).
	OutcomeOne
	// OutcomeMany means several plausible candidates were found -- the caller
	// should present a choice control, never guess.
	OutcomeMany
)

// Result is what Resolve found.
type Result struct {
	Outcome    Outcome
	Candidates []session.EntityRef // len 0, 1, or >1 matching Outcome
}

// Resolve resolves ref against real data in spaceID. A pronoun reference
// ("it", "that") resolves from session state's Candidates() (focused entity,
// selection, the sole last-shown entity, the previous action's target, then
// sidebar pins), filtered to ref.Kind. An expression reference searches the
// matching reader by title/name.
func (r Resolver) Resolve(ctx context.Context, ref decision.Reference, st session.State, spaceID string) (Result, error) {
	if ref.Pronoun {
		var out []session.EntityRef
		for _, c := range st.Candidates() {
			if c.Type == ref.Kind {
				out = append(out, c)
			}
		}
		return classify(out), nil
	}
	if ref.Expression == "" {
		return Result{Outcome: OutcomeNone}, nil
	}

	switch ref.Kind {
	case sneatdomain.EntityHappening:
		if r.Readers.Happenings == nil {
			return Result{Outcome: OutcomeNone}, fmt.Errorf("pipeline: no happenings reader configured")
		}
		hs, err := r.Readers.Happenings.FindByTitle(ctx, spaceID, ref.Expression)
		if err != nil {
			return Result{}, err
		}
		return classify(toEntityRefs(hs, func(h data.Happening) session.EntityRef {
			return session.EntityRef{Type: sneatdomain.EntityHappening, Title: h.Title,
				Keys: map[string]string{"spaceID": h.SpaceID, "happeningID": h.ID}}
		})), nil
	case sneatdomain.EntityTodo:
		if r.Readers.Todos == nil {
			return Result{Outcome: OutcomeNone}, fmt.Errorf("pipeline: no todos reader configured")
		}
		ts, err := r.Readers.Todos.FindByTitle(ctx, spaceID, ref.Expression)
		if err != nil {
			return Result{}, err
		}
		return classify(toEntityRefs(ts, func(td data.Todo) session.EntityRef {
			return session.EntityRef{Type: sneatdomain.EntityTodo, Title: td.Title,
				Keys: map[string]string{"spaceID": td.SpaceID, "list": td.List, "itemID": td.ID}}
		})), nil
	case sneatdomain.EntityContact:
		if r.Readers.Contacts == nil {
			return Result{Outcome: OutcomeNone}, fmt.Errorf("pipeline: no contacts reader configured")
		}
		cs, err := r.Readers.Contacts.FindByName(ctx, spaceID, ref.Expression)
		if err != nil {
			return Result{}, err
		}
		return classify(toEntityRefs(cs, func(c data.Contact) session.EntityRef {
			return session.EntityRef{Type: sneatdomain.EntityContact, Title: c.Name,
				Keys: map[string]string{"spaceID": c.SpaceID, "contactID": c.ID}}
		})), nil
	default:
		return Result{Outcome: OutcomeNone}, fmt.Errorf("pipeline: unknown reference kind %q", ref.Kind)
	}
}

func classify(refs []session.EntityRef) Result {
	switch len(refs) {
	case 0:
		return Result{Outcome: OutcomeNone}
	case 1:
		return Result{Outcome: OutcomeOne, Candidates: refs}
	default:
		return Result{Outcome: OutcomeMany, Candidates: refs}
	}
}

func toEntityRefs[T any](items []T, conv func(T) session.EntityRef) []session.EntityRef {
	out := make([]session.EntityRef, 0, len(items))
	for _, it := range items {
		out = append(out, conv(it))
	}
	return out
}
