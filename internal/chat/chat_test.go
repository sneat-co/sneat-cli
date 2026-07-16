package chat

import (
	"context"
	"errors"
	"testing"

	"github.com/bots-go-framework/bots-go-core/botkb"
)

// --- fakes ---

// fakeProcessor is the smallest thing that satisfies Processor. It stands in
// for the concrete processors a renderer must never name.
type fakeProcessor struct {
	replies []Reply
	err     error
}

func (f fakeProcessor) SendText(context.Context, string) ([]Reply, error) {
	return f.replies, f.err
}

func (f fakeProcessor) PressButton(context.Context, string) ([]Reply, error) {
	return f.replies, f.err
}

func (f fakeProcessor) Commands() []CommandInfo { return nil }

// The seam is satisfied structurally: any type with the two methods is a
// Processor, so a renderer can depend on the interface alone.
var _ Processor = fakeProcessor{}

// --- tests ---

func TestReply_KeyboardIsOptional(t *testing.T) {
	// A reply with no buttons leaves the keyboard nil.
	plain := Reply{Text: "hello"}
	if plain.Keyboard != nil {
		t.Errorf("zero Reply keyboard = %v, want nil", plain.Keyboard)
	}

	// A reply with buttons carries a botkb.Keyboard, built as rows of buttons.
	kb := botkb.NewMessageKeyboard(botkb.KeyboardTypeInline,
		[]botkb.Button{botkb.NewDataButton("Yes", "yes"), botkb.NewDataButton("Cancel", "cancel")},
	)
	withKb := Reply{Text: "Delete it?", Keyboard: kb}
	if withKb.Keyboard == nil {
		t.Fatal("Reply.Keyboard should hold the assigned keyboard")
	}
	if got := withKb.Keyboard.KeyboardType(); got != botkb.KeyboardTypeInline {
		t.Errorf("keyboard type = %v, want KeyboardTypeInline", got)
	}
}

func TestProcessor_ErrorsAreReturnedNotFormatted(t *testing.T) {
	boom := errors.New("boom")
	var p Processor = fakeProcessor{err: boom}

	for _, tt := range []struct {
		name string
		call func() ([]Reply, error)
	}{
		{"SendText", func() ([]Reply, error) { return p.SendText(context.Background(), "/spaces") }},
		{"PressButton", func() ([]Reply, error) { return p.PressButton(context.Background(), "data") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			replies, err := tt.call()
			// The failure travels as an error, unwrapped to the original.
			if !errors.Is(err, boom) {
				t.Errorf("err = %v, want boom", err)
			}
			// It must not be rendered into user-facing reply prose — that is
			// the renderer's job, not the processor's.
			if len(replies) != 0 {
				t.Errorf("a failure must not produce replies, got %v", replies)
			}
		})
	}
}

// A chat turn may produce more than one message, so the seam returns a slice:
// a confirmation flow is a reply plus a prompt, committed in order.
func TestProcessor_ReturnsMultipleRepliesInOrder(t *testing.T) {
	var p Processor = fakeProcessor{replies: []Reply{
		{Text: "Deleted."},
		{Text: "Anything else?", Keyboard: botkb.NewMessageKeyboard(botkb.KeyboardTypeInline,
			[]botkb.Button{botkb.NewDataButton("Spaces", "spaces")},
		)},
	}}

	replies, err := p.SendText(context.Background(), "yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(replies) != 2 {
		t.Fatalf("replies = %d, want 2", len(replies))
	}
	if replies[0].Text != "Deleted." || replies[1].Text != "Anything else?" {
		t.Errorf("replies out of order: %q, %q", replies[0].Text, replies[1].Text)
	}
	if replies[0].Keyboard != nil {
		t.Error("first reply should carry no keyboard")
	}
	if replies[1].Keyboard == nil {
		t.Error("second reply should carry the prompt keyboard")
	}
}
