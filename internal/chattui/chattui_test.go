package chattui

import (
	"context"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sneat-co/sneat-cli/internal/chat"
)

// --- fakes ---

// fakeProcessor answers turns from canned replies, so a model can be driven
// without a space reader behind it.
type fakeProcessor struct {
	replies []chat.Reply
	err     error
}

func (f fakeProcessor) SendText(context.Context, string) ([]chat.Reply, error) {
	return f.replies, f.err
}

func (f fakeProcessor) PressButton(context.Context, string) ([]chat.Reply, error) {
	return f.replies, f.err
}

// --- alt screen ---

// altScreenBit mirrors bubbletea's own startupOptions bit for the alternate
// screen — `withAltScreen startupOptions = 1 << iota`, the first bit, in its
// tea.go. Both the bit and the field it lives in are unexported, so the value
// is restated here and altScreenStartup reads the field reflectively.
//
// That restatement is only safe because it is checked rather than trusted:
// TestNewProgramWithAltScreenIsDetected builds a program that does ask for the
// alternate screen and fails if this bit does not show up in it. Without that
// control, a bubbletea rename would silently turn the assertion below into one
// that passes no matter what Run does.
const altScreenBit = 1

// altScreenStartup reports whether p was constructed with tea.WithAltScreen().
//
// tea.NewProgram applies each ProgramOption to the Program it returns, so the
// options a program was given are readable off the program itself — there is no
// exported accessor, hence the reflection. Reading an unexported field's scalar
// value is allowed; only Interface() and the setters reject a read-only Value.
func altScreenStartup(t *testing.T, p *tea.Program) bool {
	t.Helper()
	f := reflect.ValueOf(p).Elem().FieldByName("startupOptions")
	if !f.IsValid() {
		t.Fatal("tea.Program has no startupOptions field: bubbletea changed, and this test can no longer see whether the alternate screen was requested")
	}
	return f.Int()&altScreenBit != 0
}

// TestNewProgramWithAltScreenIsDetected is the control for the assertion below:
// it proves altScreenStartup actually observes the alternate-screen option, so
// that TestNewProgramDoesNotUseAltScreen is a test that can fail.
func TestNewProgramWithAltScreenIsDetected(t *testing.T) {
	p := tea.NewProgram(New(fakeProcessor{}), tea.WithAltScreen())
	if !altScreenStartup(t, p) {
		t.Fatal("a program built with tea.WithAltScreen() did not report the alt-screen startup option: altScreenStartup no longer detects it")
	}
}

// TestNewProgramDoesNotUseAltScreen pins REQ: inline-rendering — the chat draws
// in the terminal's normal buffer, so the transcript survives exit as ordinary
// scrollback. Taking over the screen would discard it.
func TestNewProgramDoesNotUseAltScreen(t *testing.T) {
	p := newProgram(fakeProcessor{})
	if altScreenStartup(t, p) {
		t.Error("the chat program was constructed with the alternate screen; it must render inline so the transcript stays in terminal scrollback (chat-tui#req:inline-rendering)")
	}
}

// --- model ---

// TestNewStartsOnTheInputWithNothingLive pins the state a session opens in:
// focus on the input, no live reply to focus buttons in, and no reply in
// flight. Task 4's focus movement and pending lock are written against it.
func TestNewStartsOnTheInputWithNothingLive(t *testing.T) {
	m := New(fakeProcessor{})
	if m.focus != focusInput {
		t.Errorf("focus = %v, want focusInput", m.focus)
	}
	if m.live != nil {
		t.Errorf("live = %+v, want nil: a session opens with no focusable reply", m.live)
	}
	if m.pending {
		t.Error("pending = true, want false: nothing is in flight before the first turn")
	}
	if m.row != 0 || m.col != 0 {
		t.Errorf("button cursor = (%d,%d), want (0,0)", m.row, m.col)
	}
}

// TestModelHandlesWindowSize checks the root model records the terminal width
// it renders the live region to.
func TestModelHandlesWindowSize(t *testing.T) {
	m, _ := New(fakeProcessor{}).Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if got := m.(Model).width; got != 80 {
		t.Errorf("width = %d, want 80", got)
	}
}

// TestModelQuitsOnCtrlC checks the always-available exit. The rest of the key
// table is Task 4's.
func TestModelQuitsOnCtrlC(t *testing.T) {
	_, cmd := New(fakeProcessor{}).Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c returned no command, want tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("ctrl+c produced %T, want tea.QuitMsg", cmd())
	}
}
