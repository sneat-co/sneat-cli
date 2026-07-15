package chattui

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/bots-go-framework/bots-go-core/botkb"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sneat-co/sneat-cli/internal/chat"
)

// --- fakes ---

// fakeProcessor answers turns from canned replies, so a model can be driven
// without a space reader behind it.
//
// It records what it was asked in sent, when a test supplies somewhere to
// record: that a turn reached the Processor at all — and from a command rather
// than from Update — is otherwise not observable from the model.
type fakeProcessor struct {
	replies []chat.Reply
	err     error
	sent    *[]string
}

func (f fakeProcessor) SendText(_ context.Context, text string) ([]chat.Reply, error) {
	if f.sent != nil {
		*f.sent = append(*f.sent, text)
	}
	return f.replies, f.err
}

func (f fakeProcessor) PressButton(context.Context, string) ([]chat.Reply, error) {
	return f.replies, f.err
}

// spacesKeyboard is a two-row keyboard standing in for what a `/spaces` turn
// answers with.
func spacesKeyboard() botkb.Keyboard {
	return botkb.NewMessageKeyboard(botkb.KeyboardTypeInline,
		[]botkb.Button{botkb.NewDataButton("Family", "space=family")},
		[]botkb.Button{botkb.NewDataButton("Work", "space=work")},
	)
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

// --- scrollback ---
//
// A committed turn is handed to the terminal, not kept in the model, so "was it
// committed" cannot be read off model state — by design, that is the whole point
// of REQ: scrollback-commit. It is read instead off the commands Update returns,
// by running them and inspecting the messages they produce, which is exactly
// what the event loop would do with them.

// printedBody reports the text msg prints to scrollback, and whether it is such
// a message at all.
//
// tea.Println is the mechanism that makes a turn durable: it prints above the
// live region, and the program never repaints what it printed. But its message
// type (printLineMessage) and that type's body field are both unexported, so
// what a command commits cannot be reached by a type assertion — hence the
// reflection. Reading an unexported field's string value is allowed; only
// Interface() and the setters reject a read-only Value.
//
// Like altScreenStartup above, this is checked rather than trusted:
// TestPrintlnBodyIsReadable is its control.
func printedBody(msg tea.Msg) (string, bool) {
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Struct || v.Type().Name() != "printLineMessage" {
		return "", false
	}
	f := v.FieldByName("messageBody")
	if !f.IsValid() || f.Kind() != reflect.String {
		return "", false
	}
	return f.String(), true
}

// sequencedCmds returns the commands msg runs in order, and whether it is a
// tea.Sequence message at all.
//
// Sequence's message type (sequenceMsg) is unexported, so it is matched by kind
// and name. Its elements need no loophole: they are exported tea.Cmd values, and
// a value read out of an interface is not read-only the way an unexported field
// is. TestSequenceCmdsAreReadable is its control.
func sequencedCmds(msg tea.Msg) ([]tea.Cmd, bool) {
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Slice || v.Type().Name() != "sequenceMsg" {
		return nil, false
	}
	out := make([]tea.Cmd, 0, v.Len())
	for i := 0; i < v.Len(); i++ {
		c, ok := v.Index(i).Interface().(tea.Cmd)
		if !ok {
			return nil, false
		}
		out = append(out, c)
	}
	return out, true
}

// drain runs cmd and returns every message it produces, in order, flattening
// tea.Sequence and tea.Batch bundles into the commands they carry.
func drain(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	var out []tea.Msg
	var walk func(tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		msg := c()
		if msg == nil {
			return
		}
		if cmds, ok := sequencedCmds(msg); ok {
			for _, sub := range cmds {
				walk(sub)
			}
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, sub := range batch {
				walk(sub)
			}
			return
		}
		out = append(out, msg)
	}
	walk(cmd)
	return out
}

// scrollbackOf filters drained messages down to what reached the terminal.
func scrollbackOf(msgs []tea.Msg) []string {
	var out []string
	for _, msg := range msgs {
		if body, ok := printedBody(msg); ok {
			out = append(out, body)
		}
	}
	return out
}

// submitTurn drives one whole turn: it types text, presses enter, and hands the
// Processor's answer back to the model the way the event loop would. It returns
// the model the turn left behind, and everything the turn committed to
// scrollback, in order.
func submitTurn(t *testing.T, m Model, text string) (Model, []string) {
	t.Helper()
	m.input.SetValue(text)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	var sb []string
	for _, msg := range drain(t, cmd) {
		if body, ok := printedBody(msg); ok {
			sb = append(sb, body)
			continue
		}
		next, cmd := m.Update(msg)
		m = next.(Model)
		sb = append(sb, scrollbackOf(drain(t, cmd))...)
	}
	return m, sb
}

// TestPrintlnBodyIsReadable is the control for printedBody: it proves the
// reflection above observes what a tea.Println command actually prints, so the
// commit assertions below are tests that can fail. Without this control, a
// bubbletea rename would silently turn every one of them into an assertion that
// nothing is ever committed to scrollback — which passes no matter what the
// model does.
func TestPrintlnBodyIsReadable(t *testing.T) {
	body, ok := printedBody(tea.Println("hello scrollback")())
	if !ok {
		t.Fatal("printedBody did not recognise a tea.Println message: bubbletea changed, and the commit assertions can no longer see what reaches scrollback")
	}
	if body != "hello scrollback" {
		t.Errorf("printedBody = %q, want %q", body, "hello scrollback")
	}
}

// TestSequenceCmdsAreReadable is the control for sequencedCmds, and with it for
// drain: it proves a tea.Sequence is walked into the commands it runs rather
// than dropped as an unrecognised message — which would make every commit made
// from inside a sequence invisible to the assertions below.
func TestSequenceCmdsAreReadable(t *testing.T) {
	got := scrollbackOf(drain(t, tea.Sequence(tea.Println("first"), tea.Println("second"))))
	want := []string{"first", "second"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("drained a tea.Sequence to %q, want %q: sequencedCmds no longer recognises one", got, want)
	}
}

// --- committing a submitted turn (REQ: scrollback-commit) ---

// TestSubmitCommitsTheUserLineAndSendsOffTheUIThread pins the first half of the
// commit rule for submitted text: the line the user typed reaches scrollback
// before the reply it prompts, and the turn reaches the Processor from a command
// rather than from Update — SendText does network I/O, and the live region must
// not block on it.
func TestSubmitCommitsTheUserLineAndSendsOffTheUIThread(t *testing.T) {
	var sent []string
	m := New(fakeProcessor{
		replies: []chat.Reply{{Text: "Your spaces:", Keyboard: spacesKeyboard()}},
		sent:    &sent,
	})
	m.input.SetValue("/spaces")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if len(sent) != 0 {
		t.Errorf("SendText was called with %q from Update itself; it must run off the UI thread as a command", sent)
	}
	if !m.pending {
		t.Error("pending = false after a submit, want true: a reply is in flight")
	}
	if m.input.Value() != "" {
		t.Errorf("input = %q after a submit, want empty: the line moved to scrollback", m.input.Value())
	}

	msgs := drain(t, cmd)
	if len(msgs) == 0 {
		t.Fatal("a submit produced no messages, want the scrollback commit and the Processor's answer")
	}
	if _, ok := printedBody(msgs[0]); !ok {
		t.Errorf("the first message a submit produces is %T, want the scrollback commit: the submitted line commits before the replies it prompts render (chat-tui#req:scrollback-commit)", msgs[0])
	}
	if got := scrollbackOf(msgs); len(got) != 1 || !strings.Contains(got[0], "/spaces") {
		t.Fatalf("a submit committed %q to scrollback, want exactly one block echoing %q", got, "/spaces")
	}
	if !reflect.DeepEqual(sent, []string{"/spaces"}) {
		t.Errorf("SendText received %q, want [%q]", sent, "/spaces")
	}
}

// TestSubmitSequencesTheCommitAheadOfTheSend pins how that ordering is
// guaranteed rather than merely likely. tea.Batch would run the commit and the
// send concurrently, so a Processor that answers fast enough could land its
// reply in scrollback ahead of the line that prompted it; tea.Sequence dispatches
// the commit's message before the send even starts (chat-tui#req:scrollback-commit).
func TestSubmitSequencesTheCommitAheadOfTheSend(t *testing.T) {
	m := New(fakeProcessor{replies: []chat.Reply{{Text: "Hi!"}}})
	m.input.SetValue("hello")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("a submit returned no command")
	}
	cmds, ok := sequencedCmds(cmd())
	if !ok {
		t.Fatalf("a submit returned %T, want a tea.Sequence: the commit must be ordered ahead of the send, not raced against it", cmd())
	}
	if len(cmds) != 2 {
		t.Fatalf("a submit sequenced %d commands, want 2: the commit then the send", len(cmds))
	}
	if _, ok := printedBody(cmds[0]()); !ok {
		t.Error("the first command a submit sequences does not commit to scrollback; the commit must come first")
	}
}

// TestSubmitIgnoresAnEmptyLine checks that a bare enter is not a turn: it would
// otherwise reach the Processor with nothing and leave a blank echo in the
// transcript.
func TestSubmitIgnoresAnEmptyLine(t *testing.T) {
	var sent []string
	m := New(fakeProcessor{sent: &sent})
	m.input.SetValue("   ")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if got := scrollbackOf(drain(t, cmd)); len(got) != 0 {
		t.Errorf("an empty submit committed %q to scrollback, want nothing", got)
	}
	if len(sent) != 0 {
		t.Errorf("an empty submit reached the Processor with %q", sent)
	}
	if m.pending {
		t.Error("pending = true after an empty submit, want false: nothing is in flight")
	}
}

// TestButtonedReplyBecomesLiveAndRendersInTheLiveRegion pins the second half of
// the rule: a reply carrying buttons is not complete on arrival — its buttons
// are still focusable — so it stays out of scrollback and is drawn in the live
// region instead (chat-tui#req:scrollback-commit, chat-tui#req:inline-rendering).
func TestButtonedReplyBecomesLiveAndRendersInTheLiveRegion(t *testing.T) {
	proc := fakeProcessor{replies: []chat.Reply{{Text: "Your spaces:", Keyboard: spacesKeyboard()}}}
	m, sb := submitTurn(t, New(proc), "/spaces")

	if m.live == nil {
		t.Fatal("live = nil after a reply carrying a keyboard; it stays live while its buttons are focusable (chat-tui#req:scrollback-commit)")
	}
	if m.live.Text != "Your spaces:" {
		t.Errorf("live.Text = %q, want %q", m.live.Text, "Your spaces:")
	}
	for _, block := range sb {
		if strings.Contains(block, "Your spaces:") {
			t.Errorf("a reply carrying a keyboard was committed to scrollback (%q); it is not complete until its buttons stop being focusable (chat-tui#req:scrollback-commit)", block)
		}
	}
	view := m.View()
	for _, want := range []string{"Your spaces:", "Family", "Work"} {
		if !strings.Contains(view, want) {
			t.Errorf("View() = %q, missing %q: the live region draws the focusable reply and its buttons (chat-tui#req:inline-rendering)", view, want)
		}
	}
}

// TestSubmittingAgainCommitsTheLiveReplyWithInertButtons pins what supersedes a
// live reply. Submitting is user input, so the reply that was still focusable
// commits first — with its buttons inert, because they stop being focusable at
// exactly that moment — and the echo of the new line follows it, both before the
// new turn's replies render (chat-tui#req:scrollback-commit).
func TestSubmittingAgainCommitsTheLiveReplyWithInertButtons(t *testing.T) {
	proc := fakeProcessor{replies: []chat.Reply{{Text: "Your spaces:", Keyboard: spacesKeyboard()}}}
	m, _ := submitTurn(t, New(proc), "/spaces")
	if m.live == nil {
		t.Fatal("setup: expected a live reply after /spaces")
	}
	m.proc = fakeProcessor{replies: []chat.Reply{{Text: "Hi!"}}}

	m.input.SetValue("hello")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	msgs := drain(t, cmd)
	sb := scrollbackOf(msgs)
	if len(sb) != 1 {
		t.Fatalf("a submit over a live reply committed %q, want one block carrying the superseded reply then the echo", sb)
	}
	block := sb[0]
	iReply := strings.Index(block, "Your spaces:")
	iEcho := strings.Index(block, "hello")
	switch {
	case iReply < 0:
		t.Errorf("the superseded live reply is missing from the committed block %q (chat-tui#req:scrollback-commit)", block)
	case iEcho < 0:
		t.Errorf("the submitted line is missing from the committed block %q", block)
	case iReply > iEcho:
		t.Errorf("the committed block %q echoes the new line before the reply it supersedes; the live reply commits first (chat-tui#req:scrollback-commit)", block)
	}
	for _, label := range []string{"Family", "Work"} {
		if !strings.Contains(block, label) {
			t.Errorf("the committed block %q is missing button %q: a committed reply keeps its buttons as inert text (chat-tui#req:scrollback-commit)", block, label)
		}
	}
	if strings.Contains(block, focusedMarkLeft) {
		t.Errorf("the committed block %q marks a button as focused; committed buttons are inert text (chat-tui#req:focus-and-keys)", block)
	}
	if m.live != nil {
		t.Errorf("live = %+v after its reply was superseded, want nil", m.live)
	}
	if m.focus != focusInput {
		t.Errorf("focus = %v after the live reply committed, want focusInput: the button block it pointed into no longer exists", m.focus)
	}
}

// TestCommitLiveRendersInertButtonsAndReturnsFocus pins inertness from the one
// state where it is a claim that can actually fail: with focus sitting in the
// live reply's button block. Committing it renders the same reply the live region
// is drawing right now, and it must come out without the focus mark — the buttons
// stop being focusable at the moment the reply commits (chat-tui#req:scrollback-commit).
// Focus comes back to the input with it, because the block it pointed into is
// gone.
//
// Text submission cannot reach this state — from the button block, enter is a
// press rather than a submit — so it is driven directly here. It is the state
// Task 5's press path commits from.
func TestCommitLiveRendersInertButtonsAndReturnsFocus(t *testing.T) {
	m := New(fakeProcessor{})
	reply := chat.Reply{Text: "Your spaces:", Keyboard: spacesKeyboard()}
	m.live = &reply
	m.focus, m.row, m.col = focusButtons, 1, 0

	// The live region marks the focused button: that is what the commit below
	// must not carry into scrollback.
	if live := m.View(); !strings.Contains(live, focusedMarkLeft+"Work") {
		t.Fatalf("View() = %q, want the focused button %q marked with %q; without the mark, the assertion below cannot fail", live, "Work", focusedMarkLeft)
	}

	block := m.commitLive()

	for _, label := range []string{"Family", "Work"} {
		if !strings.Contains(block, label) {
			t.Errorf("the committed block %q is missing button %q: a committed reply keeps its buttons as inert text (chat-tui#req:scrollback-commit)", block, label)
		}
	}
	if strings.Contains(block, focusedMarkLeft) {
		t.Errorf("the committed block %q marks a button as focused; committed buttons are inert text (chat-tui#req:focus-and-keys)", block)
	}
	if m.live != nil {
		t.Errorf("live = %+v after committing it, want nil: nothing in scrollback is focusable", m.live)
	}
	if m.focus != focusInput {
		t.Errorf("focus = %v after the live reply committed, want focusInput: the button block it pointed into no longer exists", m.focus)
	}
	if m.row != 0 || m.col != 0 {
		t.Errorf("button cursor = (%d,%d) after the live reply committed, want (0,0)", m.row, m.col)
	}
}

// TestLiveRegionMarksTheFocusedButton is the control for the inertness assertion
// above: it proves the focus mark is rendered when a button really is focused,
// so "the committed block carries no mark" is a claim that can fail. Without it,
// dropping the mark entirely would satisfy that assertion.
func TestLiveRegionMarksTheFocusedButton(t *testing.T) {
	proc := fakeProcessor{replies: []chat.Reply{{Text: "Your spaces:", Keyboard: spacesKeyboard()}}}
	m, _ := submitTurn(t, New(proc), "/spaces")
	m.focus, m.row, m.col = focusButtons, 1, 0

	if view := m.View(); !strings.Contains(view, focusedMarkLeft+"Work") {
		t.Fatalf("View() = %q, want the focused button %q marked with %q", view, "Work", focusedMarkLeft)
	}
}

// TestReplyWithNoKeyboardCommitsImmediately pins the other branch of the rule:
// with no buttons there is nothing left to focus, so the reply is complete on
// arrival and goes straight to scrollback (chat-tui#req:scrollback-commit).
func TestReplyWithNoKeyboardCommitsImmediately(t *testing.T) {
	const help = "Sneat CLI chat commands: /spaces"
	m, sb := submitTurn(t, New(fakeProcessor{replies: []chat.Reply{{Text: help}}}), "/help")

	if m.live != nil {
		t.Errorf("live = %+v after a reply with no keyboard, want nil: it has nothing left to focus", m.live)
	}
	if len(sb) != 2 {
		t.Fatalf("the turn committed %q, want two blocks: the echo then the reply", sb)
	}
	if !strings.Contains(sb[0], "/help") {
		t.Errorf("the turn committed %q first, want the submitted line", sb[0])
	}
	if !strings.Contains(sb[1], help) {
		t.Errorf("the turn committed %q second, want the reply %q", sb[1], help)
	}
	if view := m.View(); strings.Contains(view, help) {
		t.Errorf("View() = %q still draws a committed reply; the live region draws only what can still change (chat-tui#req:inline-rendering)", view)
	}
}

// TestCommitRule pins which of a turn's replies commit and which one stays live,
// across the shapes a turn can answer with (chat-tui#req:scrollback-commit).
func TestCommitRule(t *testing.T) {
	buttoned := func(text string) chat.Reply { return chat.Reply{Text: text, Keyboard: spacesKeyboard()} }
	plain := func(text string) chat.Reply { return chat.Reply{Text: text} }

	tests := []struct {
		name string
		// replies is what the Processor answers the turn with.
		replies []chat.Reply
		// wantCommitted is the reply texts expected in scrollback after the
		// echo, in order.
		wantCommitted []string
		// wantLive is the text of the reply expected to stay live, or "" for
		// none.
		wantLive string
	}{
		{
			name: "a turn that answers with nothing commits nothing",
		},
		{
			name:          "a reply with no keyboard commits",
			replies:       []chat.Reply{plain("Hi!")},
			wantCommitted: []string{"Hi!"},
		},
		{
			name:     "a trailing reply carrying a keyboard stays live",
			replies:  []chat.Reply{buttoned("Your spaces:")},
			wantLive: "Your spaces:",
		},
		{
			name:          "earlier replies commit around a trailing live one",
			replies:       []chat.Reply{plain("Done."), buttoned("What next?")},
			wantCommitted: []string{"Done."},
			wantLive:      "What next?",
		},
		{
			// Only the most recent message's buttons are focusable, so a
			// keyboard on any earlier reply of the same turn is superseded
			// before it is ever drawn: it commits inert.
			name:          "a keyboard on a non-trailing reply commits with it",
			replies:       []chat.Reply{buttoned("Pick a space:"), plain("Loading…")},
			wantCommitted: []string{"Pick a space:", "Loading…"},
		},
		{
			// Nothing to focus means nothing to wait for.
			name:          "a keyboard carrying no buttons does not make a reply live",
			replies:       []chat.Reply{{Text: "Hi!", Keyboard: botkb.NewMessageKeyboard(botkb.KeyboardTypeInline)}},
			wantCommitted: []string{"Hi!"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, sb := submitTurn(t, New(fakeProcessor{replies: tt.replies}), "go")
			if len(sb) == 0 || !strings.Contains(sb[0], "go") {
				t.Fatalf("the turn committed %q, want the submitted line first", sb)
			}
			body := strings.Join(sb[1:], "\n")

			at := 0
			for _, want := range tt.wantCommitted {
				i := strings.Index(body[at:], want)
				if i < 0 {
					t.Fatalf("scrollback after the echo = %q, want %q committed after the replies before it", body, want)
				}
				at += i + len(want)
			}
			if tt.wantLive == "" {
				if m.live != nil {
					t.Errorf("live = %+v, want nil", m.live)
				}
				return
			}
			if m.live == nil {
				t.Fatalf("live = nil, want the reply %q to stay focusable", tt.wantLive)
			}
			if m.live.Text != tt.wantLive {
				t.Errorf("live.Text = %q, want %q", m.live.Text, tt.wantLive)
			}
			if strings.Contains(body, tt.wantLive) {
				t.Errorf("the live reply %q was also committed to scrollback %q", tt.wantLive, body)
			}
		})
	}
}

// --- the live region (REQ: inline-rendering) ---

// TestViewIsOnlyTheLiveRegion pins that View draws nothing but what can still
// change. With no live reply that is the input line and the footer hint — never
// a transcript, which would fight the terminal for text it already owns.
func TestViewIsOnlyTheLiveRegion(t *testing.T) {
	view := New(fakeProcessor{}).View()
	if lines := strings.Split(view, "\n"); len(lines) != 2 {
		t.Errorf("View() = %q (%d lines), want 2: the input line and the footer hint", view, len(lines))
	}
	if !strings.Contains(view, footerHelp) {
		t.Errorf("View() = %q, missing the footer hint %q", view, footerHelp)
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
