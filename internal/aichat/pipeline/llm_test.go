package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/strongo/aichat/ai"
	"github.com/strongo/aichat/ai/cloud"
	"github.com/strongo/aichat/ai/cloudproto"
	"github.com/strongo/aichat/ai/decision"
	"github.com/strongo/aichat/ai/openaicompat"
	"github.com/strongo/aichat/ai/session"

	"github.com/sneat-co/sneat-cli/internal/aichat/data"
	"github.com/sneat-co/sneat-cli/internal/aichat/sneatdomain"
)

func sseWrite(w http.ResponseWriter, data string) {
	_, _ = io.WriteString(w, "data: "+data+"\n\n")
	w.(http.Flusher).Flush()
}

func drain(t *testing.T, seq func(func(ai.Event, error) bool)) (text string, events []ai.Event) {
	t.Helper()
	for ev, err := range seq {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		events = append(events, ev)
		if ev.Type == ai.EventTextDelta {
			text += ev.Text
		}
	}
	return text, events
}

// TestStream_BYOKOpenAICompatible_ThroughPipeline covers MVP scenario 11: a
// BYOK OpenAI-compatible endpoint streaming through the real pipeline
// (Pipeline.Stream -> the real ai/openaicompat.Provider -> splitStream),
// including a trailing <sneat-action> block hidden from the visible text.
func TestStream_BYOKOpenAICompatible_ThroughPipeline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		sseWrite(w, `{"model":"gpt-5","choices":[{"delta":{"content":"You have "}}]}`)
		sseWrite(w, `{"model":"gpt-5","choices":[{"delta":{"content":"5 things. "}}]}`)
		sseWrite(w, `{"model":"gpt-5","choices":[{"delta":{"content":"<sneat-action>{\"kind\":\"todo.list_todos\"}</sneat-action>"}}]}`)
		sseWrite(w, "[DONE]")
	}))
	defer srv.Close()

	p := Pipeline{
		LLM:     openaicompat.New(openaicompat.Config{BaseURL: srv.URL, APIKey: "sk-test", Model: "gpt-5"}),
		Product: "sneat",
	}
	req, _ := p.StreamRequest(context.Background(), "what's on my plate", nil, "sp1", nil, nil, nil)
	seq, splitter := p.Stream(context.Background(), req)
	text, _ := drain(t, seq)
	if text != "You have 5 things. " {
		t.Fatalf("text = %q, want the action block hidden", text)
	}
	trailing, action, err := splitter.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if trailing != "" {
		t.Fatalf("trailing = %q, want empty (already flushed via EventCompleted)", trailing)
	}
	if action == nil || action.Kind != "todo.list_todos" {
		t.Fatalf("action = %+v", action)
	}
}

// TestStream_Cloud_ThroughPipeline covers MVP scenario 12: Sneat AI Cloud
// streaming through api.sneat.cloud's cloudproto protocol, via the real
// pipeline and the real ai/cloud.Client, including usage/allowance in the
// event stream a status line would read.
func TestStream_Cloud_ThroughPipeline(t *testing.T) {
	var gotProduct string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProduct = r.Header.Get(cloudproto.HeaderProduct)
		w.Header().Set("Content-Type", cloudproto.ContentTypeSSE)
		w.WriteHeader(http.StatusOK)
		_ = cloudproto.WriteEvent(w, ai.Event{Type: ai.EventStarted, Provider: "cloud", Model: "auto"})
		_ = cloudproto.WriteEvent(w, ai.Event{Type: ai.EventTextDelta, Text: "You have 5 things."})
		_ = cloudproto.WriteEvent(w, ai.Event{Type: ai.EventCompleted, Usage: &ai.Usage{
			InputTokens: 42, OutputTokens: 7,
			Allowance: &ai.Allowance{Unit: "tokens", Used: 100, Limit: 10000},
		}})
		w.(http.Flusher).Flush()
	}))
	defer srv.Close()

	p := Pipeline{
		LLM:     cloud.New(cloud.Config{BaseURL: srv.URL + "/v0/", Product: "sneat", Token: func(context.Context) (string, error) { return "tok", nil }}),
		Product: "sneat",
	}
	req, _ := p.StreamRequest(context.Background(), "show my calendar today", nil, "sp1", nil, nil, nil)
	seq, splitter := p.Stream(context.Background(), req)
	text, events := drain(t, seq)
	if text != "You have 5 things." {
		t.Fatalf("text = %q", text)
	}
	if gotProduct != "sneat" {
		t.Fatalf("X-AI-Product = %q, want sneat", gotProduct)
	}
	var usage *ai.Usage
	for _, ev := range events {
		if ev.Type == ai.EventCompleted {
			usage = ev.Usage
		}
	}
	if usage == nil || usage.Allowance == nil || usage.Allowance.Used != 100 || usage.Allowance.Limit != 10000 {
		t.Fatalf("usage = %+v, want allowance reported for a status line", usage)
	}
	if _, action, err := splitter.Finish(); err != nil || action != nil {
		t.Fatalf("splitter.Finish() = action=%+v err=%v, want no action", action, err)
	}
}

// TestStreamRequest_IncludesHistory covers S7: the last HistoryTurns
// exchanges are sent ahead of the current user message, oldest first.
func TestStreamRequest_IncludesHistory(t *testing.T) {
	p := Pipeline{Now: func() time.Time { return time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC) }}
	history := []ai.Message{
		{Role: ai.RoleUser, Text: "hi"},
		{Role: ai.RoleAssistant, Text: "hello"},
	}
	req, _ := p.StreamRequest(context.Background(), "what's next", nil, "sp1", nil, nil, history)
	if len(req.Messages) != 3 {
		t.Fatalf("Messages = %+v, want history + current turn", req.Messages)
	}
	// ai.Message is no longer comparable via == (v0.0.3 added a []ai.ToolCall
	// field for tool-calling support) -- compare the fields this test
	// actually cares about instead.
	if req.Messages[0].Role != history[0].Role || req.Messages[0].Text != history[0].Text ||
		req.Messages[1].Role != history[1].Role || req.Messages[1].Text != history[1].Text {
		t.Fatalf("Messages = %+v, want history first", req.Messages)
	}
	if req.Messages[2].Text != "what's next" || req.Messages[2].Role != ai.RoleUser {
		t.Fatalf("Messages[2] = %+v, want the current user turn", req.Messages[2])
	}
}

// TestStreamRequest_HistoryIsBoundedToHistoryTurns covers the same ruling's
// bound: StreamRequest itself caps history to HistoryTurns exchanges even if
// a caller hands it more.
func TestStreamRequest_HistoryIsBoundedToHistoryTurns(t *testing.T) {
	p := Pipeline{}
	var history []ai.Message
	for i := 0; i < HistoryTurns+5; i++ {
		history = append(history,
			ai.Message{Role: ai.RoleUser, Text: fmt.Sprintf("u%d", i)},
			ai.Message{Role: ai.RoleAssistant, Text: fmt.Sprintf("a%d", i)})
	}
	req, _ := p.StreamRequest(context.Background(), "now", nil, "sp1", nil, nil, history)
	// +1 for the current turn's own message.
	if want := HistoryTurns*2 + 1; len(req.Messages) != want {
		t.Fatalf("len(Messages) = %d, want %d (bounded to HistoryTurns)", len(req.Messages), want)
	}
	// The oldest exchanges are dropped, not the newest.
	if req.Messages[0].Text != "u5" {
		t.Fatalf("Messages[0] = %+v, want the oldest RETAINED exchange (u5)", req.Messages[0])
	}
}

// TestStreamRequest_SessionBlockCarriesFocusSelectionSidebarTitles covers
// S7: the "now"/TZ line plus focused/selection/sidebar entity titles are
// sent as a dynamic context block, so the LLM sees what the user is looking
// at without Sneat re-deriving it from Keys alone.
func TestStreamRequest_SessionBlockCarriesFocusSelectionSidebarTitles(t *testing.T) {
	p := Pipeline{Now: func() time.Time { return time.Date(2026, 9, 24, 15, 30, 0, 0, time.UTC) }, TZ: "Europe/Paris"}
	focused := session.EntityRef{Type: "happening", Title: "Dentist"}
	st := &session.State{
		Focused:   &focused,
		Selection: []session.EntityRef{{Type: "todo", Title: "Buy milk"}},
		Sidebar:   []session.EntityRef{{Type: "contact", Title: "Alice"}},
	}
	req, _ := p.StreamRequest(context.Background(), "hi", st, "sp1", nil, nil, nil)
	block := findBlock(t, req.Context, "session")
	for _, want := range []string{"Europe/Paris", "Dentist", "Buy milk", "Alice"} {
		if !strings.Contains(block.Text, want) {
			t.Fatalf("session block = %q, want it to contain %q", block.Text, want)
		}
	}
}

// TestStreamRequest_SessionBlockExcludesOtherSpaceSidebarPin is m3 (fix
// round r4 review): a sidebar pin tagged with a DIFFERENT space's ID must
// never be named to the LLM as something pinned in THIS session -- Resolver
// already refuses to resolve a pronoun against it (see
// TestResolver_PronounExcludesOtherSpaceSidebarPin), and the LLM context
// must agree, or the model could reference a title that isn't actually
// resolvable in the space this turn is running against.
func TestStreamRequest_SessionBlockExcludesOtherSpaceSidebarPin(t *testing.T) {
	p := Pipeline{Now: func() time.Time { return time.Date(2026, 9, 24, 15, 30, 0, 0, time.UTC) }, TZ: "Europe/Paris"}
	st := &session.State{
		Sidebar: []session.EntityRef{
			{Type: "contact", Title: "Alice", Keys: map[string]string{"spaceID": "sp1"}},
			{Type: "contact", Title: "Bob (other space)", Keys: map[string]string{"spaceID": "spOLD"}},
		},
	}
	req, _ := p.StreamRequest(context.Background(), "hi", st, "sp1", nil, nil, nil)
	block := findBlock(t, req.Context, "session")
	if !strings.Contains(block.Text, "Alice") {
		t.Fatalf("session block = %q, want the CURRENT-space pin (Alice)", block.Text)
	}
	if strings.Contains(block.Text, "Bob") {
		t.Fatalf("session block = %q, must not name the OTHER-space pin (Bob)", block.Text)
	}
}

// TestStreamRequest_ContactsCountOnlyUnlessMentionedOrRequired covers S7's
// contacts cap: a turn that neither requires the contacts scope nor
// plausibly mentions a person gets a count, not the full name list; naming
// someone (a capitalized word past the first) includes the full list.
func TestStreamRequest_ContactsCountOnlyUnlessMentionedOrRequired(t *testing.T) {
	readers := contactsReaders(t)
	p := Pipeline{Readers: readers}

	req, _ := p.StreamRequest(context.Background(), "what should I do today", nil, "sp1", nil, nil, nil)
	block := findBlock(t, req.Context, "contacts")
	if strings.Contains(block.Text, "Alice") {
		t.Fatalf("contacts block = %q, want a count only (no person mentioned)", block.Text)
	}
	if !strings.Contains(block.Text, "1 contact") {
		t.Fatalf("contacts block = %q, want a count", block.Text)
	}

	req, _ = p.StreamRequest(context.Background(), "can you remind Alice about the meeting", nil, "sp1", nil, nil, nil)
	block = findBlock(t, req.Context, "contacts")
	if !strings.Contains(block.Text, "Alice") {
		t.Fatalf("contacts block = %q, want the full list once a person is mentioned", block.Text)
	}
}

// TestStreamRequest_ContactsFullWhenRequiredByDecision covers the Select
// (decision-driven) path's equivalent rule: RequiredScopes naming contacts
// gets the full list even with no person mentioned in text.
func TestStreamRequest_ContactsFullWhenRequiredByDecision(t *testing.T) {
	readers := contactsReaders(t)
	p := Pipeline{Readers: readers}
	d := &decision.Decision{RequiredScopes: []string{sneatdomain.ModuleContacts}, NeedsLLM: true}
	req, _ := p.StreamRequest(context.Background(), "who do I know", nil, "sp1", d, nil, nil)
	block := findBlock(t, req.Context, "contacts")
	if !strings.Contains(block.Text, "Alice") {
		t.Fatalf("contacts block = %q, want the full list (scope explicitly required)", block.Text)
	}
}

// TestDynamicBlocks_CapsHappeningsAndTodos covers S7's item cap: a dynamic
// block never dumps more than maxDynamicItems records.
func TestDynamicBlocks_CapsHappeningsAndTodos(t *testing.T) {
	var items []data.Happening
	for i := 0; i < maxDynamicItems+10; i++ {
		items = append(items, data.Happening{SpaceID: "sp1", ID: fmt.Sprintf("h%d", i), Title: fmt.Sprintf("H%d", i),
			Start: time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)})
	}
	p := Pipeline{
		Now:     func() time.Time { return time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC) },
		Readers: data.Readers{Happenings: &data.FakeHappenings{Items: items}},
	}
	blocks := p.DynamicBlocks(context.Background(), "sp1", []string{sneatdomain.ModuleCalendar}, false)
	block := findBlock(t, blocks, "relevant_happenings")
	if got := strings.Count(block.Text, "- H"); got != maxDynamicItems {
		t.Fatalf("listed %d happenings, want the cap of %d", got, maxDynamicItems)
	}
}

// TestDynamicBlocks_TodoContextIncludesBuyList_NotDoneFirst covers m10: the
// LLM's todo context must include the buy list too (otherwise "get milk" has
// nothing to resolve against), sorted not-done first so a done item -- far
// less likely to be what a follow-up means -- doesn't crowd out live ones
// under the shared cap.
func TestDynamicBlocks_TodoContextIncludesBuyList_NotDoneFirst(t *testing.T) {
	p := Pipeline{
		Readers: data.Readers{Todos: &data.FakeTodos{Items: []data.Todo{
			{ID: "t1", SpaceID: "sp1", List: data.ListKindDo, Title: "Finish report", Done: true},
			{ID: "t2", SpaceID: "sp1", List: data.ListKindDo, Title: "Call dentist"},
			{ID: "b1", SpaceID: "sp1", List: data.ListKindBuy, Title: "Milk"},
			{ID: "b2", SpaceID: "sp1", List: data.ListKindBuy, Title: "Eggs", Done: true},
		}}},
	}
	blocks := p.DynamicBlocks(context.Background(), "sp1", []string{sneatdomain.ModuleTodo}, false)
	block := findBlock(t, blocks, "todos")
	if !strings.Contains(block.Text, "Milk") {
		t.Fatalf("todo context = %q, want the buy list included", block.Text)
	}
	callIdx := strings.Index(block.Text, "Call dentist")
	milkIdx := strings.Index(block.Text, "Milk")
	reportIdx := strings.Index(block.Text, "Finish report")
	eggsIdx := strings.Index(block.Text, "Eggs")
	if callIdx < 0 || milkIdx < 0 || reportIdx < 0 || eggsIdx < 0 {
		t.Fatalf("todo context missing an item: %q", block.Text)
	}
	if callIdx >= reportIdx || milkIdx >= reportIdx || callIdx >= eggsIdx || milkIdx >= eggsIdx {
		t.Fatalf("todo context = %q, want both not-done items before both done items", block.Text)
	}
}

// TestDynamicBlocks_Calendar_ProjectsRecurringOccurrence_NotTemplateDate is
// the calendar lane's own r3 handoff: DynamicBlocks must tell the LLM a
// recurring happening's real upcoming occurrence date, via
// happeningRowsInWindow (the same projection a calendar presentation uses),
// not HappeningsReader.Window's raw Start/End, which is the recurring
// happening's stored TEMPLATE date (2026-01-02 here) -- reporting that every
// day would make the LLM think a weekly Monday/Friday class is always "next
// Friday Jan 2".
func TestDynamicBlocks_Calendar_ProjectsRecurringOccurrence_NotTemplateDate(t *testing.T) {
	yoga, _ := monFriYoga(t)
	now := mondayNoon(t) // Monday 2026-09-21
	p := Pipeline{
		Now:     func() time.Time { return now },
		Readers: data.Readers{Happenings: &data.FakeHappenings{Items: []data.Happening{yoga}}},
	}
	blocks := p.DynamicBlocks(context.Background(), "sp1", []string{sneatdomain.ModuleCalendar}, false)
	block := findBlock(t, blocks, "relevant_happenings")
	if strings.Contains(block.Text, "2026-01-02") || strings.Contains(block.Text, "Jan 02") {
		t.Fatalf("relevant_happenings = %q, leaked the recurring happening's stored template date", block.Text)
	}
	if !strings.Contains(block.Text, "Mon 2026-09-21") {
		t.Fatalf("relevant_happenings = %q, want this week's real Monday occurrence (2026-09-21)", block.Text)
	}
}

func contactsReaders(t *testing.T) data.Readers {
	t.Helper()
	return data.Readers{Contacts: &data.FakeContacts{Items: []data.Contact{{ID: "c1", SpaceID: "sp1", Name: "Alice"}}}}
}

func findBlock(t *testing.T, blocks []ai.ContextBlock, name string) ai.ContextBlock {
	t.Helper()
	for _, b := range blocks {
		if b.Name == name {
			return b
		}
	}
	t.Fatalf("no %q context block among %+v", name, blocks)
	return ai.ContextBlock{}
}

// TestStreamRequest_SystemPromptTreatsSpaceContentAsData covers m10: the
// system prompt tells the model that calendar/todo/contacts context is data
// to answer from, never an instruction, regardless of what its text says
// (brief §17's mutation-safety boundary applied to prompt content too).
func TestStreamRequest_SystemPromptTreatsSpaceContentAsData(t *testing.T) {
	p := Pipeline{}
	req, _ := p.StreamRequest(context.Background(), "hi", nil, "sp1", nil, nil, nil)
	if !strings.Contains(req.System, "DATA") && !strings.Contains(req.System, "data") {
		t.Fatalf("System = %q, want it to state space content is data, not instructions", req.System)
	}
}

// TestSneatActionInstruction_ConfirmationWording is item 3 of the
// coordinator's PR #56 review round 2: the instruction must tell the model
// which actions require the user's "yes" confirmation before they happen
// (reschedule/cancel a happening, delete a todo -- exactly isDestructive's
// list in pipeline.go) and phrase those as a proposal, never as already
// done; and separately confirm which actions run immediately without
// confirmation (add_todo/add_to_buy/complete_todo/reopen_todo -- the
// pipeline.runAction path resolveAndAct takes for any non-destructive
// kind), where saying "done" is accurate.
func TestSneatActionInstruction_ConfirmationWording(t *testing.T) {
	text := sneatActionInstruction
	if !strings.Contains(text, "confirmation") {
		t.Fatalf("System = %q, want it to mention confirmation is required for some actions", text)
	}
	if !strings.Contains(text, `never "Done, I cancelled it."`) {
		t.Fatalf("System = %q, want an explicit example of the WRONG (already-done) phrasing to avoid for a destructive action", text)
	}
	if !strings.Contains(text, "run immediately") {
		t.Fatalf("System = %q, want it to state that adding/completing/reopening run immediately (no confirmation)", text)
	}
	// The three isDestructive kinds must each be named in the same sentence
	// as "confirmation" -- not merely present anywhere in the instruction
	// (they are also named earlier, in the per-kind list).
	confirmSentence := text[strings.Index(text, "Rescheduling"):strings.Index(text, "Adding an item")]
	for _, kind := range []string{"reschedul", "cancel", "delet"} {
		if !strings.Contains(strings.ToLower(confirmSentence), kind) {
			t.Errorf("confirmation sentence = %q, want it to cover %q (isDestructive's list)", confirmSentence, kind)
		}
	}
}

// fakeErroringLLM streams one delta ending mid-tag (held back by the
// Splitter as a possible tag start) then a fatal stream error, never an
// EventCompleted -- the shape splitStream's error branch must handle.
type fakeErroringLLM struct{}

func (fakeErroringLLM) Name() string { return "fake-erroring" }

func (fakeErroringLLM) Stream(context.Context, ai.ChatRequest) iter.Seq2[ai.Event, error] {
	return func(yield func(ai.Event, error) bool) {
		if !yield(ai.Event{Type: ai.EventTextDelta, Text: "Hello <sneat-a"}, nil) {
			return
		}
		yield(ai.Event{}, errors.New("boom"))
	}
}

// TestStream_ErrorFlushesHeldBackText covers S6/llm.go's splitStream error
// branch: a fatal stream error must not silently drop whatever safe text
// the Splitter was still holding back (bytes that could have been the
// start of a tag, here "<sneat-a") -- it must be flushed before the error.
func TestStream_ErrorFlushesHeldBackText(t *testing.T) {
	p := Pipeline{LLM: fakeErroringLLM{}}
	seq, _ := p.Stream(context.Background(), ai.ChatRequest{})
	text, _, _, err := ai.Collect(seq)
	if err == nil {
		t.Fatal("expected the stream's fatal error to propagate")
	}
	if text != "Hello <sneat-a" {
		t.Fatalf("text = %q, want the held-back partial-tag bytes flushed before the error", text)
	}
}

func TestStream_NoLLMConfiguredIsAnError(t *testing.T) {
	p := Pipeline{}
	seq, _ := p.Stream(context.Background(), ai.ChatRequest{})
	_, _, _, err := ai.Collect(seq)
	if err == nil {
		t.Fatal("expected an error when no LLM provider is configured")
	}
}
