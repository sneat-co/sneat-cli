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
	return NewProcessor(fakeSpaces{spaces: spaces}, "u1")
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
// returned, one titled button per row, ordered by space ID.
func TestProcessor_SpacesListsRealSpacesAsButtons(t *testing.T) {
	p := newTestProcessor(twoSpaces())
	reply := send(t, p, "/spaces")

	// The reply text states that the user has 2 spaces.
	if !strings.Contains(reply.Text, "2 spaces") {
		t.Errorf("reply %q does not state that the user has 2 spaces", reply.Text)
	}

	// The keyboard has exactly 2 rows, each holding one button, labelled with
	// the space titles and ordered by space ID: family1 precedes personal1.
	buttons := spaceButtons(t, reply)
	want := []string{"Family", "Personal"}
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
		{"title empty", map[string]any{"title": "", "type": "private"}, "solo1"},
		{"title absent", map[string]any{"type": "private"}, "solo1"},
		{"title not a string", map[string]any{"title": 42}, "solo1"},
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
func TestProcessor_SpaceButtonsAreOrderedByID(t *testing.T) {
	// Inserted out of order, and titled with nothing, so each button's label is
	// its own ID and the assertion reads the ordering directly.
	spaces := map[string]any{}
	for _, id := range []string{"s07", "s02", "s10", "s04", "s09", "s01", "s06", "s03", "s08", "s05"} {
		spaces[id] = map[string]any{"title": ""}
	}
	want := []string{"s01", "s02", "s03", "s04", "s05", "s06", "s07", "s08", "s09", "s10"}

	const passes = 50
	for pass := range passes {
		p := newTestProcessor(spaces)
		reply := send(t, p, "/spaces")
		got := buttonLabels(spaceButtons(t, reply))
		if !slices.Equal(got, want) {
			t.Fatalf("pass %d: buttons = %v, want them ordered by space ID %v", pass, got, want)
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
	p := NewProcessor(fakeSpaces{err: boom}, "u1")

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
