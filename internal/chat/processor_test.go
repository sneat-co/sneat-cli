package chat

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"strings"
	"testing"
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
