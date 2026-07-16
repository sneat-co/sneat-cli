package chat

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/bots-go-framework/bots-go-core/botkb"
)

// --- fakes ---

// fakeSpaces stands in for the Firestore-backed spaces reader, following the
// fixture pattern of internal/tui/tui_test.go.
type fakeSpaces struct {
	spaces map[string]any
	err    error
}

func (f fakeSpaces) ListSpaces(context.Context, string) (map[string]any, error) {
	return f.spaces, f.err
}

var _ SpacesReader = fakeSpaces{}

// twoSpaces is the fixture the scenarios use: "Family" and "Personal".
func twoSpaces() map[string]any {
	return map[string]any{
		"family1":   map[string]any{"title": "Family", "type": "family", "status": "active"},
		"personal1": map[string]any{"title": "Personal", "type": "private", "status": "active"},
	}
}

// newTestProcessor builds a processor over the given spaces.
func newTestProcessor(spaces map[string]any) Processor {
	return NewProcessor(Deps{Spaces: fakeSpaces{spaces: spaces}, UID: "u1"})
}

// fakeContacts returns canned contacts per space ID.
type fakeContacts struct {
	bySpace map[string][]Contact
	err     error
}

func (f fakeContacts) ListContacts(_ context.Context, spaceID string) ([]Contact, error) {
	return f.bySpace[spaceID], f.err
}

// spaceButtons returns a /spaces reply's buttons, one per row, asserting the
// keyboard vocabulary REQ: botkb-vocabulary requires along the way: a
// botkb.Keyboard of the inline type, holding botkb.DataButton values arranged
// in rows of []botkb.Button.
func spaceButtons(t *testing.T, reply Reply) []*botkb.DataButton {
	t.Helper()
	if reply.Keyboard == nil {
		t.Fatalf("/spaces reply %q carries no keyboard", reply.Text)
	}
	if got := reply.Keyboard.KeyboardType(); got != botkb.KeyboardTypeInline {
		t.Errorf("keyboard type = %v, want botkb.KeyboardTypeInline", got)
	}
	kb, ok := reply.Keyboard.(*botkb.MessageKeyboard)
	if !ok {
		t.Fatalf("keyboard is %T, want *botkb.MessageKeyboard", reply.Keyboard)
	}
	// Buttons is botkb's own [][]botkb.Button — rows of buttons, matching
	// Telegram's inline_keyboard shape. Each row's single element is asserted
	// to be a *botkb.DataButton below, which is the half this package owns.
	rows := kb.Buttons
	buttons := make([]*botkb.DataButton, 0, len(rows))
	for i, row := range rows {
		// One button per row: REQ: spaces-command stacks the spaces vertically.
		if len(row) != 1 {
			t.Fatalf("row %d holds %d buttons, want exactly 1", i, len(row))
		}
		b, ok := row[0].(*botkb.DataButton)
		if !ok {
			t.Fatalf("row %d button is %T, want *botkb.DataButton", i, row[0])
		}
		if got := b.ButtonType(); got != botkb.ButtonTypeData {
			t.Errorf("row %d button type = %v, want botkb.ButtonTypeData", i, got)
		}
		buttons = append(buttons, b)
	}
	return buttons
}

// buttonLabels reads the label off each button, in keyboard order.
func buttonLabels(buttons []*botkb.DataButton) []string {
	labels := make([]string, 0, len(buttons))
	for _, b := range buttons {
		labels = append(labels, b.GetText())
	}
	return labels
}

// send runs one text turn and returns the single reply it must produce. Every
// input a user can type is answered, so an error here is itself a failure.
func send(t *testing.T, p Processor, text string) Reply {
	t.Helper()
	replies, err := p.SendText(context.Background(), text)
	if err != nil {
		t.Fatalf("SendText(%q): unexpected error: %v", text, err)
	}
	if len(replies) != 1 {
		t.Fatalf("SendText(%q): replies = %d, want 1: %v", text, len(replies), replies)
	}
	return replies[0]
}

// --- tests ---

// The concrete processor is unexported, so only the interface leaves the
// package: a renderer importing chat has no implementation to name.
var _ Processor = (*processor)(nil)

func TestNewProcessor_StartsWithNoActiveSpace(t *testing.T) {
	p := newTestProcessor(twoSpaces()).(*processor)
	if p.activeSpace != "" {
		t.Errorf("activeSpace = %q, want empty until the user selects one", p.activeSpace)
	}
}

func TestProcessor_HelpNamesTheCommands(t *testing.T) {
	p := newTestProcessor(twoSpaces())
	reply := send(t, p, "/help")
	for _, want := range []string{"/spaces", "/help"} {
		if !strings.Contains(reply.Text, want) {
			t.Errorf("/help reply %q does not name %s", reply.Text, want)
		}
	}
}

// TestProcessor_SpacesListsRealSpacesAsButtons is the scenario from
// _tests/slash-commands-act-on-real-spaces.md: /spaces lists what the reader
// returned, one titled button per row, in the requirement's order.
func TestProcessor_SpacesListsRealSpacesAsButtons(t *testing.T) {
	p := newTestProcessor(twoSpaces())
	reply := send(t, p, "/spaces")

	// The reply text states that the user has 2 spaces.
	if !strings.Contains(reply.Text, "2 spaces") {
		t.Errorf("reply %q does not state that the user has 2 spaces", reply.Text)
	}

	// The keyboard has exactly 2 rows, each holding one button, labelled with
	// the space titles. The fixture's spaces are a private and a family one, so
	// the order is the requirement's rather than the IDs': family is ranked last,
	// nearest the renderer's entry point (REQ: spaces-command).
	buttons := spaceButtons(t, reply)
	want := []string{"Personal", "Family"}
	if got := buttonLabels(buttons); !slices.Equal(got, want) {
		t.Errorf("button labels = %v, want %v", got, want)
	}
}

// TestProcessor_SpacesButtonsCarryURLCallbackData is the scenario from
// _tests/buttons-use-botkb-and-url-callback-data.md. The botkb vocabulary
// itself — inline keyboard, DataButton values in rows — is asserted by
// spaceButtons; what is left is the callback data each button carries.
func TestProcessor_SpacesButtonsCarryURLCallbackData(t *testing.T) {
	p := newTestProcessor(map[string]any{
		"family1": map[string]any{"title": "Family"},
	})
	reply := send(t, p, "/spaces")

	buttons := spaceButtons(t, reply)
	if len(buttons) != 1 {
		t.Fatalf("buttons = %d, want 1", len(buttons))
	}
	if got, want := buttons[0].Data, "space?id=family1"; got != want {
		t.Errorf("button data = %q, want %q", got, want)
	}
}

// TestProcessor_SpaceButtonLabelFallsBackToID covers the scenario's empty-title
// space, plus the two other shapes a brief read out of a map[string]any can
// take: a title key that is absent, and one that is not a string. All three are
// "no title", so all three fall back to the ID.
func TestProcessor_SpaceButtonLabelFallsBackToID(t *testing.T) {
	for _, tt := range []struct {
		name  string
		brief any
		want  string
	}{
		{"title present", map[string]any{"title": "Solo"}, "Solo"},
		{"title empty falls back to Type (id)", map[string]any{"title": "", "type": "private"}, "Private (solo1)"},
		{"title absent falls back to Type (id)", map[string]any{"type": "family"}, "Family (solo1)"},
		{"title not a string falls back to Type (id)", map[string]any{"title": 42, "type": "private"}, "Private (solo1)"},
		{"type already capitalized is left alone", map[string]any{"type": "Family"}, "Family (solo1)"},
		{"type not a string falls back to the bare id", map[string]any{"type": 42}, "solo1"},
		{"title and type both empty falls back to the bare id", map[string]any{"title": "", "type": ""}, "solo1"},
		{"brief not a map", "not-a-brief", "solo1"},
		{"brief nil", nil, "solo1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestProcessor(map[string]any{"solo1": tt.brief})
			reply := send(t, p, "/spaces")

			buttons := spaceButtons(t, reply)
			if len(buttons) != 1 {
				t.Fatalf("buttons = %d, want 1", len(buttons))
			}
			if got := buttons[0].GetText(); got != tt.want {
				t.Errorf("button label = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestProcessor_SpaceButtonsAreOrderedByID guards against ranging over
// ListSpaces's map[string]any directly. Go randomizes map iteration, so an
// unordered implementation reshuffles the user's buttons on every invocation
// and passes only intermittently.
//
// Two fixture spaces would not catch it: a handful of entries can come back in
// sorted order by luck often enough for the guard to be worthless. Ten spaces
// inserted out of order, listed on repeated passes, leaves an unsorted
// implementation no room to pass by chance.
func TestProcessor_SpaceButtonsAreOrdered(t *testing.T) {
	// Inserted out of order, and titled with nothing, so each button's label is
	// its own ID and the assertion reads the ordering directly. All are custom
	// types, so they rank together and sort alphabetically among themselves.
	spaces := map[string]any{}
	for _, id := range []string{"s07", "s02", "s10", "s04", "s09", "s01", "s06", "s03", "s08", "s05"} {
		spaces[id] = map[string]any{"title": ""}
	}
	want := []string{"s01", "s02", "s03", "s04", "s05", "s06", "s07", "s08", "s09", "s10"}

	// Many passes, and ten entries rather than two: Go randomizes map iteration
	// per range, so a two-entry map would agree with an unsorted implementation
	// half the time and the guard would pass by luck.
	const passes = 50
	for pass := range passes {
		p := newTestProcessor(spaces)
		reply := send(t, p, "/spaces")
		got := buttonLabels(spaceButtons(t, reply))
		if !slices.Equal(got, want) {
			t.Fatalf("pass %d: button order = %v, want %v", pass, got, want)
		}
	}
}

// TestProcessor_SpaceButtonsRankFamilyLast pins the order the requirement
// states: custom spaces alphabetically, then private, then family last — the
// seat nearest the renderer's entry point, and so the cheapest to reach
// (chat-messenger#req:spaces-command).
func TestProcessor_SpaceButtonsRankFamilyLast(t *testing.T) {
	spaces := map[string]any{
		"fam1":  map[string]any{"title": "", "type": "family"},
		"priv1": map[string]any{"title": "", "type": "private"},
		"z2":    map[string]any{"title": "Z Space 2", "type": "club"},
		"s1":    map[string]any{"title": "Space 1", "type": "company"},
	}
	want := []string{"Space 1", "Z Space 2", "Private (priv1)", "Family (fam1)"}

	const passes = 50
	for pass := range passes {
		p := newTestProcessor(spaces)
		got := buttonLabels(spaceButtons(t, send(t, p, "/spaces")))
		if !slices.Equal(got, want) {
			t.Fatalf("pass %d: button order = %v, want %v: custom alphabetically, then private, then family last", pass, got, want)
		}
	}
}

// TestProcessor_SpaceButtonOrderIsTotal pins the tiebreak. Two spaces can share
// a label; without a further key their relative order would ride on map
// iteration and flip between invocations.
func TestProcessor_SpaceButtonOrderIsTotal(t *testing.T) {
	spaces := map[string]any{
		"b2": map[string]any{"title": "Shared", "type": "club"},
		"a1": map[string]any{"title": "Shared", "type": "club"},
	}

	// Asserted on callback data, not labels. The labels are equal by
	// construction — that is the whole point of the fixture — so comparing them
	// would compare ["Shared","Shared"] with itself and pass however the buttons
	// were ordered. The data carries the ID, which is what the tiebreak sorts on
	// and the only thing that can show a swap.
	want := []string{"space?id=a1", "space?id=b2"}

	const passes = 50
	for pass := range passes {
		p := newTestProcessor(spaces)
		buttons := spaceButtons(t, send(t, p, "/spaces"))
		got := make([]string, 0, len(buttons))
		for _, b := range buttons {
			got = append(got, b.Data)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("pass %d: order = %v, want %v: equal labels must break on space ID, not on map iteration", pass, got, want)
		}
	}
}

// TestProcessor_SpacesWithNoSpacesSaysSoAndCarriesNoKeyboard: the reply says
// the user has no spaces and carries no keyboard at all — not an empty one. A
// renderer branches on keyboard presence to decide whether a reply is
// focusable, so an empty-but-present keyboard would leave it with a focus block
// containing nothing to focus.
func TestProcessor_SpacesWithNoSpacesSaysSoAndCarriesNoKeyboard(t *testing.T) {
	for _, tt := range []struct {
		name   string
		spaces map[string]any
	}{
		{"empty map", map[string]any{}},
		{"nil map", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestProcessor(tt.spaces)
			reply := send(t, p, "/spaces")

			if !strings.Contains(strings.ToLower(reply.Text), "no spaces") {
				t.Errorf("reply %q does not state that the user has no spaces", reply.Text)
			}
			if reply.Keyboard != nil {
				t.Errorf("reply should carry no keyboard at all, got %v", reply.Keyboard)
			}
		})
	}
}

// TestProcessor_SpacesReaderErrorIsReturnedNotFormatted is the scenario from
// _tests/processor-returns-errors-unformatted.md, against the first command
// that can actually fail: a reader error travels back as an error, never as
// reply prose the processor formatted for a surface it cannot see.
func TestProcessor_SpacesReaderErrorIsReturnedNotFormatted(t *testing.T) {
	boom := errors.New("boom")
	p := NewProcessor(Deps{Spaces: fakeSpaces{err: boom}, UID: "u1"})

	replies, err := p.SendText(context.Background(), "/spaces")
	if err == nil {
		t.Fatal("SendText(/spaces) with a failing reader = nil error, want the failure returned")
	}
	// The original survives wrapping, so a caller can still inspect it.
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, does not unwrap to the reader's error", err)
	}
	// And no reply carries user-facing error prose.
	if len(replies) != 0 {
		t.Errorf("a failure must not produce replies, got %v", replies)
	}
}

func TestProcessor_UnknownCommandNamesItAndPointsAtHelp(t *testing.T) {
	for _, tt := range []struct {
		name string
		text string
		want string // the command name the reply must echo
	}{
		{"bare", "/nope", "/nope"},
		{"with arguments", "/nope one two", "/nope"},
		{"surrounding space", "  /nope  ", "/nope"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestProcessor(twoSpaces())
			// An unknown command is answered, not failed: no error.
			reply := send(t, p, tt.text)
			if !strings.Contains(reply.Text, tt.want) {
				t.Errorf("reply %q does not name the unknown command %s", reply.Text, tt.want)
			}
			if !strings.Contains(reply.Text, "/help") {
				t.Errorf("reply %q does not point at /help", reply.Text)
			}
			if reply.Keyboard != nil {
				t.Errorf("unknown-command reply should carry no keyboard, got %v", reply.Keyboard)
			}
		})
	}
}

// Routing is by the first word, so arguments do not turn a known command into
// an unknown one.
func TestProcessor_RoutesByFirstWord(t *testing.T) {
	p := newTestProcessor(twoSpaces())
	reply := send(t, p, "/help me please")
	if strings.Contains(reply.Text, "Unknown") {
		t.Errorf("/help with arguments must still route to /help, got %q", reply.Text)
	}
	if !strings.Contains(reply.Text, "/spaces") {
		t.Errorf("reply %q is not the /help listing", reply.Text)
	}
}

func TestProcessor_FreeTextIsDeferredNotRouted(t *testing.T) {
	for _, tt := range []struct {
		name string
		text string
	}{
		{"word", "hello"},
		{"question", "what are my spaces?"},
		{"empty", ""},
		{"slash inside", "tell me about a/b"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestProcessor(twoSpaces())
			reply := send(t, p, tt.text)

			// It says the capability does not exist yet...
			if !strings.Contains(strings.ToLower(reply.Text), "not yet available") {
				t.Errorf("reply %q does not state that free-text chat is not yet available", reply.Text)
			}
			// ...and names at least one command that does work.
			if !strings.Contains(reply.Text, "/spaces") && !strings.Contains(reply.Text, "/help") {
				t.Errorf("reply %q names no working command", reply.Text)
			}
			// A reply with nothing to press carries no keyboard at all: a
			// renderer branches on keyboard presence to decide focusability.
			if reply.Keyboard != nil {
				t.Errorf("free-text reply should carry no keyboard, got %v", reply.Keyboard)
			}
		})
	}
}

// --- button presses ---

// press runs one press turn and returns the single reply it must produce, for
// the presses that are answered rather than failed.
func press(t *testing.T, p Processor, data string) Reply {
	t.Helper()
	replies, err := p.PressButton(context.Background(), data)
	if err != nil {
		t.Fatalf("PressButton(%q): unexpected error: %v", data, err)
	}
	if len(replies) != 1 {
		t.Fatalf("PressButton(%q): replies = %d, want 1: %v", data, len(replies), replies)
	}
	return replies[0]
}

// TestProcessor_PressSpaceButtonOpensTheCard: pressing a space button selects
// that space and re-renders the pressed message as the space card — named by
// label, carrying its action buttons, marked Edit (REQ: active-space-selection).
func TestProcessor_PressSpaceButtonOpensTheCard(t *testing.T) {
	p := newTestProcessor(twoSpaces()).(*processor)
	send(t, p, "/spaces")
	reply := press(t, p, "space?id=family1")

	if p.activeSpace != "family1" {
		t.Errorf("activeSpace = %q, want %q", p.activeSpace, "family1")
	}
	// The card names the space the way the button did — by title, not the ID.
	if !strings.Contains(reply.Text, "Family") || strings.Contains(reply.Text, "family1") {
		t.Errorf("card text = %q, want it to name the space by label \"Family\" and not its ID", reply.Text)
	}
	// It edits the pressed message in place rather than appending.
	if !reply.Edit {
		t.Error("a space press did not return an Edit reply; the card must re-render in place")
	}
	// It offers the card's actions, including a Contacts button.
	if reply.Keyboard == nil {
		t.Fatal("the space card carries no keyboard")
	}
	if !strings.Contains(buttonLabelsFlat(reply.Keyboard), "Contacts") {
		t.Errorf("the space card has no Contacts button; its buttons are %q", buttonLabelsFlat(reply.Keyboard))
	}
}

// buttonLabelsFlat joins every button's label across all rows, for assertions
// that a card offers a given action.
func buttonLabelsFlat(kb botkb.Keyboard) string {
	mk, ok := kb.(*botkb.MessageKeyboard)
	if !ok {
		return ""
	}
	var labels []string
	for _, row := range mk.Buttons {
		for _, b := range row {
			labels = append(labels, b.GetText())
		}
	}
	return strings.Join(labels, " | ")
}

// A space with no title names the card by its ID fallback, the same the button
// label took: the card must name what the user actually pressed.
func TestProcessor_PressSpaceButtonConfirmsByLabel(t *testing.T) {
	for _, tt := range []struct {
		name  string
		brief any
		want  string
	}{
		{"titled", map[string]any{"title": "Solo"}, "Solo"},
		{"untitled uses the same Type (id) fallback as the button", map[string]any{"title": "", "type": "family"}, "Family (solo1)"},
		{"untitled and typeless", map[string]any{"title": ""}, "solo1"},
		{"brief not a map", "not-a-brief", "solo1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestProcessor(map[string]any{"solo1": tt.brief}).(*processor)
			// List first: a press resolves against the last listing, which is
			// what the button it presses was drawn from. This is also what makes
			// the press need no fetch of its own.
			send(t, p, "/spaces")
			reply := press(t, p, "space?id=solo1")

			if p.activeSpace != "solo1" {
				t.Errorf("activeSpace = %q, want %q", p.activeSpace, "solo1")
			}
			if !strings.Contains(reply.Text, tt.want) {
				t.Errorf("reply %q does not name the newly active space %q", reply.Text, tt.want)
			}
		})
	}
}

// TestProcessor_PressingWhatSpacesEncodedSelectsThatSpace closes the loop the
// two halves of the callback vocabulary leave open: /spaces encodes the data,
// PressButton dispatches on it. Asserting each against a literal would let both
// drift to `space?spaceID=` together and still pass, so this presses exactly
// what the command emitted.
func TestProcessor_PressingWhatSpacesEncodedSelectsThatSpace(t *testing.T) {
	p := newTestProcessor(twoSpaces()).(*processor)

	buttons := spaceButtons(t, send(t, p, "/spaces"))
	for _, b := range buttons {
		reply := press(t, p, b.Data)
		if !strings.Contains(reply.Text, b.GetText()) {
			t.Errorf("pressing %q: reply %q does not name the space the button labelled %q",
				b.Data, reply.Text, b.GetText())
		}
	}
	// The last button pressed is the space left active — family, which the
	// ordering pins to the end.
	if want := "family1"; p.activeSpace != want {
		t.Errorf("activeSpace = %q, want %q", p.activeSpace, want)
	}
}

// TestProcessor_UnhandleablePressIsAnsweredNotDropped is the scenario's
// unrecognized-data block (REQ: unrecognized-callback-data). Each trigger is
// structural: the data does not parse, names an unknown path, or omits the
// argument its path requires.
//
// The REQ permits either a reply or an error; this processor answers all three
// with a reply, and the test asserts both halves. The general contract — never
// nothing, never a panic — is asserted first, so the test still states the
// requirement rather than only the choice made under it.
func TestProcessor_UnhandleablePressIsAnsweredNotDropped(t *testing.T) {
	for _, tt := range []struct {
		name string
		data string
	}{
		{"unknown path", "nope?id=1"},
		{"unparseable", "%zz"},
		{"required argument omitted", "space"},
		{"no data at all", ""},
		{"unknown path, no arguments", "nope"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestProcessor(twoSpaces()).(*processor)

			replies, err := p.PressButton(context.Background(), tt.data)

			// The contract: a reply or an error, never silently nothing.
			if err == nil && len(replies) == 0 {
				t.Fatalf("PressButton(%q) returned no replies and no error — a silent no-op", tt.data)
			}
			// The choice made under it: an answerable press is answered, so the
			// user reads it as the bot's turn rather than as a failure.
			if err != nil {
				t.Fatalf("PressButton(%q) = error %v, want a reply saying it could not be handled", tt.data, err)
			}
			if len(replies) != 1 {
				t.Fatalf("PressButton(%q): replies = %d, want 1: %v", tt.data, len(replies), replies)
			}
			if !strings.Contains(strings.ToLower(replies[0].Text), "could not be handled") {
				t.Errorf("reply %q does not say the action could not be handled", replies[0].Text)
			}
			if replies[0].Keyboard != nil {
				t.Errorf("reply should carry no keyboard, got %v", replies[0].Keyboard)
			}
			// Data nobody can dispatch selects nothing.
			if p.activeSpace != "" {
				t.Errorf("activeSpace = %q, want it untouched by an unhandleable press", p.activeSpace)
			}
		})
	}
}

// An argument passed empty is not an argument omitted: `space?id=` carries the
// id the path requires, so it is dispatched and fails on the lookup — the stale
// case, an error — rather than being answered as structurally unhandleable.
// This is the distinction callbackData.arg's bool exists to make.
func TestProcessor_PressSpaceWithEmptyIDIsAStaleLookupNotAMissingArgument(t *testing.T) {
	p := newTestProcessor(twoSpaces()).(*processor)

	replies, err := p.PressButton(context.Background(), "space?id=")
	if err == nil {
		t.Fatalf("PressButton(space?id=) = nil error, want the lookup failure returned; replies: %v", replies)
	}
	if len(replies) != 0 {
		t.Errorf("a failure must not produce replies, got %v", replies)
	}
	if p.activeSpace != "" {
		t.Errorf("activeSpace = %q, want it unchanged by a failed selection", p.activeSpace)
	}
}

// TestProcessor_PressStaleSpaceButtonErrorsAndKeepsTheActiveSpace is the
// scenario's stale-button block: an id naming no space the reader returns is an
// error, and the active space survives it (REQ: active-space-selection).
func TestProcessor_PressStaleSpaceButtonErrorsAndKeepsTheActiveSpace(t *testing.T) {
	p := newTestProcessor(twoSpaces()).(*processor)
	send(t, p, "/spaces")

	// Establish the active space through the same door the user would use.
	press(t, p, "space?id=family1")
	if p.activeSpace != "family1" {
		t.Fatalf("setup: activeSpace = %q, want %q", p.activeSpace, "family1")
	}

	replies, err := p.PressButton(context.Background(), "space?id=ghost1")
	if err == nil {
		t.Fatalf("PressButton(space?id=ghost1) = nil error, want an error; replies: %v", replies)
	}
	// The failure is returned, not formatted into prose the renderer would show
	// as an ordinary bot turn (REQ: errors-are-returned-not-formatted).
	if len(replies) != 0 {
		t.Errorf("a failure must not produce replies, got %v", replies)
	}
	if p.activeSpace != "family1" {
		t.Errorf("activeSpace = %q, want %q — a failed selection must not change it", p.activeSpace, "family1")
	}
}

// A reader failure during a press comes back as an error, like /spaces's does:
// the selection cannot be confirmed if the spaces it would name never arrived.
func TestProcessor_PressDoesNotFetchSpaces(t *testing.T) {
	// A press must resolve against the last listing without fetching again —
	// that redundant fetch was a real, user-visible delay on every selection
	// (REQ: active-space-selection). Pinned by counting reads: one for /spaces,
	// none for the press.
	r := &countingSpaces{spaces: twoSpaces()}
	p := NewProcessor(Deps{Spaces: r, UID: "u1"}).(*processor)

	send(t, p, "/spaces")
	if r.calls != 1 {
		t.Fatalf("setup: /spaces made %d reads, want 1", r.calls)
	}

	press(t, p, "space?id=family1")
	if r.calls != 1 {
		t.Errorf("a press made %d reads in total, want the press to add none: it must resolve against the last listing", r.calls)
	}
	if p.activeSpace != "family1" {
		t.Errorf("activeSpace = %q, want family1", p.activeSpace)
	}
}

// countingSpaces is a spaces reader that records how many times it was read, to
// prove a press adds no fetch of its own.
type countingSpaces struct {
	spaces map[string]any
	calls  int
}

func (c *countingSpaces) ListSpaces(context.Context, string) (map[string]any, error) {
	c.calls++
	return c.spaces, nil
}

// sandboxPackages are the packages that wire the sandbox — a mock LLM over a
// fake space and user, on an in-memory or OpenVaultDB store. Reaching any of
// them from here would let a real-data transcript execute fixture actions,
// which is the whole reason free text stops at the deferral reply.
var sandboxPackages = []struct {
	path string
	why  string
}{
	{"convoruntime", "the sandbox-only conversational runtime"},
	{"convodev", "sandbox space/user fixtures"},
	{"convosetup", "sandbox runtime wiring"},
	{"llmmock", "the sandbox mock LLM"},
	{"dalgo2memory", "the sandbox in-memory datastore"},
	{"dalgo2openvaultdb", "the sandbox OpenVaultDB datastore"},
	{"sneat-cli/cmd/sneat/commands", "it imports convoruntime, so importing it reaches the runtime transitively"},
}

// TestPackage_DoesNotReachTheSandboxRuntime is the structural half of
// chat-messenger#req:free-text-deferred: the deferral reply is only honest if
// there is no path from this package to the sandbox at all.
func TestPackage_DoesNotReachTheSandboxRuntime(t *testing.T) {
	// Direct imports, read from the source: covers every file in the package,
	// tests included, and needs no toolchain.
	t.Run("direct imports", func(t *testing.T) {
		entries, err := os.ReadDir(".")
		if err != nil {
			t.Fatalf("read package dir: %v", err)
		}
		fset := token.NewFileSet()
		var inspected int
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			f, err := parser.ParseFile(fset, e.Name(), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("parse %s: %v", e.Name(), err)
			}
			inspected++
			for _, imp := range f.Imports {
				assertNotSandbox(t, e.Name(), strings.Trim(imp.Path.Value, `"`))
			}
		}
		if inspected == 0 {
			t.Fatal("inspected no .go files — the guard would pass vacuously")
		}
	})

	// Transitive dependencies: an import of an innocent-looking package that
	// itself reaches the runtime is the failure mode worth guarding.
	t.Run("transitive dependencies", func(t *testing.T) {
		out, err := exec.Command("go", "list", "-deps", ".").CombinedOutput()
		if err != nil {
			t.Fatalf("go list -deps: %v\n%s", err, out)
		}
		deps := strings.Fields(string(out))
		if len(deps) == 0 {
			t.Fatal("go list returned no dependencies — the guard would pass vacuously")
		}
		for _, dep := range deps {
			assertNotSandbox(t, "package dependencies", dep)
		}
	})
}

// assertNotSandbox fails when importPath belongs to the sandbox wiring.
func assertNotSandbox(t *testing.T, where, importPath string) {
	t.Helper()
	for _, bad := range sandboxPackages {
		if strings.Contains(importPath, bad.path) {
			t.Errorf("%s: depends on %q — %s; free text must stop at the deferral reply", where, importPath, bad.why)
		}
	}
}

// --- new commands (REQ: space-command, whoami-command, version-command, contacts-command, command-registry) ---

func TestProcessor_HelpAndRegistryNameEveryCommand(t *testing.T) {
	p := newTestProcessor(twoSpaces())
	// /help renders the registry, so every registered command appears in both.
	help := send(t, p, "/help").Text
	names := make(map[string]bool)
	for _, c := range p.(*processor).Commands() {
		names[c.Name] = true
		if !strings.Contains(help, c.Name) {
			t.Errorf("/help does not name %q, but the registry lists it", c.Name)
		}
	}
	for _, want := range []string{"/spaces", "/space", "/who-am-i", "/contacts", "/version", "/help"} {
		if !names[want] {
			t.Errorf("the command registry is missing %q", want)
		}
	}
	// The arg hint reaches the registry for a command that takes one.
	for _, c := range p.(*processor).Commands() {
		if c.Name == "/contacts" && c.Arg == "" {
			t.Error("/contacts registry entry carries no arg hint")
		}
	}
}

func TestProcessor_WhoAmINamesTheEmail(t *testing.T) {
	p := NewProcessor(Deps{Spaces: fakeSpaces{}, UID: "u1", Email: "user@example.com"})
	if got := send(t, p, "/who-am-i").Text; !strings.Contains(got, "user@example.com") {
		t.Errorf("/who-am-i = %q, does not name the email", got)
	}
}

func TestProcessor_VersionNamesTheBuild(t *testing.T) {
	p := NewProcessor(Deps{Spaces: fakeSpaces{}, UID: "u1", Version: "1.2.3"})
	if got := send(t, p, "/version").Text; !strings.Contains(got, "1.2.3") {
		t.Errorf("/version = %q, does not name the version", got)
	}
}

func TestProcessor_SpaceReportsTheActiveSpace(t *testing.T) {
	p := newTestProcessor(twoSpaces()).(*processor)

	// None selected yet.
	if got := send(t, p, "/space").Text; !strings.Contains(strings.ToLower(got), "no space") || !strings.Contains(got, "/spaces") {
		t.Errorf("/space with none selected = %q, want it to say none is selected and point at /spaces", got)
	}

	// After a press, it names the active space by label.
	send(t, p, "/spaces")
	press(t, p, "space?id=family1")
	if got := send(t, p, "/space").Text; !strings.Contains(got, "Family") {
		t.Errorf("/space after selecting family1 = %q, want it to name Family", got)
	}
}

func TestProcessor_ContactsResolvesTheSpace(t *testing.T) {
	spaces := map[string]any{
		"vaoyj": map[string]any{"title": "", "type": "family"},
		"ao58m": map[string]any{"title": "", "type": "private"},
		"c1":    map[string]any{"title": "Club A", "type": "club"},
		"c2":    map[string]any{"title": "Club B", "type": "club"},
	}
	contacts := fakeContacts{bySpace: map[string][]Contact{
		"vaoyj": {{Name: "Carol"}},
		"ao58m": {{Name: "Alice"}, {Name: "Bob"}},
	}}
	newP := func() *processor {
		return NewProcessor(Deps{Spaces: fakeSpaces{spaces: spaces}, Contacts: contacts, UID: "u1"}).(*processor)
	}

	t.Run("no arg, no active space, points at /spaces", func(t *testing.T) {
		got := send(t, newP(), "/contacts").Text
		if !strings.Contains(strings.ToLower(got), "no space") || !strings.Contains(got, "/spaces") {
			t.Errorf("= %q, want it to say no space is selected and point at /spaces", got)
		}
	})

	t.Run("no arg uses the active space", func(t *testing.T) {
		p := newP()
		send(t, p, "/spaces")
		press(t, p, "space?id=ao58m")
		got := send(t, p, "/contacts").Text
		if !strings.Contains(got, "Alice") || !strings.Contains(got, "Bob") {
			t.Errorf("= %q, want Alice and Bob from the active space", got)
		}
	})

	t.Run("type resolves to the one space of that type", func(t *testing.T) {
		got := send(t, newP(), "/contacts family").Text
		if !strings.Contains(got, "Carol") {
			t.Errorf("/contacts family = %q, want Carol from the family space", got)
		}
	})

	t.Run("exact ID", func(t *testing.T) {
		got := send(t, newP(), "/contacts vaoyj").Text
		if !strings.Contains(got, "Carol") {
			t.Errorf("/contacts vaoyj = %q, want Carol", got)
		}
	})

	t.Run("ambiguous type is named, not guessed", func(t *testing.T) {
		got := send(t, newP(), "/contacts club").Text
		if !strings.Contains(strings.ToLower(got), "club") || strings.Contains(got, "Club A") {
			t.Errorf("/contacts club = %q, want it to report the type as ambiguous rather than list one club's contacts", got)
		}
	})

	t.Run("no such space is answered, not errored", func(t *testing.T) {
		replies, err := newP().SendText(context.Background(), "/contacts nope")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(replies) != 1 || !strings.Contains(replies[0].Text, "nope") {
			t.Errorf("= %v, want a reply naming the unknown space \"nope\"", replies)
		}
	})
}

func TestProcessor_ContactsReaderErrorIsReturnedNotFormatted(t *testing.T) {
	boom := errors.New("boom")
	p := NewProcessor(Deps{
		Spaces:   fakeSpaces{spaces: map[string]any{"s1": map[string]any{"type": "family"}}},
		Contacts: fakeContacts{err: boom},
		UID:      "u1",
	})
	replies, err := p.SendText(context.Background(), "/contacts family")
	if err == nil || !errors.Is(err, boom) {
		t.Errorf("err = %v, want the reader's error returned unwrapped", err)
	}
	if len(replies) != 0 {
		t.Errorf("a failure must produce no replies, got %v", replies)
	}
}

// --- card navigation (REQ: card-edit, active-space-selection, contacts-card, button-kinds) ---

// cardSetup lists spaces and opens the family space card, returning the processor
// mid-navigation for the tests below.
func cardSetup(t *testing.T) *processor {
	t.Helper()
	p := NewProcessor(Deps{
		Spaces:   fakeSpaces{spaces: map[string]any{"vaoyj": map[string]any{"title": "", "type": "family"}}},
		Contacts: fakeContacts{bySpace: map[string][]Contact{"vaoyj": {{Name: "Alice"}, {Name: "Bob"}}}},
		UID:      "u1",
	}).(*processor)
	send(t, p, "/spaces")
	return p
}

func TestProcessor_SpaceCardHasAllThreeButtonKinds(t *testing.T) {
	p := cardSetup(t)
	card := press(t, p, "space?id=vaoyj")
	mk, ok := card.Keyboard.(*botkb.MessageKeyboard)
	if !ok {
		t.Fatalf("space card keyboard is %T", card.Keyboard)
	}
	var kinds = map[botkb.ButtonType]int{}
	for _, row := range mk.Buttons {
		for _, b := range row {
			kinds[b.ButtonType()]++
		}
	}
	// One card carries a callback, a URL, and a send button — the three kinds
	// (REQ: button-kinds).
	for kind, name := range map[botkb.ButtonType]string{
		botkb.ButtonTypeData: "callback",
		botkb.ButtonTypeURL:  "URL",
		botkb.ButtonTypeText: "send",
	} {
		if kinds[kind] == 0 {
			t.Errorf("the space card has no %s button; kinds present: %v", name, kinds)
		}
	}
	// The URL button opens the space in the web app.
	for _, row := range mk.Buttons {
		for _, b := range row {
			if u, ok := b.(*botkb.UrlButton); ok && !strings.Contains(u.URL, "vaoyj") {
				t.Errorf("URL button opens %q, want it to name the space vaoyj", u.URL)
			}
		}
	}
}

func TestProcessor_ContactsCardListsAndOffersBack(t *testing.T) {
	p := cardSetup(t)
	press(t, p, "space?id=vaoyj") // open the space card first (sets the cache-backed label)
	card := press(t, p, "contacts?space=vaoyj")

	if !card.Edit {
		t.Error("the contacts card is not an Edit reply; it must re-render the card in place")
	}
	if !strings.Contains(card.Text, "Alice") || !strings.Contains(card.Text, "Bob") {
		t.Errorf("contacts card = %q, want it to list Alice and Bob", card.Text)
	}
	if !strings.Contains(buttonLabelsFlat(card.Keyboard), "Back") {
		t.Errorf("the contacts card has no ← Back button; its buttons are %q", buttonLabelsFlat(card.Keyboard))
	}
}

func TestProcessor_CardNavigationIsALoop(t *testing.T) {
	p := cardSetup(t)

	space := press(t, p, "space?id=vaoyj")
	if !strings.Contains(space.Text, "Space:") {
		t.Fatalf("space card = %q", space.Text)
	}
	contacts := press(t, p, "contacts?space=vaoyj")
	if !strings.Contains(contacts.Text, "Alice") {
		t.Fatalf("contacts card = %q", contacts.Text)
	}
	// ← Back returns to the space card.
	back := press(t, p, "space?id=vaoyj")
	if !strings.Contains(back.Text, "Space:") || !back.Edit {
		t.Errorf("← Back = %q (edit=%v), want the space card as an Edit reply", back.Text, back.Edit)
	}
	// ← Spaces returns to the list, as an Edit (re-render), not a new turn.
	list := press(t, p, "spaces")
	if !strings.Contains(list.Text, "space") || !list.Edit {
		t.Errorf("← Spaces = %q (edit=%v), want the spaces list re-rendered in place", list.Text, list.Edit)
	}
	if list.Keyboard == nil {
		t.Error("the re-rendered spaces list carries no space buttons")
	}
}
