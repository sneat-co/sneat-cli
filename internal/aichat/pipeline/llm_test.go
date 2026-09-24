package pipeline

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/strongo/aichat/ai"
	"github.com/strongo/aichat/ai/cloud"
	"github.com/strongo/aichat/ai/cloudproto"
	"github.com/strongo/aichat/ai/openaicompat"
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
	req, _ := p.StreamRequest(context.Background(), "what's on my plate", nil, "sp1", nil, nil)
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
	req, _ := p.StreamRequest(context.Background(), "show my calendar today", nil, "sp1", nil, nil)
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

func TestStream_NoLLMConfiguredIsAnError(t *testing.T) {
	p := Pipeline{}
	seq, _ := p.Stream(context.Background(), ai.ChatRequest{})
	_, _, _, err := ai.Collect(seq)
	if err == nil {
		t.Fatal("expected an error when no LLM provider is configured")
	}
}
