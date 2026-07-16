package chattui

import (
	"context"
	"errors"
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
// It records what it was asked in sent and pressed, when a test supplies
// somewhere to record: that a turn reached the Processor at all — and from a
// command rather than from Update — is otherwise not observable from the model.
type fakeProcessor struct {
	replies []chat.Reply
	err     error
	sent    *[]string
	pressed *[]string
}

func (f fakeProcessor) SendText(_ context.Context, text string) ([]chat.Reply, error) {
	if f.sent != nil {
		*f.sent = append(*f.sent, text)
	}
	return f.replies, f.err
}

func (f fakeProcessor) PressButton(_ context.Context, data string) ([]chat.Reply, error) {
	if f.pressed != nil {
		*f.pressed = append(*f.pressed, data)
	}
	return f.replies, f.err
}

// spacesKeyboard is a two-row keyboard standing in for what a `/spaces` turn
// answers with: one button per row, carrying the `space?id=<spaceID>` callback
// data internal/chat's processor really emits. Borrowing the real string rather
// than inventing one keeps the press assertions below pinned to what a press in
// a running session would actually carry to the Processor.
func spacesKeyboard() botkb.Keyboard {
	return botkb.NewMessageKeyboard(botkb.KeyboardTypeInline,
		[]botkb.Button{botkb.NewDataButton("Family", "space?id=family1")},
		[]botkb.Button{botkb.NewDataButton("Work", "space?id=work1")},
	)
}

// raggedKeyboard is a keyboard whose rows are not all the same width: a
// two-button row, a one-button row, then another two-button row.
//
// No shipped command emits a multi-button row today — `/spaces` emits one button
// per row — so `left`/`right`, and the column clamping that moving between rows
// of different widths needs, have no production case yet. They are still keys
// REQ: focus-and-keys names, and rows of buttons of any width are what the botkb
// keyboard vocabulary is, so this is the shape a command will eventually emit.
// Until one does, it is how those keys are driven.
func raggedKeyboard() botkb.Keyboard {
	return botkb.NewMessageKeyboard(botkb.KeyboardTypeInline,
		[]botkb.Button{botkb.NewDataButton("Yes", "confirm?ok=1"), botkb.NewDataButton("No", "confirm?ok=0")},
		[]botkb.Button{botkb.NewDataButton("Explain", "confirm?explain=1")},
		[]botkb.Button{botkb.NewDataButton("A", "pick?v=a"), botkb.NewDataButton("B", "pick?v=b")},
	)
}

// liveModel builds a model sitting on a reply whose buttons are still focusable,
// with focus on the input: the state a turn answered with buttons leaves behind,
// which TestButtonedReplyBecomesLiveAndRendersInTheLiveRegion pins against a real
// turn.
//
// It is built directly rather than driven through one so a test can choose the
// keyboard's shape — no command emits a multi-button row yet — and so a key
// assertion is not also an assertion about the turn that got there.
func liveModel(proc chat.Processor, kb botkb.Keyboard) Model {
	m := New(proc)
	reply := chat.Reply{Text: "Your spaces:", Keyboard: kb}
	m.live = &reply
	return m
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

// sequenceIn returns the commands the tea.Sequence bundled into cmd runs, in
// order, and whether cmd dispatches one at all.
//
// It exists because a command does not always reach the event loop bare. A press
// commits the live reply, which returns focus to the input, and syncInputCursor
// batches the input's own cursor command alongside whatever the key handler
// returned — so the sequence a press makes arrives as one element of a tea.Batch
// rather than as the command itself. Only the bundle is walked, never the
// sequence's own elements: those are the commit and the turn, and running them
// here would be running the turn twice.
//
// TestSequenceIsFoundThroughABatch is its control.
func sequenceIn(t *testing.T, cmd tea.Cmd) ([]tea.Cmd, bool) {
	t.Helper()
	if cmd == nil {
		return nil, false
	}
	msg := cmd()
	if cmds, ok := sequencedCmds(msg); ok {
		return cmds, true
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return nil, false
	}
	for _, sub := range batch {
		if cmds, ok := sequenceIn(t, sub); ok {
			return cmds, true
		}
	}
	return nil, false
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

// pump runs cmd the way the event loop would: what it prints is collected, and
// what it does not — the Processor's answer to the turn, arriving as a message —
// goes back through Update, whose own commands are pumped in turn. It returns the
// model those messages left behind and everything they produced, in order.
func pump(t *testing.T, m Model, cmd tea.Cmd) (Model, []tea.Msg) {
	t.Helper()
	var out []tea.Msg
	for _, msg := range drain(t, cmd) {
		if _, ok := printedBody(msg); ok {
			out = append(out, msg)
			continue
		}
		next, answer := m.Update(msg)
		m = next.(Model)
		out = append(out, drain(t, answer)...)
	}
	return m, out
}

// turnMsgs drives one whole turn — it types text, presses enter, and pumps the
// Processor's answer back through the model — and returns the model the turn left
// behind together with every message the turn produced, in order.
//
// It returns the messages rather than only what they printed, so a caller can
// also see what else the turn asked the event loop to do: a quit, in particular,
// is a message like any other and is invisible to scrollbackOf.
func turnMsgs(t *testing.T, m Model, text string) (Model, []tea.Msg) {
	t.Helper()
	m.input.SetValue(text)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return pump(t, next.(Model), cmd)
}

// submitTurn drives one whole turn and returns the model it left behind, and
// everything the turn committed to scrollback, in order.
func submitTurn(t *testing.T, m Model, text string) (Model, []string) {
	t.Helper()
	m, msgs := turnMsgs(t, m, text)
	return m, scrollbackOf(msgs)
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

// TestSequenceIsFoundThroughABatch is the control for sequenceIn, and it has to
// fail in both directions to be worth anything: it proves a tea.Sequence is found
// through the tea.Batch a press's command arrives wrapped in, and that none is
// found where none was made. Without the second half, "the press sequences its
// commit ahead of the turn" would pass on a press that batched the two — which is
// the exact race the sequence exists to rule out.
func TestSequenceIsFoundThroughABatch(t *testing.T) {
	cmds, ok := sequenceIn(t, tea.Batch(tea.Println("noise"), tea.Sequence(tea.Println("first"), tea.Println("second"))))
	if !ok {
		t.Fatal("sequenceIn did not find a tea.Sequence bundled into a tea.Batch: the press ordering assertion can no longer see the sequence a press makes")
	}
	got := scrollbackOf(drain(t, tea.Sequence(cmds...)))
	want := []string{"first", "second"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sequenceIn returned commands printing %q, want %q", got, want)
	}
	if _, ok := sequenceIn(t, tea.Batch(tea.Println("first"), tea.Println("second"))); ok {
		t.Error("sequenceIn found a tea.Sequence in a tea.Batch that carries none: the press ordering assertion would pass on a press that raced its commit against the turn")
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

// --- committing a pressed turn (REQ: scrollback-commit) ---
//
// Pressing a button is user input exactly as submitting text is, so it commits
// the live reply and then an echo of what the user did, before the replies it
// prompts render. It is the half of the rule text submission cannot exercise: a
// press is the only input that arrives *from* the live reply's own button block,
// so it is the only one that has to end that block's focusability to happen at
// all.

// pressTurn drives one whole press turn — it walks focus onto a button with
// keys, presses enter, and pumps the Processor's answer back through the model —
// and returns the model the turn left behind together with every message it
// produced, in order. It is the press counterpart of turnMsgs.
func pressTurn(t *testing.T, m Model, keys ...string) (Model, []tea.Msg) {
	t.Helper()
	m, _ = pressKeys(t, m, keys...)
	if m.focus != focusButtons {
		t.Fatalf("setup: %v did not put focus in the button block, so enter would not press a button", keys)
	}
	next, cmd := m.Update(key(t, "enter"))
	return pump(t, next.(Model), cmd)
}

// TestPressCommitsTheLiveReplyThenEchoesTheLabel drives the first half of
// _tests/press-commits-live-reply.md — the `/spaces` → press-a-space flow end to
// end (chat-tui#req:scrollback-commit).
//
// A press is user input, so the reply whose button it pressed commits first, with
// its buttons inert: pressing one is precisely the moment they stop being
// focusable. The echo of the press follows it, and only then do the replies the
// press prompted render.
func TestPressCommitsTheLiveReplyThenEchoesTheLabel(t *testing.T) {
	const answer = "Family space: 3 members"
	var pressed []string
	m := liveModel(fakeProcessor{replies: []chat.Reply{{Text: answer}}, pressed: &pressed}, spacesKeyboard())

	m, _ = pressKeys(t, m, "down")
	// The live region marks the pressed button: that is what the commit below
	// must not carry into scrollback, and without the mark that assertion could
	// not fail.
	if got := focusedLabel(m); got != "Family" {
		t.Fatalf("setup: the live region marks %q as focused, want %q", got, "Family")
	}

	next, cmd := m.Update(key(t, "enter"))
	m, msgs := pump(t, next.(Model), cmd)

	sb := scrollbackOf(msgs)
	if len(sb) != 2 {
		t.Fatalf("a press committed %q, want two blocks: the pressed reply with the echo of the press, then the reply the press prompted (chat-tui#req:scrollback-commit)", sb)
	}
	block := sb[0]
	iReply := strings.Index(block, "Your spaces:")
	iEcho := strings.Index(block, inputPrompt+"Family")
	switch {
	case iReply < 0:
		t.Errorf("the reply whose button was pressed is missing from the committed block %q; a press commits the live reply (chat-tui#req:scrollback-commit)", block)
	case iEcho < 0:
		t.Errorf("the committed block %q carries no echo of the press naming the pressed button %q; the transcript records what the user did (chat-tui#req:scrollback-commit)", block, "Family")
	case iReply > iEcho:
		t.Errorf("the committed block %q echoes the press before the reply it supersedes; the live reply commits first (chat-tui#req:scrollback-commit)", block)
	}
	if strings.Contains(block, "space?id=family1") {
		t.Errorf("the committed block %q names the pressed button's callback data; the echo names the label the user pressed, which is what they saw", block)
	}
	if !strings.Contains(block, "Work") {
		t.Errorf("the committed block %q is missing button %q: a committed reply keeps its buttons as inert text (chat-tui#req:scrollback-commit)", block, "Work")
	}
	if strings.Contains(block, focusedMarkLeft) {
		t.Errorf("the committed block %q marks a button as focused; a press commits the reply with its buttons rendered inert (chat-tui#req:scrollback-commit)", block)
	}
	if !strings.Contains(sb[1], answer) {
		t.Errorf("a press committed %q after the press, want the reply it prompted, %q: the replies render after the echo (chat-tui#req:scrollback-commit)", sb[1], answer)
	}
	if !reflect.DeepEqual(pressed, []string{"space?id=family1"}) {
		t.Errorf("PressButton received %q, want exactly one call, carrying [%q]", pressed, "space?id=family1")
	}
	if view := m.View(); strings.Contains(view, "Your spaces:") {
		t.Errorf("View() = %q still draws the reply whose button was pressed; no live reply from before the press remains focusable (chat-tui#req:scrollback-commit)", view)
	}
}

// TestPressSequencesTheCommitAheadOfThePress pins how a press's ordering is
// guaranteed rather than merely likely, for the same reason
// TestSubmitSequencesTheCommitAheadOfTheSend does it for a submit: tea.Batch
// would run the commit and the turn concurrently, so a Processor that answered
// fast enough could land its reply in scrollback ahead of the press that prompted
// it (chat-tui#req:scrollback-commit).
func TestPressSequencesTheCommitAheadOfThePress(t *testing.T) {
	m, _ := pressKeys(t, liveModel(fakeProcessor{replies: []chat.Reply{{Text: "Family space:"}}}, spacesKeyboard()), "down")
	if m.focus != focusButtons {
		t.Fatal("setup: expected focus in the button block after down")
	}

	_, cmd := m.Update(key(t, "enter"))

	cmds, ok := sequenceIn(t, cmd)
	if !ok {
		t.Fatal("a press dispatched no tea.Sequence: the commit must be ordered ahead of the press, not raced against it (chat-tui#req:scrollback-commit)")
	}
	if len(cmds) != 2 {
		t.Fatalf("a press sequenced %d commands, want 2: the commit then the press", len(cmds))
	}
	if _, ok := printedBody(cmds[0]()); !ok {
		t.Error("the first command a press sequences does not commit to scrollback; the commit must come first")
	}
}

// TestAPressLeavesNothingFocusableFromBeforeIt pins the rest of
// _tests/press-commits-live-reply.md, across everything a press can be answered
// with: a press always ends the previous reply's focusability, so no live reply is
// ever stranded (chat-tui#req:scrollback-commit).
//
// The first case is the scenario's second half — a press answered with a reply
// carrying no keyboard leaves nothing live and focus on the input. The rest are
// the shapes that would otherwise be the ways to strand one.
func TestAPressLeavesNothingFocusableFromBeforeIt(t *testing.T) {
	const answer = "Family space: 3 members"

	tests := []struct {
		name string
		// replies is what PressButton answers the press with.
		replies []chat.Reply
		// wantLive is the text of the reply expected to stay live after the
		// press, or "" for none.
		wantLive string
	}{
		{
			name:    "a reply carrying no keyboard commits, leaving nothing live",
			replies: []chat.Reply{{Text: answer}},
		},
		{
			// The pressed reply still commits; the new one takes the live slot,
			// so what is focusable is never what the user already pressed.
			name:     "a reply carrying a keyboard becomes the live one",
			replies:  []chat.Reply{{Text: "Family space:", Keyboard: raggedKeyboard()}},
			wantLive: "Family space:",
		},
		{
			// A press answered with nothing must still not leave the reply it
			// pressed live: the press already ended its focusability.
			name: "a press answered with nothing leaves nothing live",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, msgs := pressTurn(t, liveModel(fakeProcessor{replies: tt.replies}, spacesKeyboard()), "down")

			if sb := scrollbackOf(msgs); len(sb) == 0 || !strings.Contains(sb[0], "Your spaces:") {
				t.Fatalf("a press committed %q, want the reply it superseded first (chat-tui#req:scrollback-commit)", sb)
			}
			if m.focus != focusInput {
				t.Errorf("focus = %v after a press, want focusInput: the button block it pressed from no longer exists (chat-tui#req:focus-and-keys)", m.focus)
			}
			if view := m.View(); strings.Contains(view, "Your spaces:") {
				t.Errorf("View() = %q still draws the reply whose button was pressed; a press always ends the previous reply's focusability (chat-tui#req:scrollback-commit)", view)
			}
			if tt.wantLive == "" {
				if m.live != nil {
					t.Errorf("live = %+v after a press, want nil: nothing the press was answered with is focusable", m.live)
				}
				return
			}
			if m.live == nil {
				t.Fatalf("live = nil, want the reply %q to stay focusable", tt.wantLive)
			}
			if m.live.Text != tt.wantLive {
				t.Errorf("live.Text = %q, want %q", m.live.Text, tt.wantLive)
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
	if !strings.Contains(view, footerHelpInput) {
		t.Errorf("View() = %q, missing the footer hint %q", view, footerHelpInput)
	}
}

// TestFooterNamesTheKeysThatWorkRightNow pins the hint line to the state it
// describes (chat-tui#req:inline-rendering). enter and esc each mean one thing
// on the input and another in the button block, and while a reply is in flight
// only ctrl+c means anything at all, so no one fixed line can name the keys
// without also naming meanings that do not apply — which is the ambiguity the
// hint exists to remove (chat-tui#req:focus-and-keys,
// chat-tui#req:input-locked-while-pending).
func TestFooterNamesTheKeysThatWorkRightNow(t *testing.T) {
	// Asserting the hints this state must not carry is what makes each case
	// fail on a hint that is merely a superset of the right one.
	all := []string{footerHelpInput, footerHelpInputLive, footerHelpButtons, footerHelpPending}

	tests := []struct {
		name  string
		model func(t *testing.T) Model
		want  string
	}{
		{
			name:  "focus on the input, with nothing focusable to offer",
			model: func(*testing.T) Model { return New(fakeProcessor{}) },
			want:  footerHelpInput,
		},
		{
			name:  "focus on the input, with a button block to enter",
			model: func(*testing.T) Model { return liveModel(fakeProcessor{}, spacesKeyboard()) },
			want:  footerHelpInputLive,
		},
		{
			name: "focus in the button block",
			model: func(t *testing.T) Model {
				m, _ := pressKeys(t, liveModel(fakeProcessor{}, spacesKeyboard()), "down")
				return m
			},
			want: footerHelpButtons,
		},
		{
			name: "a reply in flight",
			model: func(*testing.T) Model {
				m := liveModel(fakeProcessor{}, spacesKeyboard())
				m.pending = true
				return m
			},
			want: footerHelpPending,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := tt.model(t).View()
			if !strings.Contains(view, tt.want) {
				t.Errorf("View() = %q, want the footer hint %q", view, tt.want)
			}
			for _, other := range all {
				if other == tt.want {
					continue
				}
				if strings.Contains(view, other) {
					t.Errorf("View() = %q carries the hint %q, which names keys that do not mean that here", view, other)
				}
			}
		})
	}
}

// --- model ---

// TestNewStartsOnTheInputWithNothingLive pins the state a session opens in:
// focus on the input, no live reply to focus buttons in, and no reply in
// flight. Task 4's focus movement and pending lock are written against it.
// TestPlaceholderRendersInFull guards a bubbles trap that no model-state
// assertion can see. textinput.placeholderView does
// `p := make([]rune, m.Width+1); copy(p, []rune(m.Placeholder))` — so with the
// zero-value Width it allocates a ONE-rune buffer and silently truncates the
// placeholder to its first character. The session rendered "> T" for every
// frame until a real terminal showed it. The whole placeholder must survive,
// at the initial width and after a resize.
func TestPlaceholderRendersInFull(t *testing.T) {
	const want = "Type a message"

	m := New(fakeProcessor{})
	if got := m.View(); !strings.Contains(got, want) {
		t.Errorf("initial View() does not render the whole placeholder\n got: %q\nwant it to contain: %q", got, want)
	}

	rm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	if got := rm.(Model).View(); !strings.Contains(got, want) {
		t.Errorf("View() after resize does not render the whole placeholder\n got: %q\nwant it to contain: %q", got, want)
	}
}

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

// --- keys and focus (REQ: focus-and-keys, REQ: input-locked-while-pending) ---

// keyMsgs are the KeyMsg values for the keys the requirement names, plus a few
// it does not, which the model must therefore have no meaning for.
//
// Driving Update with the message the event loop itself would deliver is what
// makes the tests below exercise the real key table rather than a paraphrase of
// it.
var keyMsgs = map[string]tea.KeyMsg{
	"ctrl+c": {Type: tea.KeyCtrlC},
	"enter":  {Type: tea.KeyEnter},
	"esc":    {Type: tea.KeyEsc},
	"up":     {Type: tea.KeyUp},
	"down":   {Type: tea.KeyDown},
	"left":   {Type: tea.KeyLeft},
	"right":  {Type: tea.KeyRight},
	"tab":    {Type: tea.KeyTab},
	"x":      {Type: tea.KeyRunes, Runes: []rune("x")},
}

// key returns the KeyMsg for the key the requirement calls name.
//
// It checks that name against the name bubbletea gives the message, which is the
// name the model matches on. That check is this helper's own control: without it
// a key these tests call "esc" could quietly stop being the key the model sees
// under that name, and every assertion below would go on passing while pressing
// something else — or nothing at all.
func key(t *testing.T, name string) tea.KeyMsg {
	t.Helper()
	msg, ok := keyMsgs[name]
	if !ok {
		t.Fatalf("no KeyMsg for the key %q", name)
	}
	if got := msg.String(); got != name {
		t.Fatalf("the KeyMsg built for %q reads as %q: bubbletea renamed the key, and a test naming %q no longer presses what the model matches on", name, got, name)
	}
	return msg
}

// pressKeys drives keys through the model in order, returning the model they
// left behind and the command the last of them produced.
func pressKeys(t *testing.T, m Model, names ...string) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, name := range names {
		var next tea.Model
		next, cmd = m.Update(key(t, name))
		m = next.(Model)
	}
	return m, cmd
}

// quitsIn reports whether msgs carry the quit.
func quitsIn(msgs []tea.Msg) bool {
	for _, msg := range msgs {
		if _, ok := msg.(tea.QuitMsg); ok {
			return true
		}
	}
	return false
}

// quits reports whether cmd quits the program.
//
// tea.Quit is a tea.Cmd — a function — so whether a key quits cannot be read off
// the command Update returns: it is readable only off the message the command
// produces, which is what the event loop dispatches. drain runs it exactly as the
// loop would, so a quit bundled into a tea.Sequence or tea.Batch counts too, and
// a nil command produces nothing and quits nothing.
//
// TestQuitIsDetectedAndNotImagined is its control.
func quits(t *testing.T, cmd tea.Cmd) bool {
	t.Helper()
	return quitsIn(drain(t, cmd))
}

// TestQuitIsDetectedAndNotImagined is the control for quits: it proves quits sees
// a real tea.Quit, wherever it is bundled, and does not see one where there is
// none. Without it, "the program quits" and "the program does not quit" could
// both be assertions that pass no matter what the model returns.
func TestQuitIsDetectedAndNotImagined(t *testing.T) {
	if !quits(t, tea.Quit) {
		t.Error("quits did not recognise tea.Quit: every \"the program quits\" assertion below now fails regardless of the model")
	}
	if !quits(t, tea.Sequence(tea.Println("noise"), tea.Quit)) {
		t.Error("quits did not find a tea.Quit inside a tea.Sequence: a quit could hide from every \"does not quit\" assertion below")
	}
	if quits(t, nil) {
		t.Error("quits reported that a nil command quits the program")
	}
	if quits(t, tea.Println("noise")) {
		t.Error("quits reported that a command which only prints quits the program")
	}
}

// focusedLabel returns the label of the button the live region marks as focused,
// or "" when it marks none.
//
// It reads the mark out of View rather than the cursor out of the model, so a
// cursor that moves without the region following it — or one left pointing at no
// button at all — is not mistaken for focus having moved.
func focusedLabel(m Model) string {
	view := m.View()
	i := strings.Index(view, focusedMarkLeft)
	if i < 0 {
		return ""
	}
	rest := view[i+len(focusedMarkLeft):]
	j := strings.Index(rest, focusedMarkRight)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// TestFocusMovement pins what each key that moves focus means, from each place
// focus can be (chat-tui#req:focus-and-keys). Every case starts on the input,
// which is where a session and every turn leave focus, and presses only real
// keys to get where it is going.
func TestFocusMovement(t *testing.T) {
	tests := []struct {
		name string
		// kb is the live reply's keyboard: spacesKeyboard is one button per row,
		// raggedKeyboard has rows of two, one and two.
		kb botkb.Keyboard
		// keys are pressed in order, from focus on the input.
		keys []string
		// wantFocus is where focus must end up.
		wantFocus focusTarget
		// wantLabel is the button the live region must mark, or "" when focus is
		// not in the block and so nothing in it is focused.
		wantLabel string
	}{
		{
			name:      "down from the input enters the block at the first row",
			kb:        spacesKeyboard(),
			keys:      []string{"down"},
			wantFocus: focusButtons,
			wantLabel: "Family",
		},
		{
			name:      "up from the first row returns to the input",
			kb:        spacesKeyboard(),
			keys:      []string{"down", "up"},
			wantFocus: focusInput,
		},
		{
			name:      "down moves on to the next row",
			kb:        spacesKeyboard(),
			keys:      []string{"down", "down"},
			wantFocus: focusButtons,
			wantLabel: "Work",
		},
		{
			// Only up *past* the first row leaves: from any other row it is a
			// move within the block.
			name:      "up moves back a row before it reaches the input",
			kb:        spacesKeyboard(),
			keys:      []string{"down", "down", "up"},
			wantFocus: focusButtons,
			wantLabel: "Family",
		},
		{
			// down leaves the input for the block and moves down it; it never
			// leaves the block, or the key would mean three things.
			name:      "down stops at the last row",
			kb:        spacesKeyboard(),
			keys:      []string{"down", "down", "down", "down"},
			wantFocus: focusButtons,
			wantLabel: "Work",
		},
		{
			name:      "esc leaves the block for the input",
			kb:        spacesKeyboard(),
			keys:      []string{"down", "down", "esc"},
			wantFocus: focusInput,
		},
		{
			name:      "down re-enters the block at the first row, not where it left",
			kb:        spacesKeyboard(),
			keys:      []string{"down", "down", "esc", "down"},
			wantFocus: focusButtons,
			wantLabel: "Family",
		},
		{
			name:      "right moves along a row",
			kb:        raggedKeyboard(),
			keys:      []string{"down", "right"},
			wantFocus: focusButtons,
			wantLabel: "No",
		},
		{
			// No wrap: wrapping would make right mean "go to the other end" as
			// well as "go one to the right".
			name:      "right stops at the end of a row",
			kb:        raggedKeyboard(),
			keys:      []string{"down", "right", "right"},
			wantFocus: focusButtons,
			wantLabel: "No",
		},
		{
			name:      "left moves back along a row",
			kb:        raggedKeyboard(),
			keys:      []string{"down", "right", "left"},
			wantFocus: focusButtons,
			wantLabel: "Yes",
		},
		{
			name:      "left stops at the start of a row",
			kb:        raggedKeyboard(),
			keys:      []string{"down", "left"},
			wantFocus: focusButtons,
			wantLabel: "Yes",
		},
		{
			// Rows need not be the same width, so a row change can leave the
			// column past the end of the row it lands in: a cursor on no button,
			// which enter could not press.
			name:      "down onto a narrower row keeps the cursor on a button",
			kb:        raggedKeyboard(),
			keys:      []string{"down", "right", "down"},
			wantFocus: focusButtons,
			wantLabel: "Explain",
		},
		{
			name:      "up onto a narrower row keeps the cursor on a button",
			kb:        raggedKeyboard(),
			keys:      []string{"down", "down", "down", "right", "up"},
			wantFocus: focusButtons,
			wantLabel: "Explain",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, cmd := pressKeys(t, liveModel(fakeProcessor{}, tt.kb), tt.keys...)
			if quits(t, cmd) {
				t.Errorf("%v quit the program; only esc from the input and ctrl+c do (chat-tui#req:focus-and-keys)", tt.keys)
			}
			if m.focus != tt.wantFocus {
				t.Errorf("focus = %v after %v, want %v", m.focus, tt.keys, tt.wantFocus)
			}
			if got := focusedLabel(m); got != tt.wantLabel {
				t.Errorf("the live region marks %q as focused after %v, want %q", got, tt.keys, tt.wantLabel)
			}
		})
	}
}

// TestDownEntersTheButtonBlockAtTheFirstRow pins where down puts focus, and not
// merely that it moves it: entry is at row 0
// (chat-tui#req:focus-and-keys).
//
// The cursor is planted in the block first, which no sequence of keys can do
// while focus is on the input — every path that leaves the block clears the
// cursor behind it, and TestFocusMovement's "down re-enters the block at the
// first row" drives that path with real keys. This builds the state directly
// because it is the one that reset exists to make not matter: without it, down
// would enter wherever focus had been last, and the assertion would be on
// nothing.
func TestDownEntersTheButtonBlockAtTheFirstRow(t *testing.T) {
	m := liveModel(fakeProcessor{}, spacesKeyboard())
	m.row, m.col = 1, 0

	m, _ = pressKeys(t, m, "down")

	if m.focus != focusButtons {
		t.Fatalf("focus = %v after down from the input, want focusButtons", m.focus)
	}
	if got := focusedLabel(m); got != "Family" {
		t.Errorf("down entered the button block on %q, want the first button %q: it enters at row 0 (chat-tui#req:focus-and-keys)", got, "Family")
	}
}

// TestDownWithNothingFocusableStaysOnTheInput pins the one thing that stops down
// from being unconditional: focus is the button block only while there is a block
// to point into (chat-tui#req:focus-and-keys).
func TestDownWithNothingFocusableStaysOnTheInput(t *testing.T) {
	m, cmd := pressKeys(t, New(fakeProcessor{}), "down")
	if m.focus != focusInput {
		t.Errorf("focus = %v after down with no live reply, want focusInput: there is no button block to enter", m.focus)
	}
	if quits(t, cmd) {
		t.Error("down quit the program")
	}
}

// TestKeysWithNoMeaningInTheButtonBlockDoNotReachTheInput pins the block half of
// "exactly one target holds focus": the input does not hold it, so it does not
// take keys — typing while a button is focused must not edit the line
// (chat-tui#ac:interaction-is-unambiguous).
func TestKeysWithNoMeaningInTheButtonBlockDoNotReachTheInput(t *testing.T) {
	for _, name := range []string{"x", "tab"} {
		t.Run(name, func(t *testing.T) {
			m := liveModel(fakeProcessor{}, spacesKeyboard())
			m.input.SetValue("hello")
			m, _ = pressKeys(t, m, "down")
			if m.focus != focusButtons {
				t.Fatal("setup: expected focus in the button block after down")
			}

			m, cmd := pressKeys(t, m, name)

			if quits(t, cmd) {
				t.Errorf("%q quit the program from the button block", name)
			}
			if m.focus != focusButtons {
				t.Errorf("focus = %v after %q in the button block, want it left there", m.focus, name)
			}
			if m.input.Value() != "hello" {
				t.Errorf("input = %q after %q in the button block, want %q: the input does not hold focus, so it does not take keys", m.input.Value(), name, "hello")
			}
		})
	}
}

// TestInputCursorFollowsFocus pins that the region shows exactly one focused
// target (chat-tui#ac:interaction-is-unambiguous). The input draws a cursor while
// it is focused, so leaving it focused while the button block holds focus would
// put two focus indicators on screen at once.
func TestInputCursorFollowsFocus(t *testing.T) {
	m := liveModel(fakeProcessor{}, spacesKeyboard())
	if !m.input.Focused() {
		t.Fatal("setup: the input starts focused, and draws a cursor to say so")
	}

	m, _ = pressKeys(t, m, "down")
	if m.input.Focused() {
		t.Error("the input still draws its cursor while the button block holds focus; exactly one target holds focus at a time (chat-tui#ac:interaction-is-unambiguous)")
	}

	m, _ = pressKeys(t, m, "esc")
	if !m.input.Focused() {
		t.Error("the input does not draw its cursor after focus returned to it")
	}
}

// TestEscFromTheInputQuits and TestEscFromTheButtonBlockReturnsFocusAndDoesNotQuit
// are a pair, and only work as one. esc means two different things, one per focus
// (chat-tui#req:focus-and-keys); a model that quit from both, or from neither,
// would fail one of them.
func TestEscFromTheInputQuits(t *testing.T) {
	m := liveModel(fakeProcessor{}, spacesKeyboard())
	if m.focus != focusInput {
		t.Fatal("setup: expected focus on the input")
	}

	_, cmd := m.Update(key(t, "esc"))

	if !quits(t, cmd) {
		t.Error("esc from the input did not quit the program (chat-tui#req:focus-and-keys)")
	}
}

func TestEscFromTheButtonBlockReturnsFocusAndDoesNotQuit(t *testing.T) {
	m, _ := pressKeys(t, liveModel(fakeProcessor{}, spacesKeyboard()), "down", "down")
	if m.focus != focusButtons {
		t.Fatal("setup: expected focus in the button block")
	}

	next, cmd := m.Update(key(t, "esc"))
	m = next.(Model)

	if quits(t, cmd) {
		t.Error("esc from the button block quit the program; from there it returns focus to the input, and only the esc after that quits (chat-tui#req:focus-and-keys)")
	}
	if m.focus != focusInput {
		t.Errorf("focus = %v after esc from the button block, want focusInput", m.focus)
	}
}

// TestCtrlCAlwaysQuits pins the one key with a single meaning everywhere: it
// quits from either focus, and even while a reply is in flight, where every other
// key is ignored (chat-tui#req:focus-and-keys,
// chat-tui#req:input-locked-while-pending).
//
// The in-flight case is where this diverges from the guard it mirrors:
// internal/tui's confirm screen ignores every key while its delete runs, ctrl+c
// included. That is safe for a screen resolving one round trip; a chat session
// may block on a slow backend for as long as it likes, so a guard copied from
// there verbatim would trap the user with no way out.
func TestCtrlCAlwaysQuits(t *testing.T) {
	tests := []struct {
		name  string
		model func(t *testing.T) Model
	}{
		{
			name:  "from the input",
			model: func(*testing.T) Model { return New(fakeProcessor{}) },
		},
		{
			name: "from the button block",
			model: func(t *testing.T) Model {
				m, _ := pressKeys(t, liveModel(fakeProcessor{}, spacesKeyboard()), "down")
				if m.focus != focusButtons {
					t.Fatal("setup: expected focus in the button block after down")
				}
				return m
			},
		},
		{
			name: "while a reply is in flight",
			model: func(*testing.T) Model {
				m := liveModel(fakeProcessor{}, spacesKeyboard())
				m.pending = true
				return m
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, cmd := tt.model(t).Update(key(t, "ctrl+c"))
			if !quits(t, cmd) {
				t.Error("ctrl+c did not quit the program; it is the one key that always does (chat-tui#req:focus-and-keys, chat-tui#req:input-locked-while-pending)")
			}
		})
	}
}

// TestEnterOnAFocusedButtonPressesIt pins enter's other meaning: from the button
// block it presses the focused button rather than submitting the input
// (chat-tui#req:focus-and-keys). The press reaches the Processor from a command,
// because PressButton does network I/O the live region must not block on — the
// same reason submitText sends from one.
func TestEnterOnAFocusedButtonPressesIt(t *testing.T) {
	tests := []struct {
		name string
		// keys walk focus onto the button under test, from the input.
		keys []string
		// want is the callback data the press must carry.
		want string
	}{
		{
			name: "the button the block is entered on",
			keys: []string{"down"},
			want: "space?id=family1",
		},
		{
			// The press reads the cursor rather than assuming the first row.
			name: "a button further down",
			keys: []string{"down", "down"},
			want: "space?id=work1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var pressed []string
			m, _ := pressKeys(t, liveModel(fakeProcessor{pressed: &pressed}, spacesKeyboard()), tt.keys...)

			next, cmd := m.Update(key(t, "enter"))
			m = next.(Model)

			if len(pressed) != 0 {
				t.Errorf("PressButton was called with %q from Update itself; it must run off the UI thread as a command", pressed)
			}
			if !m.pending {
				t.Error("pending = false after a press, want true: a reply is in flight")
			}
			drain(t, cmd)
			if !reflect.DeepEqual(pressed, []string{tt.want}) {
				t.Errorf("PressButton received %q, want exactly one call, carrying [%q]", pressed, tt.want)
			}
		})
	}
}

// TestEnterInTheButtonBlockDoesNotSubmitTheInput is the other half of enter's
// two meanings: a line left in the input is not a turn while the button block
// holds focus (chat-tui#req:focus-and-keys).
func TestEnterInTheButtonBlockDoesNotSubmitTheInput(t *testing.T) {
	var sent, pressed []string
	m := liveModel(fakeProcessor{sent: &sent, pressed: &pressed}, spacesKeyboard())
	m.input.SetValue("hello")
	m, _ = pressKeys(t, m, "down")

	next, cmd := m.Update(key(t, "enter"))
	m = next.(Model)
	drain(t, cmd)

	if len(sent) != 0 {
		t.Errorf("enter in the button block submitted %q to the Processor; from there it presses the focused button", sent)
	}
	if m.input.Value() != "hello" {
		t.Errorf("input = %q after enter in the button block, want %q left untouched", m.input.Value(), "hello")
	}
	if len(pressed) != 1 {
		t.Errorf("PressButton received %q, want the one press enter means here", pressed)
	}
}

// TestEnterOnAButtonCarryingNoCallbackDataDoesNothing pins the one button enter
// cannot press. chat.Processor names a press by its callback data
// (chat-messenger#req:processor-seam) and a URL button carries none, so there is
// no turn to make. It must not put the session in flight either: nothing would
// answer it, and the lock would never lift
// (chat-tui#req:input-locked-while-pending).
func TestEnterOnAButtonCarryingNoCallbackDataDoesNothing(t *testing.T) {
	var pressed []string
	kb := botkb.NewMessageKeyboard(botkb.KeyboardTypeInline,
		[]botkb.Button{botkb.NewUrlButton("Open in browser", "https://sneat.app")},
	)
	m, _ := pressKeys(t, liveModel(fakeProcessor{pressed: &pressed}, kb), "down")
	if m.focus != focusButtons {
		t.Fatal("setup: expected focus in the button block")
	}

	next, cmd := m.Update(key(t, "enter"))
	m = next.(Model)
	msgs := drain(t, cmd)

	if len(pressed) != 0 {
		t.Errorf("PressButton received %q for a button carrying no callback data", pressed)
	}
	if m.pending {
		t.Error("pending = true after enter on a button carrying no callback data: nothing is in flight, and nothing would ever lift the lock (chat-tui#req:input-locked-while-pending)")
	}
	// No turn was made, so nothing superseded the reply: it is still the live
	// one, and its buttons are still focusable. A press that committed before
	// working out whether it had a turn to make would take the transcript, and
	// the user's buttons, with it.
	if got := scrollbackOf(msgs); len(got) != 0 {
		t.Errorf("enter on a button carrying no callback data committed %q to scrollback; it makes no turn, so there is nothing to commit and nothing to echo (chat-tui#req:scrollback-commit)", got)
	}
	if m.live == nil {
		t.Error("live = nil after enter on a button carrying no callback data; it makes no turn, so the reply is not superseded and its buttons stay focusable (chat-tui#req:scrollback-commit)")
	}
}

// TestPendingIgnoresEveryKeyButCtrlC pins the lock
// (chat-tui#req:input-locked-while-pending). Each key here would do something
// were the reply not in flight — which is what makes every assertion below one
// that can fail: without the lock, "x" reaches the input, enter submits, esc
// quits, and down enters the button block.
//
// ctrl+c is the exception, and TestCtrlCAlwaysQuits covers it.
func TestPendingIgnoresEveryKeyButCtrlC(t *testing.T) {
	for _, name := range []string{"enter", "esc", "up", "down", "left", "right", "tab", "x"} {
		t.Run(name, func(t *testing.T) {
			var sent, pressed []string
			m := liveModel(fakeProcessor{sent: &sent, pressed: &pressed}, spacesKeyboard())
			m.input.SetValue("hello")
			m.pending = true

			next, cmd := m.Update(key(t, name))
			m = next.(Model)
			msgs := drain(t, cmd)

			if quitsIn(msgs) {
				t.Errorf("%q quit the program while a reply was in flight; only ctrl+c does", name)
			}
			if len(sent) != 0 || len(pressed) != 0 {
				t.Errorf("%q reached the Processor while a reply was in flight (SendText %q, PressButton %q)", name, sent, pressed)
			}
			if got := scrollbackOf(msgs); len(got) != 0 {
				t.Errorf("%q committed %q to scrollback while a reply was in flight", name, got)
			}
			if m.focus != focusInput {
				t.Errorf("focus = %v after %q while a reply was in flight, want it left on the input", m.focus, name)
			}
			if m.input.Value() != "hello" {
				t.Errorf("input = %q after %q while a reply was in flight, want %q: the input is locked", m.input.Value(), name, "hello")
			}
			if !m.pending {
				t.Errorf("pending = false after %q; the reply it was waiting for is still in flight", name)
			}
		})
	}
}

// TestPendingLocksOnlyKeys pins the edge of the lock: it refuses input, not the
// answer the input is waiting for. A lock that also swallowed the Processor's
// reply would never lift (chat-tui#req:input-locked-while-pending).
func TestPendingLocksOnlyKeys(t *testing.T) {
	m := liveModel(fakeProcessor{}, spacesKeyboard())
	m.pending = true

	next, _ := m.Update(repliesMsg{[]chat.Reply{{Text: "Hi!"}}})
	if m = next.(Model); m.pending {
		t.Error("pending = true after the replies arrived; the lock lifts when the reply lands")
	}
}

// --- failures (REQ: errors-render-in-transcript) ---

// errProcessorFailure is the failure the tests below drive.
//
// It is a bare error carrying no user-facing wording, which is what a Processor
// returns by contract (chat-messenger#req:errors-are-returned-not-formatted). It
// deliberately does not call itself an error: that is the renderer's word to add,
// and TestFailureIsWordedByTheRenderer can only tell the two apart because this
// string does not already contain it.
var errProcessorFailure = errors.New("space reader: connection refused")

// TestFailureRendersInTheTranscriptAndTheSessionContinues drives
// _tests/failure-renders-in-transcript.md end to end: a turn that fails joins the
// transcript as a message rather than destroying it
// (chat-tui#req:errors-render-in-transcript,
// chat-tui#ac:transcript-is-durable-terminal-text).
func TestFailureRendersInTheTranscriptAndTheSessionContinues(t *testing.T) {
	// A transcript already holding a committed turn: what the failure must not
	// cost the user.
	m, first := submitTurn(t, New(fakeProcessor{replies: []chat.Reply{{Text: "Hi!"}}}), "hello")
	if len(first) != 2 || !strings.Contains(first[1], "Hi!") {
		t.Fatalf("setup: the first turn committed %q, want the echo then the reply", first)
	}

	m.proc = fakeProcessor{err: errProcessorFailure}
	m, msgs := turnMsgs(t, m, "/spaces")

	if quitsIn(msgs) {
		t.Error("a Processor failure quit the program; the session continues, because quitting would discard the user's transcript (chat-tui#req:errors-render-in-transcript)")
	}
	sb := scrollbackOf(msgs)
	if len(sb) != 2 {
		t.Fatalf("the failing turn committed %q, want two blocks: the echo then the failure", sb)
	}
	if !strings.Contains(sb[0], "/spaces") {
		t.Errorf("the failing turn committed %q first, want the submitted line", sb[0])
	}
	if !strings.Contains(sb[1], errProcessorFailure.Error()) {
		t.Errorf("the failing turn committed %q, want a block naming the failure %q: an error is rendered as a bot message in the transcript (chat-tui#req:errors-render-in-transcript)", sb[1], errProcessorFailure.Error())
	}

	// The transcript the first turn committed is the terminal's, not the model's,
	// so "retained" is not a claim about model state — there is none to read. What
	// it means operationally is that the failing turn does nothing to what the
	// terminal already owns, which is asserted here the only way it can be: the
	// turn prints, and does nothing else at all.
	for _, msg := range msgs {
		if _, ok := printedBody(msg); !ok {
			t.Errorf("the failing turn produced a %T; it must only print, leaving the transcript the terminal already owns alone (chat-tui#req:errors-render-in-transcript)", msg)
		}
	}

	// The input accepts a further message. The lock the failed turn took has to
	// have lifted with it, or the session is over in everything but name.
	if m.pending {
		t.Error("pending = true after a Processor failure; the failure is the answer the turn was waiting for, and the lock must lift with it or the input never takes another message (chat-tui#req:input-locked-while-pending)")
	}
	var sent []string
	m.proc = fakeProcessor{replies: []chat.Reply{{Text: "Hi again!"}}, sent: &sent}
	_, after := submitTurn(t, m, "hello again")

	if !reflect.DeepEqual(sent, []string{"hello again"}) {
		t.Errorf("a message submitted after a failure reached the Processor as %q, want [%q]: the session continues (chat-tui#req:errors-render-in-transcript)", sent, "hello again")
	}
	if len(after) != 2 || !strings.Contains(after[1], "Hi again!") {
		t.Errorf("the turn after a failure committed %q, want the echo then the reply", after)
	}
}

// TestBothTurnKindsRouteAFailureToTheTranscript pins that the failure path covers
// every way a turn can reach the Processor. SendText and PressButton can both
// fail, so a press that fails must land in the transcript exactly as a submit that
// fails does (chat-tui#req:errors-render-in-transcript).
func TestBothTurnKindsRouteAFailureToTheTranscript(t *testing.T) {
	tests := []struct {
		name string
		// turn makes one turn against proc, with real keys, and pumps its answer
		// back through the model the way the event loop would.
		turn func(t *testing.T, proc chat.Processor) (Model, []tea.Msg)
	}{
		{
			name: "a submitted message fails",
			turn: func(t *testing.T, proc chat.Processor) (Model, []tea.Msg) {
				return turnMsgs(t, New(proc), "/spaces")
			},
		},
		{
			name: "a button press fails",
			turn: func(t *testing.T, proc chat.Processor) (Model, []tea.Msg) {
				return pressTurn(t, liveModel(proc, spacesKeyboard()), "down")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, msgs := tt.turn(t, fakeProcessor{err: errProcessorFailure})

			if quitsIn(msgs) {
				t.Error("a failed turn quit the program; the session continues (chat-tui#req:errors-render-in-transcript)")
			}
			sb := scrollbackOf(msgs)
			if len(sb) == 0 {
				t.Fatal("a failed turn committed nothing to scrollback, want the failure rendered as a bot message (chat-tui#req:errors-render-in-transcript)")
			}
			if last := sb[len(sb)-1]; !strings.Contains(last, errProcessorFailure.Error()) {
				t.Errorf("a failed turn committed %q last, want a block naming the failure %q (chat-tui#req:errors-render-in-transcript)", last, errProcessorFailure.Error())
			}
			if m.pending {
				t.Error("pending = true after a failed turn; a failure answers the turn, and the lock lifts with it (chat-tui#req:input-locked-while-pending)")
			}
		})
	}
}

// TestFailureIsCommittedNotDrawnInTheLiveRegion pins where the failure goes. It
// is a completed turn — nothing about it can still change — so it belongs in the
// scrollback the terminal owns rather than in the region View repaints
// (chat-tui#req:scrollback-commit, chat-tui#req:errors-render-in-transcript).
func TestFailureIsCommittedNotDrawnInTheLiveRegion(t *testing.T) {
	m := New(fakeProcessor{})
	m.pending = true

	next, cmd := m.Update(errMsg{errProcessorFailure})
	m = next.(Model)

	if got := scrollbackOf(drain(t, cmd)); len(got) != 1 || !strings.Contains(got[0], errProcessorFailure.Error()) {
		t.Fatalf("a failure committed %q to scrollback, want exactly one block naming it (chat-tui#req:errors-render-in-transcript)", got)
	}
	if view := m.View(); strings.Contains(view, errProcessorFailure.Error()) {
		t.Errorf("View() = %q draws the failure; it is a completed turn, and the live region draws only what can still change (chat-tui#req:inline-rendering)", view)
	}
}

// TestFailureIsWordedByTheRenderer pins whose job the wording is. The Processor
// hands back a bare error by contract
// (chat-messenger#req:errors-are-returned-not-formatted) — errProcessorFailure
// says "space reader: connection refused" and nothing about that being a failure
// — so if the transcript is to read as one, this package is what has to say so.
//
// The mark is text rather than colour, for the reason the focused button's is:
// lipgloss emits no colour when it is not writing to a terminal, so a block
// distinguished only by colour would reach a piped transcript, and this test,
// looking exactly like an ordinary reply.
func TestFailureIsWordedByTheRenderer(t *testing.T) {
	// The control for the assertion below: it can only tell the renderer's wording
	// from the Processor's while the bare error does not already carry it.
	if strings.Contains(errProcessorFailure.Error(), strings.TrimSpace(errorPrefix)) {
		t.Fatalf("the bare error %q already names itself a failure; the assertion below would pass on wording the renderer never added", errProcessorFailure)
	}

	block := renderError(errProcessorFailure)

	if !strings.Contains(block, errorPrefix) {
		t.Errorf("renderError(%q) = %q, want it marked as a failure with %q: the Processor returns a bare error, and wording it for a terminal is this package's job (chat-messenger#req:errors-are-returned-not-formatted)", errProcessorFailure, block, errorPrefix)
	}
	if !strings.Contains(block, errProcessorFailure.Error()) {
		t.Errorf("renderError(%q) = %q, want it to name the failure it renders", errProcessorFailure, block)
	}
}
