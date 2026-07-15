// Package chat defines the seam between a chat renderer and whatever processes
// a chat turn. A renderer depends on the Processor interface only, so the
// in-process implementation can be swapped for a server-backed one without
// touching the renderer: both speak the same framework-compatible vocabulary
// for buttons and callback data, and both hand failures back rather than
// rendering them.
package chat

import (
	"context"

	"github.com/bots-go-framework/bots-go-core/botkb"
)

// Reply is one message from the bot: its text and an optional keyboard.
//
// Keyboard is nil when the reply carries no buttons. It is a botkb.Keyboard —
// the bots-go-framework vocabulary, built as rows of buttons matching
// Telegram's inline_keyboard shape — rather than a locally-defined button type,
// so translating a Reply into a botmsg.MessageFromBot later is a field copy
// rather than a mapping layer.
type Reply struct {
	// Text is the bot's message text.
	Text string

	// Keyboard is the buttons to show with the message, or nil for none.
	Keyboard botkb.Keyboard
}

// Processor processes a chat turn and returns the bot's replies.
//
// The two methods mirror the framework's own split between
// botinput.TextMessage/TextAction and botinput.CallbackQuery/CallbackAction:
// SendText handles a typed message, PressButton handles a button press
// identified by its callback data.
//
// Both return []Reply rather than a single Reply because one turn may produce
// more than one message — a confirmation flow is a reply plus a prompt — and a
// renderer commits them in order.
//
// Failures come back as an error. An implementation must not format
// user-facing error text: presentation belongs to the renderer, the only layer
// that knows how errors should look on its surface.
type Processor interface {
	// SendText processes a text message typed by the user.
	SendText(ctx context.Context, text string) ([]Reply, error)

	// PressButton processes a button press, identified by its callback data.
	PressButton(ctx context.Context, data string) ([]Reply, error)
}
