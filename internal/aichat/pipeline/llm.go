package pipeline

import (
	"context"
	"fmt"
	"iter"

	"github.com/strongo/aichat/ai"
	"github.com/strongo/aichat/ai/ctxmgr"
	"github.com/strongo/aichat/ai/decision"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

// sneatActionInstruction is appended to the system prompt so the main LLM
// follows the product convention: stream user-facing text, then optionally
// end with exactly one <sneat-action>{...}</sneat-action> block (parsed by
// Splitter/HandleAction) describing a semantic action -- never a storage
// write, never a model-invented entity ID.
const sneatActionInstruction = `When the user's request implies a concrete action (creating, changing, completing, or cancelling a calendar happening, todo, or to-buy item), end your reply with exactly one block of this exact form, and nothing after it:
<sneat-action>{"kind":"<module>.<intent>","reference":"<free-text description of the target, or omit for a new item>","pronoun":<true if the user said "it"/"that", else omit>,"slots":{"when":"...","title":"..."},"presentation":"<optional presentation hint>"}</sneat-action>
Only ONE such block per reply. Never invent an entity ID -- name what the user means in words (reference), the app resolves it against real data. Omit the block entirely for a plain question or when no action is implied.`

// StaticBlocks are Sneat's per-module skill descriptions -- the ai.ContextBlock
// values ctxmgr.Manager caches across turns. Kept short and product-owned
// (not user content), per brief §7's static/dynamic split.
func StaticBlocks() []ai.ContextBlock {
	return []ai.ContextBlock{
		{Scope: sneatdomain.ModuleCalendar, Kind: ai.ContextStatic, Name: "skill", Text: "Calendar module: happenings have a title and one or more time slots. Intents: add/cancel/reschedule/update/find happening, show day/week/upcoming."},
		{Scope: sneatdomain.ModuleTodo, Kind: ai.ContextStatic, Name: "skill", Text: "Todo module: two lists, \"do\" (todo) and \"buy\" (shopping). Intents: add/complete/reopen/update/delete/find/list todo, add/list to-buy."},
		{Scope: sneatdomain.ModuleContacts, Kind: ai.ContextStatic, Name: "skill", Text: "Contacts module: people in the user's space. Intents: find/list/show contact."},
	}
}

// DynamicBlocks fetches a short per-scope textual summary for scope (the
// data the main LLM needs to answer without inventing facts) -- deliberately
// lightweight (title/count only, not a full record dump) since it is sent on
// every turn that needs it and is never cached.
func (p Pipeline) DynamicBlocks(ctx context.Context, spaceID string, scopes []string) []ai.ContextBlock {
	var out []ai.ContextBlock
	now := p.now()
	for _, scope := range scopes {
		switch scope {
		case sneatdomain.ModuleCalendar:
			if p.Readers.Happenings == nil {
				continue
			}
			hs, err := p.Readers.Happenings.Window(ctx, spaceID, now.AddDate(0, 0, -1), now.AddDate(0, 0, 14))
			if err != nil {
				continue
			}
			text := "Upcoming happenings:\n"
			for _, h := range hs {
				text += fmt.Sprintf("- %s (%s)\n", h.Title, h.Start.Format("Mon 2006-01-02 15:04"))
			}
			out = append(out, ai.ContextBlock{Scope: scope, Kind: ai.ContextDynamic, Name: "relevant_happenings", Text: text})
		case sneatdomain.ModuleTodo:
			if p.Readers.Todos == nil {
				continue
			}
			text := "Todos:\n"
			if items, err := p.Readers.Todos.List(ctx, spaceID, "do"); err == nil {
				for _, it := range items {
					text += fmt.Sprintf("- %s [done=%v]\n", it.Title, it.Done)
				}
			}
			out = append(out, ai.ContextBlock{Scope: scope, Kind: ai.ContextDynamic, Name: "todos", Text: text})
		case sneatdomain.ModuleContacts:
			if p.Readers.Contacts == nil {
				continue
			}
			cs, err := p.Readers.Contacts.List(ctx, spaceID)
			if err != nil {
				continue
			}
			text := "Contacts:\n"
			for _, c := range cs {
				text += "- " + c.Name + "\n"
			}
			out = append(out, ai.ContextBlock{Scope: scope, Kind: ai.ContextDynamic, Name: "contacts", Text: text})
		}
	}
	return out
}

// StreamRequest builds the ai.ChatRequest for a main-LLM turn: SelectAll
// (every cached static scope) when d is nil (no decision at all -- Jev off/
// abstained/timeout/malformed), or Select(d.RequiredScopes, ...) when a
// decision named required scopes but asked for LLM handling. focusedScopes
// are the modules of the session's focused/sidebar entities (pinnedScopes),
// included even when not required, per brief §4/§7.
func (p Pipeline) StreamRequest(ctx context.Context, text string, st *session.State, spaceID string, d *decision.Decision, focusedScopes []string) (ai.ChatRequest, ctxmgr.Report) {
	available := append([]ai.ContextBlock{}, StaticBlocks()...)
	var required []string
	if d != nil {
		required = d.RequiredScopes
	} else {
		required = []string{sneatdomain.ModuleCalendar, sneatdomain.ModuleTodo, sneatdomain.ModuleContacts}
	}
	available = append(available, p.DynamicBlocks(ctx, spaceID, uniqueStrings(append(append([]string{}, required...), focusedScopes...)))...)

	var blocks []ai.ContextBlock
	var report ctxmgr.Report
	if p.CtxMgr == nil {
		blocks = available
	} else if d == nil {
		blocks, report = p.CtxMgr.SelectAll(available, focusedScopes)
	} else {
		blocks, report = p.CtxMgr.Select(required, available, focusedScopes)
	}

	system := sneatActionInstruction
	req := ai.ChatRequest{
		Product:  p.Product,
		System:   system,
		Context:  blocks,
		Messages: []ai.Message{{Role: ai.RoleUser, Text: text}},
	}
	return req, report
}

// Stream starts the main-LLM turn and returns its stream, wrapped so
// <sneat-action> text never reaches the caller's rendering of text deltas
// (see Splitter) -- the caller reads the parsed action from splitter.Finish()
// once the stream ends (e.g. from a StreamObserver's EventCompleted).
func (p Pipeline) Stream(ctx context.Context, req ai.ChatRequest) (iter.Seq2[ai.Event, error], *Splitter) {
	splitter := &Splitter{}
	if p.LLM == nil {
		return func(yield func(ai.Event, error) bool) {
			yield(ai.Event{}, fmt.Errorf("pipeline: no LLM provider configured"))
		}, splitter
	}
	src := p.LLM.Stream(ctx, req)
	return splitStream(src, splitter), splitter
}

// splitStream wraps src so a text delta belonging to a trailing
// <sneat-action> block never reaches the caller as an EventTextDelta; any
// text discovered NOT to be part of a tag (Splitter.Feed's return value) is
// re-emitted as an ordinary EventTextDelta, and any trailing safe text
// Splitter.Finish uncovers at EventCompleted is flushed before it.
func splitStream(src iter.Seq2[ai.Event, error], sp *Splitter) iter.Seq2[ai.Event, error] {
	return func(yield func(ai.Event, error) bool) {
		for ev, err := range src {
			if err != nil {
				yield(ev, err)
				return
			}
			if ev.Type == ai.EventTextDelta {
				if visible := sp.Feed(ev.Text); visible != "" {
					if !yield(ai.Event{Type: ai.EventTextDelta, Text: visible}, nil) {
						return
					}
				}
				continue
			}
			if ev.Type == ai.EventCompleted {
				if trailing, _, _ := sp.Finish(); trailing != "" {
					if !yield(ai.Event{Type: ai.EventTextDelta, Text: trailing}, nil) {
						return
					}
				}
			}
			if !yield(ev, nil) {
				return
			}
		}
	}
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
