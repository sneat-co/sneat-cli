package chat

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"unicode"

	"github.com/bots-go-framework/bots-go-core/botkb"
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

// Callback vocabulary: the command a button's callback data names, and the
// argument it carries. Buttons are encoded with these and presses dispatch on
// them, so the two sides cannot drift into `space?id=` vs `space?spaceID=`.
const (
	// cbSpace selects a space: `space?id=<spaceID>`.
	cbSpace = "space"

	// cbArgSpaceID names the space cbSpace acts on.
	cbArgSpaceID = "id"
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

// PressButton processes a button press, identified by its callback data. It
// dispatches on the command the data names, the same way bots-fw's router does
// (REQ: callback-data-url).
//
// Data this processor cannot dispatch — it does not parse, names a command
// nobody registered, or omits an argument that command requires — is answered
// with a reply rather than an error (REQ: unrecognized-callback-data permits
// either). Two reasons to prefer the reply. The REQ frames this as the
// press-side counterpart of REQ: slash-command-routing's unknown typed command,
// which unknownCommand already answers with a reply naming a way forward; the
// same event arriving pressed rather than typed should not change category.
// And the reply is what a renderer can present usefully: chat-tui renders a
// returned error as a bot message and continues
// (chat-tui#req:errors-render-in-transcript), so on that surface the two look
// alike — but a web renderer is free to surface an error as failure chrome,
// where "this button is out of date, here is what to do" would read as the
// session breaking rather than as the bot answering. Errors stay reserved for
// what genuinely failed: the reader, or the lookup below.
func (p *processor) PressButton(ctx context.Context, data string) ([]Reply, error) {
	cb, err := parseCallbackData(data)
	if err != nil {
		return p.unhandledPress(), nil
	}
	switch cb.command {
	case cbSpace:
		// The bool, not the value: an id passed empty is not an id omitted, so
		// it goes on to selectSpace and fails there on the lookup, which is the
		// stale-button case rather than a structural one. That is the whole
		// reason callbackData.arg reports presence separately from value.
		id, ok := cb.arg(cbArgSpaceID)
		if !ok {
			return p.unhandledPress(), nil
		}
		return p.selectSpace(ctx, id)
	default:
		return p.unhandledPress(), nil
	}
}

// selectSpace makes id the session's active space and says so
// (REQ: active-space-selection).
//
// The reader is consulted rather than trusted: a button's id is only as fresh
// as the listing that emitted it, and a space can be revoked mid-session. An id
// naming no space the user can currently see leaves activeSpace alone and comes
// back as an error — the failure is real, and this package cannot know how the
// surface shows one (REQ: errors-are-returned-not-formatted).
func (p *processor) selectSpace(ctx context.Context, id string) ([]Reply, error) {
	spaces, err := p.spaces.ListSpaces(ctx, p.uid)
	if err != nil {
		return nil, fmt.Errorf("failed to list spaces: %w", err)
	}
	brief, ok := spaces[id]
	if !ok {
		return nil, fmt.Errorf("space %q is not among the spaces %q can see", id, p.uid)
	}
	// Assigned only past the lookup: an unknown id must leave the previously
	// active space standing.
	p.activeSpace = id
	// Named by label, through the same resolution the button's own label took,
	// so the confirmation echoes what the user pressed rather than the ID the
	// callback data happened to carry.
	return []Reply{{Text: fmt.Sprintf("Active space is now %s.", spaceLabel(brief, id))}}, nil
}

// unhandledPress answers callback data this processor cannot dispatch — never a
// silent no-op (REQ: unrecognized-callback-data).
//
// It does not echo the data back. A user never typed it, so quoting `%zz` at
// them names nothing they chose; what they can act on is that the button is
// stale and that /help lists what still works.
func (p *processor) unhandledPress() []Reply {
	return []Reply{{Text: fmt.Sprintf(
		"That action could not be handled — the button may be out of date. Try %s for the list of commands.",
		cmdHelp)}}
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

// spacesCmd lists the signed-in user's spaces, one pressable button per space
// (REQ: spaces-command).
//
// A reader failure comes back as an error rather than a reply saying so: this
// package cannot know how the surface renders a failure, and a listing the
// reader never produced would be the fixture-shaped lie this Feature exists to
// prevent (REQ: errors-are-returned-not-formatted).
func (p *processor) spacesCmd(ctx context.Context) ([]Reply, error) {
	spaces, err := p.spaces.ListSpaces(ctx, p.uid)
	if err != nil {
		return nil, fmt.Errorf("failed to list spaces: %w", err)
	}
	if len(spaces) == 0 {
		// No keyboard at all rather than an empty one: a renderer branches on
		// keyboard presence to decide whether a reply is focusable, so an
		// empty-but-present keyboard would give it a focus block with nothing
		// to focus.
		return []Reply{{Text: "You have no spaces."}}, nil
	}

	// Sorted, not ranged: ListSpaces returns a map, whose iteration order Go
	// randomizes, so ranging it directly would reshuffle the user's buttons on
	// every /spaces. internal/tui's spaceItemsFrom sorts for the same reason.
	ids := orderSpaces(spaces)

	rows := make([][]botkb.Button, 0, len(ids))
	for _, id := range ids {
		// Built through the encoder, which verifies the data parses back as
		// this command under the router's own contract, rather than by pasting
		// the string together here.
		data, err := encodeCallbackData(cbSpace, url.Values{cbArgSpaceID: {id}})
		if err != nil {
			return nil, fmt.Errorf("failed to build the button for space %q: %w", id, err)
		}
		// One button per row: the spaces stack vertically.
		rows = append(rows, []botkb.Button{botkb.NewDataButton(spaceLabel(spaces[id], id), data)})
	}
	return []Reply{{
		Text:     fmt.Sprintf("You have %d %s:", len(ids), plural(len(ids), "space", "spaces")),
		Keyboard: botkb.NewMessageKeyboard(botkb.KeyboardTypeInline, rows...),
	}}, nil
}

// spaceLabel returns a space's button label: its title; failing that, the
// capitalized space type with the ID in parentheses — "Family (vaoyj)"; failing
// that, the bare ID.
//
// The brief arrives as an untyped value out of a map[string]any, so "no title"
// has three shapes — the key absent, its value not a string, or the string
// empty — and a brief that is not a map at all is a fourth. Each type assertion
// below takes its comma-ok form and lands on the same fallback, so none of them
// can panic a listing into a failed command. Indexing a nil map is defined, so
// a non-map brief needs no separate branch.
func spaceLabel(brief any, id string) string {
	b, _ := brief.(map[string]any)
	if title, _ := b["title"].(string); title != "" {
		return title
	}
	// An empty title is the common case, not an edge: Sneat creates a user's
	// built-in spaces without one, so a real signed-in user's buttons are all
	// fallbacks and a bare ID would tell them nothing. The ID stays alongside
	// the type because built-in spaces share a type — two "family" spaces would
	// otherwise render identically, and telling them apart is the button's job.
	if spaceType, _ := b["type"].(string); spaceType != "" {
		return fmt.Sprintf("%s (%s)", capitalize(spaceType), id)
	}
	return id
}

// Space types Sneat creates for every user. They are the two spaces every user
// has, and the ones a plain alphabetical sort would scatter among custom
// spaces, so they are pinned to the end instead — family last of all
// (REQ: spaces-command).
const (
	spaceTypePrivate = "private"
	spaceTypeFamily  = "family"
)

// spaceRank orders the classes of space. Lower sorts earlier, so family — the
// space a user opens most — lands last, in the seat nearest the renderer's
// entry point and so the cheapest to reach.
func spaceRank(brief any) int {
	b, _ := brief.(map[string]any)
	switch spaceType, _ := b["type"].(string); strings.ToLower(spaceType) {
	case spaceTypeFamily:
		return 2
	case spaceTypePrivate:
		return 1
	default:
		return 0
	}
}

// orderSpaces returns the space IDs in the order their buttons render:
// custom spaces alphabetically by label, then private, then family.
//
// Sorting at all is what stops the buttons reshuffling — ListSpaces returns a
// map and Go randomizes its iteration. The ID tiebreak is what makes the order
// total: two spaces can share a label, and without a further key their relative
// order would ride on that same iteration.
func orderSpaces(spaces map[string]any) []string {
	ids := slices.Collect(maps.Keys(spaces))
	slices.SortFunc(ids, func(a, b string) int {
		if c := cmp.Compare(spaceRank(spaces[a]), spaceRank(spaces[b])); c != 0 {
			return c
		}
		if c := cmp.Compare(spaceLabel(spaces[a], a), spaceLabel(spaces[b], b)); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
	return ids
}

// capitalize upper-cases the first rune of s, leaving the rest as-is: the space
// type "family" becomes "Family". It is rune-aware rather than byte-indexed so
// a non-ASCII type does not get sliced mid-rune.
func capitalize(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(unicode.ToUpper(r[0])) + string(r[1:])
}

// plural picks the singular or plural form for n.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
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
