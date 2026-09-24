package pipeline

import (
	"context"
	"fmt"
	"iter"
	"slices"
	"strings"
	"unicode"

	"github.com/strongo/aichat/ai"
	"github.com/strongo/aichat/ai/ctxmgr"
	"github.com/strongo/aichat/ai/decision"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

// maxDynamicItems caps how many happenings/todos a dynamic context block
// lists (brief §7/S7 coordinator ruling): the LLM needs enough to answer
// without inventing facts, not a full record dump on every turn that needs
// one.
const maxDynamicItems = 50

// HistoryTurns is how many past user/assistant EXCHANGES StreamRequest
// includes ahead of the current turn (brief §7 coordinator ruling S7: "last
// N (8) turns of history"). A turn is one user message plus its assistant
// reply, so this is up to 2*HistoryTurns ai.Message values.
const HistoryTurns = 8

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
// every turn that needs it and is never cached. Happenings/todos are capped
// at maxDynamicItems. fullContacts controls whether the contacts scope sends
// the full name list or just a count (brief §7/S7 coordinator ruling: full
// contacts only when the contacts scope is actually required or text
// mentions a person; a count otherwise, so an unrelated turn does not pay
// for every contact's name).
func (p Pipeline) DynamicBlocks(ctx context.Context, spaceID string, scopes []string, fullContacts bool) []ai.ContextBlock {
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
			truncated := len(hs) > maxDynamicItems
			if truncated {
				hs = hs[:maxDynamicItems]
			}
			text := "Upcoming happenings:\n"
			for _, h := range hs {
				text += fmt.Sprintf("- %s (%s)\n", h.Title, h.Start.Format("Mon 2006-01-02 15:04"))
			}
			if truncated {
				text += fmt.Sprintf("(showing the first %d)\n", maxDynamicItems)
			}
			out = append(out, ai.ContextBlock{Scope: scope, Kind: ai.ContextDynamic, Name: "relevant_happenings", Text: text})
		case sneatdomain.ModuleTodo:
			if p.Readers.Todos == nil {
				continue
			}
			text := "Todos:\n"
			if items, err := p.Readers.Todos.List(ctx, spaceID, "do"); err == nil {
				truncated := len(items) > maxDynamicItems
				if truncated {
					items = items[:maxDynamicItems]
				}
				for _, it := range items {
					text += fmt.Sprintf("- %s [done=%v]\n", it.Title, it.Done)
				}
				if truncated {
					text += fmt.Sprintf("(showing the first %d)\n", maxDynamicItems)
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
			var text string
			if fullContacts {
				text = "Contacts:\n"
				for _, c := range cs {
					text += "- " + c.Name + "\n"
				}
			} else {
				text = fmt.Sprintf("%d contact(s) in this space (ask to list/search them for names).\n", len(cs))
			}
			out = append(out, ai.ContextBlock{Scope: scope, Kind: ai.ContextDynamic, Name: "contacts", Text: text})
		}
	}
	return out
}

// sessionBlock builds the "now"/TZ/focused-selection-sidebar dynamic context
// block (brief §7/S7 coordinator ruling): the current time in the session's
// zone, and the display titles of whatever the user is currently looking at
// or has pinned, so "move it to Friday" and similar resolve against what the
// LLM was actually told, not just what Sneat's resolver later works out from
// Keys alone. st may be nil (no session yet); a nil/empty state still yields
// the "now" line.
func (p Pipeline) sessionBlock(st *session.State) ai.ContextBlock {
	now := p.now()
	tz := p.TZ
	if tz == "" {
		tz = "local"
	}
	text := fmt.Sprintf("Now: %s (%s).\n", now.Format("Mon 2006-01-02 15:04"), tz)
	if st == nil {
		return ai.ContextBlock{Scope: sneatdomain.ModuleGeneral, Kind: ai.ContextDynamic, Name: "session", Text: text}
	}
	if st.Focused != nil && st.Focused.Title != "" {
		text += "Focused: " + st.Focused.Title + "\n"
	}
	if len(st.Selection) > 0 {
		text += "Selected: " + entityTitles(st.Selection) + "\n"
	}
	if len(st.Sidebar) > 0 {
		text += "Sidebar (pinned): " + entityTitles(st.Sidebar) + "\n"
	}
	return ai.ContextBlock{Scope: sneatdomain.ModuleGeneral, Kind: ai.ContextDynamic, Name: "session", Text: text}
}

func entityTitles(refs []session.EntityRef) string {
	titles := make([]string, 0, len(refs))
	for _, r := range refs {
		title := r.Title
		if title == "" {
			title = r.Type
		}
		titles = append(titles, title)
	}
	return strings.Join(titles, ", ")
}

// mentionsPerson is a small heuristic (not NLP, per brief §5's "no giant
// regex NLP engine"): a capitalized word that is not the first word of the
// text is treated as a plausible person mention ("ask Alice", "tell Bob"),
// same spirit as sentence-initial capitalization not counting. It only
// widens the SelectAll path's contacts inclusion; a false negative just
// falls back to a count block (DynamicBlocks' fullContacts=false), never a
// hard failure.
func mentionsPerson(text string) bool {
	words := strings.Fields(text)
	for i, w := range words {
		if i == 0 {
			continue
		}
		w = strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) })
		// "I" is the one common capitalized pronoun that is not sentence-
		// initial; everything else this heuristic exists to catch is a name.
		if w == "" || w == "I" || len([]rune(w)) < 2 {
			continue
		}
		r := []rune(w)
		if unicode.IsUpper(r[0]) {
			return true
		}
	}
	return false
}

// StreamRequest builds the ai.ChatRequest for a main-LLM turn: SelectAll
// (every cached static scope) when d is nil (no decision at all -- Jev off/
// abstained/timeout/malformed), or Select(d.RequiredScopes, ...) when a
// decision named required scopes but asked for LLM handling. focusedScopes
// are the modules of the session's focused/sidebar entities (pinnedScopes),
// included even when not required, per brief §4/§7. history is the last
// HistoryTurns exchanges (brief §7/S7 coordinator ruling), oldest first,
// sent ahead of the current user message -- callers own trimming it to that
// bound (chatapp's handler does; see its history field) since Pipeline
// itself is a stateless value with nowhere to keep it between turns.
func (p Pipeline) StreamRequest(ctx context.Context, text string, st *session.State, spaceID string, d *decision.Decision, focusedScopes []string, history []ai.Message) (ai.ChatRequest, ctxmgr.Report) {
	available := append([]ai.ContextBlock{}, StaticBlocks()...)
	available = append(available, p.sessionBlock(st))

	// The SelectAll (no-decision) path defaults to calendar+todo as its
	// REQUIRED scopes -- contacts is never assumed required just because Jev
	// is unavailable. Either path still sends a contacts block on every turn
	// (dynamicScopes always includes it below) so the LLM always knows contacts
	// exist; whether that block is the full name list or just a count is the
	// separate fullContacts decision (S7 coordinator ruling: full contacts
	// only when the scope is actually required or text plausibly mentions a
	// person, a count otherwise).
	var required []string
	if d != nil {
		required = d.RequiredScopes
	} else {
		required = []string{sneatdomain.ModuleCalendar, sneatdomain.ModuleTodo}
	}
	fullContacts := slices.Contains(required, sneatdomain.ModuleContacts) || mentionsPerson(text)

	dynamicScopes := uniqueStrings(append(append([]string{}, required...), focusedScopes...))
	if !slices.Contains(dynamicScopes, sneatdomain.ModuleContacts) {
		dynamicScopes = append(dynamicScopes, sneatdomain.ModuleContacts)
	}
	available = append(available, p.DynamicBlocks(ctx, spaceID, dynamicScopes, fullContacts)...)

	// pinnedScopes is what ctxmgr.Select/SelectAll uses (beyond required) to
	// decide which DYNAMIC blocks survive -- it always carries contacts too,
	// so the count-or-full contacts block above is never dropped by ctxmgr
	// even on a turn where contacts is not itself a required/focused scope.
	pinnedScopes := uniqueStrings(append(append([]string{}, focusedScopes...), sneatdomain.ModuleContacts))

	var blocks []ai.ContextBlock
	var report ctxmgr.Report
	if p.CtxMgr == nil {
		blocks = available
	} else if d == nil {
		blocks, report = p.CtxMgr.SelectAll(available, pinnedScopes)
	} else {
		blocks, report = p.CtxMgr.Select(required, available, pinnedScopes)
	}

	messages := make([]ai.Message, 0, 1+len(history))
	messages = append(messages, boundHistory(history)...)
	messages = append(messages, ai.Message{Role: ai.RoleUser, Text: text})

	req := ai.ChatRequest{
		Product:  p.Product,
		System:   sneatActionInstruction,
		Context:  blocks,
		Messages: messages,
	}
	return req, report
}

// boundHistory trims h to the last HistoryTurns exchanges (2*HistoryTurns
// messages), keeping the most recent ones -- a caller's history is expected
// to already be roughly this size (chatapp's handler trims as it appends),
// this is a defensive final cap so StreamRequest itself never sends an
// unbounded transcript regardless of caller discipline.
func boundHistory(h []ai.Message) []ai.Message {
	cap := HistoryTurns * 2
	if len(h) <= cap {
		return h
	}
	return h[len(h)-cap:]
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
