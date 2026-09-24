package pipeline

import (
	"context"
	"fmt"
	"io"
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
	if req.Messages[0] != history[0] || req.Messages[1] != history[1] {
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

func TestStream_NoLLMConfiguredIsAnError(t *testing.T) {
	p := Pipeline{}
	seq, _ := p.Stream(context.Background(), ai.ChatRequest{})
	_, _, _, err := ai.Collect(seq)
	if err == nil {
		t.Fatal("expected an error when no LLM provider is configured")
	}
}
