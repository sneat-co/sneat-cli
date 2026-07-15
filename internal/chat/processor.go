package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// SpacesReader lists the signed-in user's spaces.
//
// It is declared here rather than imported so this package stays a leaf, the
// same way internal/tui declares its own. That is not only decoupling: the
// other declaration of this shape lives in cmd/sneat/commands, which imports
// convoruntime, so importing it would put the sandbox runtime one hop away
// from a real-data conversation — exactly what free-text deferral avoids.
type SpacesReader interface {
	ListSpaces(ctx context.Context, uid string) (map[string]any, error)
}

// Command names, as the user types them.
const (
	cmdSpaces = "/spaces"
	cmdHelp   = "/help"
)

// command is one slash command: how it is typed, how /help describes it, and
// what runs when text routes to it.
type command struct {
	name    string
	summary string
	handle  func(ctx context.Context) ([]Reply, error)
}

// processor answers a chat turn in this process, against the signed-in user's
// real spaces — no server and no conversational runtime in between.
//
// The type is unexported on purpose, and NewProcessor hands back the Processor
// interface rather than this type. A renderer imports this package for the
// interface and has no implementation to name even if it wanted one, so
// REQ: processor-seam's "MUST NOT reference any concrete implementation" is
// enforced by the compiler rather than by discipline.
type processor struct {
	spaces SpacesReader
	uid    string

	// activeSpace is the session's selected space ID: set by pressing a space
	// button, read by later space-scoped commands. Empty until the user picks
	// one.
	activeSpace string

	// commands is the routing table keyed by command name; order lists the
	// same commands in the sequence /help prints them, which ranging over the
	// map would randomize.
	commands map[string]command
	order    []string
}

// NewProcessor returns a Processor that handles chat turns in this process,
// listing the spaces uid can see through the given reader.
//
// It returns the interface, not the concrete type, so no caller — the chat
// renderer's composition root above all — can name an implementation.
func NewProcessor(spaces SpacesReader, uid string) Processor {
	p := &processor{
		spaces:   spaces,
		uid:      uid,
		commands: map[string]command{},
	}
	// Registration order is the order /help prints.
	p.register(command{name: cmdSpaces, summary: "list your spaces", handle: p.spacesCmd})
	p.register(command{name: cmdHelp, summary: "show this message", handle: p.helpCmd})
	return p
}

// register adds a command to the routing table and to /help's listing.
func (p *processor) register(c command) {
	p.commands[c.name] = c
	p.order = append(p.order, c.name)
}

// SendText routes a typed message. Text starting with "/" goes to the command
// named by its first word, and an unrecognized one is named back to the user
// rather than dropped. Anything else is free text, which this processor
// answers itself.
func (p *processor) SendText(ctx context.Context, text string) ([]Reply, error) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return p.freeText(), nil
	}
	name := commandName(text)
	cmd, ok := p.commands[name]
	if !ok {
		return p.unknownCommand(name), nil
	}
	return cmd.handle(ctx)
}

// PressButton processes a button press, identified by its callback data.
//
// TODO(chat-messenger Task 5): implement REQ: active-space-selection and
// REQ: unrecognized-callback-data — dispatch on the parsed callback path, set
// activeSpace for `space?id=<id>` and name the newly active space, return an
// error without changing it when the id names no space the user can see, and
// answer unhandleable data with a reply or an error. Until then a press fails
// loudly: never a silent no-op, and never a fabricated confirmation.
func (p *processor) PressButton(context.Context, string) ([]Reply, error) {
	return nil, errors.New("button presses are not implemented yet")
}

// commandName returns the first word of a slash-command line: "/spaces" from
// both "/spaces" and "/spaces extra". No command served here takes arguments,
// so the rest of the line is not read.
func commandName(text string) string {
	if i := strings.IndexFunc(text, unicode.IsSpace); i >= 0 {
		return text[:i]
	}
	return text
}

// helpCmd lists the commands this processor serves (REQ: help-command).
func (p *processor) helpCmd(context.Context) ([]Reply, error) {
	var b strings.Builder
	b.WriteString("Commands:")
	for _, name := range p.order {
		_, _ = fmt.Fprintf(&b, "\n%s — %s", name, p.commands[name].summary)
	}
	return []Reply{{Text: b.String()}}, nil
}

// spacesCmd lists the signed-in user's spaces.
//
// TODO(chat-messenger Task 4): implement REQ: spaces-command — one inline
// button per space, one per row, ordered by space ID, labelled with the title
// and falling back to the ID, each carrying `space?id=<spaceID>`; a user with
// no spaces gets a reply saying so and no keyboard at all. It is registered
// now so /help can name it and so routing has somewhere to land. Until it is
// written it fails rather than answers: a listing the reader never produced
// would be precisely the fixture-shaped lie this Feature exists to prevent.
func (p *processor) spacesCmd(context.Context) ([]Reply, error) {
	return nil, errors.New("/spaces is not implemented yet")
}

// freeText answers a message that is not a command.
//
// The answer stops here rather than travelling on to convoruntime: that
// runtime is wired only to the sandbox — a mock LLM over a fake space and user
// — so putting a real-data session through it would mix fixture actions with
// real space listings in one transcript, and the user could not tell which
// reply was which (REQ: free-text-deferred).
func (p *processor) freeText() []Reply {
	return []Reply{{Text: fmt.Sprintf(
		"Free-text chat is not yet available. Commands: %s.", p.commandNames())}}
}

// unknownCommand answers a slash command with no handler, naming what was
// typed and pointing at /help — never a silent no-op (REQ: slash-command-routing).
func (p *processor) unknownCommand(name string) []Reply {
	return []Reply{{Text: fmt.Sprintf(
		"Unknown command %s. Try %s for the list of commands.", name, cmdHelp)}}
}

// commandNames joins the served command names for a one-line prose list.
func (p *processor) commandNames() string {
	return strings.Join(p.order, ", ")
}
