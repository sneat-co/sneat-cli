// Package chattui renders a Sneat chat session inline in the terminal. It is
// launched by `sneat chat`.
//
// It is a package of its own rather than a screen inside internal/tui: that
// package's screen stack serves list navigation, while a chat has no sibling
// screens to navigate to and a focus model spanning a text input and a block of
// buttons.
//
// It depends on internal/chat for the Processor interface only. The concrete
// processor is unexported in that package, so this one cannot name an
// implementation even by accident (chat-messenger#req:processor-seam); the
// composition root that builds one is the RunChat closure in cmd/sneat/main.go.
package chattui

import (
	"context"
	"os/exec"
	"runtime"
	"slices"
	"strings"

	"github.com/bots-go-framework/bots-go-core/botkb"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sneat-co/sneat-cli/internal/chat"
)

// focusTarget names what currently takes keys: the input line, or the live
// reply's button block. Focus is only ever one of the two (REQ: focus-and-keys).
type focusTarget int

const (
	focusInput focusTarget = iota
	focusButtons
)

// Model is the root bubbletea model for a chat session.
//
// It holds only what can still change. The transcript is not a field: committed
// turns are handed to the terminal as ordinary scrollback and never repainted
// (REQ: scrollback-commit), so the terminal owns them, not this model. What
// remains here is the live region — the reply whose buttons are still
// focusable, the input, and where focus sits.
type Model struct {
	proc  chat.Processor
	input textinput.Model

	// live is the most recent bot reply while its buttons are still
	// focusable, or nil when there is none. A reply carrying no keyboard is
	// never live: it commits on arrival.
	live *chat.Reply

	// focus is what keys go to. It is focusButtons only while live is non-nil.
	focus focusTarget

	// row and col are the focused button's position in live's keyboard, read
	// only while focus is focusButtons.
	row, col int

	// cmdIndex is the highlighted row in the command palette, read only while
	// the palette is open. paletteOff suppresses the palette after esc dismisses
	// it, until the typed command changes — so a dismissed palette does not
	// spring back on the next keystroke (REQ: command-palette).
	cmdIndex   int
	paletteOff bool

	// pending reports whether a reply is in flight. While set, keys other than
	// ctrl+c are ignored (REQ: input-locked-while-pending) and the typing
	// indicator renders (REQ: pending-is-visible).
	pending bool

	// deferred holds the scrollback a callback press would commit — the
	// superseded card and the echo of the pressed label — held until the press's
	// reply returns. Only then is it known whether that reply edits the card in
	// place (commit nothing, discard these) or supersedes it (commit these, then
	// render the reply), so the commit cannot be done eagerly the way a submit's
	// is (REQ: scrollback-commit, REQ: card-edit-in-place). Nil unless a callback
	// press is in flight.
	deferred []string

	// browser opens a URL in the user's browser, for a URL button
	// (REQ: button-kinds). It is injected rather than called directly so a test
	// drives a URL press without launching one; New wires the platform opener.
	browser func(url string) error

	// spin animates the typing indicator. It is ticked only while pending, so an
	// idle session schedules no work.
	spin spinner.Model

	// width is the terminal width the live region renders to.
	width int
}

// Model must satisfy tea.Model for tea.NewProgram to take it.
var _ tea.Model = Model{}

// New builds the root model for a chat session processed by proc.
//
// It takes the chat.Processor interface, never a concrete type
// (chat-messenger#req:processor-seam).
func New(proc chat.Processor) Model {
	in := textinput.New()
	in.Placeholder = "Type a message"
	in.Prompt = inputPrompt
	in.Width = inputWidth(defaultWidth)
	in.Focus()
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = pendingStyle
	return Model{
		proc:    proc,
		input:   in,
		spin:    sp,
		width:   defaultWidth,
		focus:   focusInput,
		browser: openInBrowser,
	}
}

// openInBrowser opens url in the user's default browser, using the platform's
// own launcher. It is the default wired into New; a test injects its own opener
// instead, so pressing a URL button never launches a real browser
// (REQ: button-kinds).
//
// The launcher is started, not waited on: it hands the URL to the browser and
// returns, rather than blocking until the browser exits.
func openInBrowser(url string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{url}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		name, args = "xdg-open", []string{url}
	}
	return exec.Command(name, args...).Start()
}

// inputWidth converts a terminal width into the width the text input renders
// at: the terminal less the prompt it sits behind.
//
// Setting it is not cosmetic. textinput.placeholderView sizes its buffer as
// `make([]rune, Width+1)` and copies the placeholder into it, so a zero Width
// silently truncates the placeholder to one rune — the input renders "> T"
// rather than "> Type a message". The floor keeps that from recurring on a
// terminal too narrow to hold the placeholder.
func inputWidth(terminal int) int {
	w := terminal - lipgloss.Width(inputPrompt)
	if min := lipgloss.Width("Type a message"); w < min {
		return min
	}
	return w
}

// defaultWidth is the assumed terminal width until the first WindowSizeMsg
// arrives. Bubble Tea sends one on startup, so this only covers the first
// frame.
const defaultWidth = 80

// Init starts the input's cursor blinking.
func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

// --- turns ---

// repliesMsg carries a Processor's answer to a turn back onto the UI thread.
type repliesMsg struct{ replies []chat.Reply }

// errMsg carries a Processor failure back onto the UI thread. The Processor
// returns a bare error by contract
// (chat-messenger#req:errors-are-returned-not-formatted): wording it for a
// terminal is this package's job, not its.
type errMsg struct{ err error }

// sendText hands a typed message to the Processor from a command, so the call
// runs off the UI thread. SendText does network I/O; calling it inline in Update
// would freeze the live region until the backend answered.
func sendText(p chat.Processor, text string) tea.Cmd {
	return func() tea.Msg {
		replies, err := p.SendText(context.Background(), text)
		if err != nil {
			return errMsg{err}
		}
		return repliesMsg{replies}
	}
}

// pressButton hands a pressed button's callback data to the Processor from a
// command, for the same reason sendText does: PressButton does network I/O, and
// the live region must not block on it.
func pressButton(p chat.Processor, data string) tea.Cmd {
	return func() tea.Msg {
		replies, err := p.PressButton(context.Background(), data)
		if err != nil {
			return errMsg{err}
		}
		return repliesMsg{replies}
	}
}

// openBrowser opens url through open, from a command, so a launcher that blocks
// does not block the live region (REQ: button-kinds).
//
// A URL button makes no chat turn: on success the command produces no message at
// all, so nothing commits and the card stays live. A launcher that fails becomes
// a bot message in the transcript rather than taking the session down, exactly as
// a Processor failure does (REQ: errors-render-in-transcript).
func openBrowser(open func(string) error, url string) tea.Cmd {
	return func() tea.Msg {
		if err := open(url); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

// Update handles the terminal's size, the key table, and the Processor's answer,
// and passes everything else to the input.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.input.Width = inputWidth(msg.Width)
	case spinner.TickMsg:
		// Only keep the animation going while a turn is in flight. Once it
		// resolves the tick stops rescheduling itself and the loop goes quiet.
		if !m.pending {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case repliesMsg:
		return m.receive(msg.replies)
	case errMsg:
		return m.fail(msg.err)
	case tea.KeyMsg:
		next, cmd := m.handleKey(msg)
		return next.syncInputCursor(cmd)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// --- keys (REQ: focus-and-keys, REQ: input-locked-while-pending) ---

// The keys the requirement names, spelled as bubbletea names them
// (tea.KeyMsg.String) rather than as tea.KeyType values, so the tables below can
// be read against the requirement line by line.
const (
	keyCtrlC = "ctrl+c"
	keyEnter = "enter"
	keyEsc   = "esc"
	keyUp    = "up"
	keyDown  = "down"
	keyLeft  = "left"
	keyRight = "right"
)

// handleKey gives a key press its one meaning.
//
// Which meaning that is depends on two things, applied here in the order that
// makes each of them absolute: ctrl+c first, because nothing may take the exit
// away; then the pending lock, because it has to be able to refuse the very keys
// focus would otherwise act on. Only what survives both reaches a focus table,
// and each key appears in exactly one of those.
func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	// ctrl+c quits from either focus, and even while a reply is in flight
	// (REQ: input-locked-while-pending). It is matched ahead of the lock for
	// precisely that reason.
	if msg.String() == keyCtrlC {
		return m, tea.Quit
	}
	// While a reply is in flight every other key is ignored, so nothing the user
	// does can race the answer they are waiting for.
	//
	// internal/tui's confirm screen guards its in-flight delete the same way, but
	// it returns before ctrl+c too. That is safe for a screen resolving a single
	// round trip; a chat session may block on a slow backend for as long as it
	// likes, so the carve-out above is where this deliberately parts company with
	// it — the user must always retain a way out.
	if m.pending {
		return m, nil
	}
	if m.focus == focusButtons {
		return m.handleButtonKey(msg)
	}
	if m.paletteOpen() {
		return m.handleCommandKey(msg)
	}
	return m.handleInputKey(msg)
}

// --- command palette (REQ: command-palette) ---

// commandQuery reports whether s is a command still being typed: a leading
// slash and no space yet. A bare "/" qualifies, so the palette opens the moment
// the slash is typed and lists everything.
func commandQuery(s string) bool {
	return strings.HasPrefix(s, "/") && !strings.ContainsRune(s, ' ')
}

// paletteMatches are the commands the current input is a prefix of, in the
// processor's own order. Empty when the input is not a command query.
func (m Model) paletteMatches() []chat.CommandInfo {
	q := m.input.Value()
	if !commandQuery(q) {
		return nil
	}
	var out []chat.CommandInfo
	for _, c := range m.proc.Commands() {
		if strings.HasPrefix(c.Name, q) {
			out = append(out, c)
		}
	}
	return out
}

// paletteOpen reports whether the palette is showing and holding the focus. It
// is derived, not stored: the palette is exactly "the input holds a command
// query that matches something, and esc has not dismissed it". Focus stays
// {input, buttons}; the palette is an overlay on the input, so a press can only
// reach it from the input, never from the button block.
func (m Model) paletteOpen() bool {
	return m.focus == focusInput && !m.paletteOff && len(m.paletteMatches()) > 0
}

// handleCommandKey routes a key while the palette is open. up/down move the
// highlight and stop at the ends, enter runs the highlighted command, esc
// dismisses without running; anything else edits the input, which re-filters
// the list and clears the dismissal.
func (m Model) handleCommandKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	matches := m.paletteMatches()
	switch msg.String() {
	case keyUp:
		if m.cmdIndex > 0 {
			m.cmdIndex--
		}
		return m, nil
	case keyDown:
		if m.cmdIndex < len(matches)-1 {
			m.cmdIndex++
		}
		return m, nil
	case keyEnter:
		// Run the highlighted command by submitting its name — the same path a
		// typed command takes, so the transcript and the send are identical.
		m.input.SetValue(matches[m.cmdIndex].Name)
		return m.submitText()
	case keyEsc:
		m.paletteOff = true
		return m, nil
	}
	// Editing the query: let the input handle the key, then clear the dismissal
	// so the palette re-opens, and keep the highlight inside the narrowed list.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.paletteOff = false
	m.cmdIndex = clampCol(m.cmdIndex, len(m.paletteMatches()))
	return m, cmd
}

// handleInputKey gives a key its meaning while the input holds focus. Keys it
// does not name are the input's to interpret: they are text.
func (m Model) handleInputKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case keyEsc:
		// esc quits from the input. From the button block it means something else
		// entirely — which is why the two tables are separate rather than one
		// table asking where focus is at every entry.
		return m, tea.Quit
	case keyEnter:
		return m.submitText()
	case keyUp:
		// The block renders above the input, so up is the key that reaches it,
		// and it lands on the LAST row — the button physically nearest the cursor
		// that just left. Entering at row 0 would jump focus to the button
		// furthest from where the user is looking.
		//
		// With no live reply there is no block to enter — focus is focusButtons
		// only while live is non-nil — so the key is the input's, like any other.
		if rows := m.liveButtonRows(); len(rows) > 0 {
			m.focus = focusButtons
			m.row = len(rows) - 1
			m.col = clampCol(0, len(rows[m.row]))
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	// An edit here can turn the input into a command query — typing the leading
	// slash — so re-arm the palette and start its highlight at the top. Refining
	// an already-open palette goes through handleCommandKey, which keeps the
	// highlight instead.
	m.paletteOff = false
	m.cmdIndex = 0
	return m, cmd
}

// handleButtonKey gives a key its meaning while the button block holds focus.
//
// Keys it does not name do nothing at all. They are not passed to the input: the
// input does not hold focus, and exactly one target does
// (AC: interaction-is-unambiguous).
func (m Model) handleButtonKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	rows := m.liveButtonRows()
	if len(rows) == 0 {
		// Focus is in a block that does not exist. Nothing reaches this: focus
		// becomes focusButtons only over a block with rows, and commitLive returns
		// focus with the reply it commits. If anything ever did, the input is
		// where focus belongs, rather than a cursor over nothing.
		m.focusInputLine()
		return m, nil
	}
	switch msg.String() {
	case keyEsc:
		// esc leaves the block for the input. It does not quit: quitting is what
		// esc means once focus is back on the input, one press later.
		m.focusInputLine()
	case keyUp:
		// up moves deeper into the block, toward its top. It stops at row 0: it
		// is already the key that entered the block, and a key that both entered
		// and left it would have two meanings for the same focus.
		if m.row > 0 {
			m.row--
			m.col = clampCol(m.col, len(rows[m.row]))
		}
	case keyDown:
		// down is how focus leaves the block, back to the input below it — the
		// mirror of the up that entered. From any row above the last it is a move
		// within the block.
		if m.row >= len(rows)-1 {
			m.focusInputLine()
			break
		}
		m.row++
		m.col = clampCol(m.col, len(rows[m.row]))
	case keyLeft:
		// left and right move within the row and stop at its ends. Wrapping would
		// give either a second meaning — "go to the other end" — for the same key
		// and the same focus.
		if m.col > 0 {
			m.col--
		}
	case keyRight:
		if m.col < len(rows[m.row])-1 {
			m.col++
		}
	case keyEnter:
		return m.pressFocusedButton()
	}
	return m, nil
}

// pressFocusedButton makes the turn the focused button stands for, dispatched by
// the button's kind (REQ: button-kinds).
//
// The three kinds part ways in what a press means and so in how it commits:
//
//   - A callback button (botkb.DataButton) runs PressButton. Its reply may edit
//     the card in place or supersede it, and which is only known once the reply
//     returns, so the commit is deferred rather than done eagerly: the superseded
//     card and the echo are held in m.deferred until receive decides
//     (REQ: scrollback-commit, REQ: card-edit-in-place).
//
//   - A send button (botkb.TextButton) puts its own text into the conversation
//     through SendText, exactly as if the user had typed and submitted it. It
//     always appends, so it commits eagerly, the way submit does.
//
//   - A URL button (botkb.UrlButton) opens its URL in the browser and makes no
//     chat turn at all: nothing commits, nothing echoes, and the card stays live
//     so the user can press on.
//
// The echo, for the two kinds that make one, names the button's label rather than
// its callback data: the label is what the user saw and chose, and
// `space?id=family1` is an implementation detail of the seam it travels over.
func (m Model) pressFocusedButton() (Model, tea.Cmd) {
	btn, ok := m.focusedButton()
	if !ok {
		return m, nil
	}
	switch btn.ButtonType() {
	case botkb.ButtonTypeText:
		// A send button is a submit whose text is the button's own: same commit,
		// same order, same path to the Processor.
		return m.submit(btn.GetText())
	case botkb.ButtonTypeURL:
		// Opening a browser is a side effect on the world, not a chat turn: no
		// commit, no echo, no pending lock, and the card stays live. A URL that
		// will not open surfaces in the transcript rather than crashing the
		// session (REQ: button-kinds).
		if url, ok := buttonURL(btn); ok {
			return m, openBrowser(m.browser, url)
		}
		return m, nil
	}
	data, ok := buttonData(btn)
	if !ok {
		// A callback button with no data names no turn: chat.Processor identifies a
		// press by its data and there is none (chat-messenger#req:processor-seam).
		// Nothing is committed, because nothing happened — the reply is not
		// superseded and its buttons are still the user's to press — and it must
		// not go pending, or the lock would never lift.
		return m, nil
	}

	// A callback press defers its commit. The superseded card and the echo are
	// rendered now, while the card is still live, but held in m.deferred rather
	// than printed: receive commits them only if the reply supersedes the card,
	// and discards them if it edits the card in place. The card stays live
	// meanwhile, so a slow reply leaves the pressed card on screen rather than a
	// blank region.
	m.deferred = nil
	if m.live != nil {
		m.deferred = append(m.deferred, renderCommittedReply(*m.live))
	}
	m.deferred = append(m.deferred, renderUserEcho(btn.GetText()))

	m.pending = true
	return m, tea.Batch(m.spin.Tick, pressButton(m.proc, data))
}

// focusInputLine puts focus back on the input, leaving no cursor behind in the
// button block.
//
// Focus is focusButtons only while there is a block to point into, and up
// re-enters at the last row, so a cursor left where focus used to be would be
// state nothing reads and every path would have to remember to reset.
func (m *Model) focusInputLine() {
	m.focus = focusInput
	m.row, m.col = 0, 0
}

// syncInputCursor points the input's own cursor at whether the input holds focus,
// and passes cmd through.
//
// The input draws a blinking cursor while it is focused, and the live region
// marks the focused button while the block holds focus. Both at once would name
// two focus holders, and exactly one ever holds it
// (AC: interaction-is-unambiguous).
//
// It runs once after every key rather than at each place focus moves, so focus —
// the one field that says where keys go — stays the only thing a key handler has
// to get right, and the cursor follows it wherever it goes.
func (m Model) syncInputCursor(cmd tea.Cmd) (Model, tea.Cmd) {
	switch {
	case m.focus == focusInput && !m.input.Focused():
		// Focus returns the cursor's blink command, which has to reach the event
		// loop or the cursor sits still.
		return m, tea.Batch(cmd, m.input.Focus())
	case m.focus == focusButtons && m.input.Focused():
		m.input.Blur()
	}
	return m, cmd
}

// liveButtonRows returns the rows of buttons the live reply carries, or nil when
// nothing is live.
//
// Focus moves over exactly what View draws, because both read the block through
// this: a cursor indexing rows the renderer skipped would mark one button and
// press another.
func (m Model) liveButtonRows() [][]botkb.Button {
	if m.live == nil {
		return nil
	}
	return buttonRows(*m.live)
}

// focusedButton returns the button focus sits on, and whether there is one.
func (m Model) focusedButton() (botkb.Button, bool) {
	rows := m.liveButtonRows()
	if m.row < 0 || m.row >= len(rows) {
		return nil, false
	}
	row := rows[m.row]
	if m.col < 0 || m.col >= len(row) {
		return nil, false
	}
	return row[m.col], true
}

// buttonData returns b's callback data, and whether it carries any.
//
// botkb.Button exposes only its text and its type, so reading callback data means
// asserting the concrete botkb.DataButton — in both its forms, since its methods
// take a value receiver and either a value or a pointer can be stored in the
// interface, exactly as buttonRows does for the keyboard. Any other kind of
// button (a URL, a text button) identifies no turn to a chat.Processor, which
// names a press by its data.
func buttonData(b botkb.Button) (string, bool) {
	switch b := b.(type) {
	case *botkb.DataButton:
		if b == nil {
			return "", false
		}
		return b.Data, true
	case botkb.DataButton:
		return b.Data, true
	}
	return "", false
}

// buttonURL returns b's URL, and whether it carries one.
//
// Like buttonData, it reads a concrete botkb type off the Button interface — the
// URL button in both its value and pointer forms, since either can be stored in
// the interface. Any other kind of button opens no browser.
func buttonURL(b botkb.Button) (string, bool) {
	switch b := b.(type) {
	case *botkb.UrlButton:
		if b == nil {
			return "", false
		}
		return b.URL, true
	case botkb.UrlButton:
		return b.URL, true
	}
	return "", false
}

// clampCol keeps the button cursor inside a row of width buttons.
//
// Rows need not be the same width, so moving between them can leave the column
// past the end of the row it lands in: a cursor marking no button, which enter
// could not press. buttonRows drops empty rows, so width is at least 1 and there
// is always a button to land on.
func clampCol(col, width int) int {
	if col >= width {
		return width - 1
	}
	if col < 0 {
		return 0
	}
	return col
}

// submitText commits the turn's user input and starts the Processor call.
//
// The order is the one REQ: scrollback-commit fixes: the live reply commits
// first, then the echo of what the user submitted, and only then does the turn
// go to the Processor. Both commits are joined into a single Println so nothing
// can interleave them, and the send is sequenced after that Println rather than
// batched alongside it — tea.Batch runs its commands concurrently, which would
// let a fast Processor land its answer in scrollback ahead of the line that
// prompted it.
func (m Model) submitText() (Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		// A bare enter is not a turn: it would reach the Processor with nothing
		// and leave a blank echo in the transcript.
		return m, nil
	}
	m.input.SetValue("")
	return m.submit(text)
}

// submit commits the live reply and an echo of text, then sends text to the
// Processor — the eager-commit path a typed line and a send button share.
//
// SendText always appends a reply, so the commit is done eagerly and ordered
// ahead of the send: both commits are joined into a single Println so nothing can
// interleave them, and the send is sequenced after that Println rather than
// batched alongside it — tea.Batch runs its commands concurrently, which would
// let a fast Processor land its answer in scrollback ahead of the line that
// prompted it (REQ: scrollback-commit).
func (m Model) submit(text string) (Model, tea.Cmd) {
	var blocks []string
	if live := m.commitLive(); live != "" {
		blocks = append(blocks, live)
	}
	blocks = append(blocks, renderUserEcho(text))

	m.pending = true
	return m, tea.Sequence(commit(blocks), tea.Batch(m.spin.Tick, sendText(m.proc, text)))
}

// receive renders the Processor's answer to a turn: every reply commits to
// scrollback except a trailing one carrying buttons, which stays live until
// something supersedes it (REQ: scrollback-commit).
//
// Only a trailing reply can be live because only the most recent message's
// buttons are focusable (REQ: focus-and-keys). A keyboard on any earlier reply of
// the same turn is already superseded by the time the turn is drawn, so it
// commits with the reply that carries it, inert.
func (m Model) receive(replies []chat.Reply) (Model, tea.Cmd) {
	m.pending = false

	// A card edit: a single reply with its Edit flag set replaces the live card in
	// place — no commit, no echo — and focus stays on the new card's first button,
	// so the user chains presses through a menu without returning to the input
	// between each (REQ: card-edit-in-place). Whatever the press deferred is
	// discarded: the card was not superseded, it was rewritten, and its earlier
	// form belongs nowhere in the transcript.
	if len(replies) == 1 && replies[0].Edit {
		card := replies[0]
		m.live = &card
		m.deferred = nil
		m.focus = focusButtons
		m.row, m.col = 0, 0
		return m, nil
	}

	// Any other outcome supersedes the card. Whatever a press deferred — the
	// superseded card and the echo of the pressed label — commits now, ahead of
	// the replies (a submit deferred nothing: it committed its echo eagerly). The
	// old card's live slot is then dropped; it is already in the deferred block.
	deferred := m.deferred
	m.deferred = nil
	m.live = nil
	m.focusInputLine()

	if n := len(replies); n > 0 && len(buttonRows(replies[n-1])) > 0 {
		last := replies[n-1]
		m.live = &last
		m.row, m.col = 0, 0
		replies = replies[:n-1]
		// Focus stays on the input: entering the button block is `up`
		// (REQ: focus-and-keys), not something a reply does to the user. A card
		// edit is the one exception, handled above.
	}
	blocks := make([]string, 0, len(replies))
	for _, r := range replies {
		blocks = append(blocks, renderCommittedReply(r))
	}
	return m, commitGroups(deferred, blocks)
}

// fail renders a Processor failure as a bot message in the transcript and leaves
// the session running (REQ: errors-render-in-transcript).
//
// The failure commits to scrollback rather than being drawn in the live region,
// because it is a completed turn: it is how the turn ended, and nothing about it
// can still change (REQ: scrollback-commit). It joins the transcript exactly as a
// reply does, which is the point — a backend that failed is a thing the
// conversation records, not a thing that interrupts it.
//
// It must not quit, and that is the requirement's own reasoning rather than a
// preference: the transcript is what the user came away with, and a program that
// exits on a failed turn takes the session down over something the next turn may
// well recover from. So the failure lifts the pending lock the same way replies
// do — a failure is an answer — and the input goes on taking messages.
func (m Model) fail(err error) (Model, tea.Cmd) {
	m.pending = false
	// A callback press deferred its commit until its reply returned; the reply is
	// this failure, which supersedes the card just as a real reply would. So the
	// deferred card and echo commit first, ahead of the error, and the card's live
	// slot is dropped. A submit deferred nothing — it committed eagerly — so this
	// is just the error for it.
	deferred := m.deferred
	m.deferred = nil
	m.live = nil
	m.focusInputLine()
	return m, commitGroups(deferred, []string{renderError(err)})
}

// commitLive renders the live reply for scrollback and clears it, returning the
// rendered block, or "" when nothing was live.
//
// The reply commits with its buttons inert: they stop being focusable at the
// moment it commits, which is precisely what completes the turn
// (REQ: scrollback-commit). Focus returns to the input with it — the block it
// pointed into no longer exists, and focus is focusButtons only while live is
// non-nil.
func (m *Model) commitLive() string {
	if m.live == nil {
		return ""
	}
	block := renderCommittedReply(*m.live)
	m.live = nil
	m.focusInputLine()
	return block
}

// commit hands rendered transcript blocks to the terminal as scrollback.
//
// tea.Println prints above the live region, and the program never repaints what
// it printed. That is what makes a committed turn ordinary terminal text —
// selectable, copyable and searchable with the terminal's own tooling, and still
// there after the session exits (REQ: scrollback-commit, REQ: inline-rendering).
//
// The blocks are joined into one Println rather than printed by several, so their
// order is fixed by the string rather than by the order the event loop happens to
// deliver them in.
func commit(blocks []string) tea.Cmd {
	if len(blocks) == 0 {
		return nil
	}
	return tea.Println(strings.Join(blocks, "\n"))
}

// commitGroups commits each group of blocks as its own scrollback entry, in
// order, skipping empty groups.
//
// A press defers two things that are separate turns in the transcript — the
// superseded card with its echo, and the reply the press prompted — so they must
// commit as separate Printlns rather than one joined block, or the reply would
// read as part of the message it answered (REQ: scrollback-commit). The groups
// are sequenced, not batched, so their order is the string's rather than the
// event loop's; a single non-empty group needs no sequence at all.
func commitGroups(groups ...[]string) tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(groups))
	for _, g := range groups {
		if c := commit(g); c != nil {
			cmds = append(cmds, c)
		}
	}
	switch len(cmds) {
	case 0:
		return nil
	case 1:
		return cmds[0]
	default:
		return tea.Sequence(cmds...)
	}
}

// View renders the live region — the only part of the chat that is repainted.
// Everything above it is committed scrollback the terminal owns
// (REQ: scrollback-commit).
//
// The region is the focusable reply and its buttons (when there is one), the
// input line, and the footer hint (REQ: inline-rendering). It is deliberately not
// the transcript: redrawing past turns here would fight the terminal for text it
// already owns.
func (m Model) View() string {
	var b strings.Builder
	if m.live != nil {
		b.WriteString(renderLiveReply(*m.live, m.focus == focusButtons, m.row, m.col))
		b.WriteByte('\n')
	}
	if m.paletteOpen() {
		b.WriteString(renderPalette(m.paletteMatches(), m.cmdIndex))
		b.WriteByte('\n')
	}
	if m.pending {
		b.WriteString(m.spin.View())
		b.WriteString(pendingStyle.Render(pendingLabel))
		b.WriteByte('\n')
	}
	b.WriteString(m.input.View())
	b.WriteByte('\n')
	b.WriteString(footerStyle.Render(m.footerHelp()))
	return b.String()
}

// footerHelp is the live region's hint line: the keys that work right now, each
// with the one meaning it has right now (REQ: inline-rendering).
//
// It is state-aware because the keys are. enter and esc each mean one thing on
// the input and another in the button block, and while a reply is in flight only
// ctrl+c means anything at all (REQ: focus-and-keys,
// REQ: input-locked-while-pending). A single fixed line could not name them
// without also naming meanings that do not apply here — leaving the user to work
// out which, which is the ambiguity a hint exists to remove.
func (m Model) footerHelp() string {
	switch {
	case m.pending:
		return footerHelpPending
	case m.paletteOpen():
		return footerHelpPalette
	case m.focus == focusButtons:
		return footerHelpButtons
	case len(m.liveButtonRows()) > 0:
		// Offered against the same thing down checks, so the hint cannot promise
		// a key that would do nothing.
		return footerHelpInputLive
	default:
		return footerHelpInput
	}
}

// --- rendering ---

// buttonRows returns the rows of buttons r's keyboard carries, skipping empty
// rows, or nil when it carries none this renderer can draw.
//
// botkb.Keyboard exposes only its type, so reading the buttons means asserting
// the concrete botkb.MessageKeyboard — in both its forms, since its methods take
// a value receiver and either a value or a pointer can be stored in the
// interface. A keyboard of some other implementation has no buttons this renderer
// can see; a nil or empty one has none to show. All of them answer nil, which is
// what keeps a reply with nothing focusable out of the live slot: it has nothing
// to wait for, so it is complete on arrival (REQ: scrollback-commit).
func buttonRows(r chat.Reply) [][]botkb.Button {
	var rows [][]botkb.Button
	switch kb := r.Keyboard.(type) {
	case *botkb.MessageKeyboard:
		if kb == nil {
			return nil
		}
		rows = kb.Buttons
	case botkb.MessageKeyboard:
		rows = kb.Buttons
	default:
		return nil
	}
	out := make([][]botkb.Button, 0, len(rows))
	for _, row := range rows {
		if len(row) > 0 {
			out = append(out, row)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// renderLiveReply renders the reply whose buttons are still focusable, marking
// the focused one when focus is in its button block.
func renderLiveReply(r chat.Reply, buttonsFocused bool, row, col int) string {
	if !buttonsFocused {
		return renderReply(r, buttonStyle, noFocus, noFocus)
	}
	return renderReply(r, buttonStyle, row, col)
}

// renderCommittedReply renders a reply as it goes into scrollback: its buttons
// inert text, none of them focused (REQ: scrollback-commit). It takes no focus
// argument at all, so no caller can carry a focus mark into the transcript.
func renderCommittedReply(r chat.Reply) string {
	return renderReply(r, inertButtonStyle, noFocus, noFocus)
}

// renderReply renders a reply's text and its rows of buttons. Buttons that are
// not focused take the unfocused style; focusRow and focusCol name the focused
// one, and noFocus for either means none is.
func renderReply(r chat.Reply, unfocused lipgloss.Style, focusRow, focusCol int) string {
	var buttons []string
	for i, row := range buttonRows(r) {
		cells := make([]string, 0, len(row))
		for j, btn := range row {
			label := btn.GetText() + buttonKindMark(btn)
			if i == focusRow && j == focusCol {
				cells = append(cells, focusedButtonStyle.Render(focusedMarkLeft+label+focusedMarkRight))
				continue
			}
			cells = append(cells, unfocused.Render("[ "+label+" ]"))
		}
		buttons = append(buttons, strings.Join(cells, " "))
	}
	return frameReply(strings.Split(r.Text, "\n"), buttons)
}

// buttonKindMark is the glyph that tells a button's kind apart in its label
// (REQ: button-kinds). A callback acts inside the card and carries none; a send
// button » puts its own text into the conversation; a URL button ↗ leaves for the
// browser. The mark rides in the label rather than in colour alone, for the
// reason the focused button's does: a monochrome terminal and a copied transcript
// keep the glyph but lose the colour, and the kind must survive both.
//
// It reads the kind off the botkb.Button interface's own ButtonType rather than
// type-asserting, so a button kind added to the vocabulary is a case here, not a
// silent fall-through to "callback".
func buttonKindMark(b botkb.Button) string {
	switch b.ButtonType() {
	case botkb.ButtonTypeText:
		return " »"
	case botkb.ButtonTypeURL:
		return " ↗"
	default:
		return ""
	}
}

// frameReply draws a bot message: its text, then — when it has buttons — a rule
// across the frame, then the buttons.
//
// The rule is where the message stops and the actions start, which is the same
// line focus stops at: everything below it is what `up` reaches
// (REQ: focus-and-keys). Without it the buttons read as more lines of text,
// since that is exactly what they are.
//
// The box is built here rather than by a lipgloss border style because lipgloss
// has no mid-border divider — a rule rendered as content would float inside the
// frame instead of meeting it, which reads as another line of the message and
// so says the opposite of what it is for.
// renderPalette draws the command list above the input, the highlighted row
// marked and inverted (REQ: command-palette). It is framed like a reply so it
// reads as one attached block, and its glyph marker — not colour alone — is
// what a monochrome terminal and a test both see, the same reason the focused
// button carries one.
func renderPalette(cmds []chat.CommandInfo, index int) string {
	// Align the summaries into a column: the widest "name arg" sets it.
	head := make([]string, len(cmds))
	headWidth := 0
	for i, c := range cmds {
		h := c.Name
		if c.Arg != "" {
			h += " " + c.Arg
		}
		head[i] = h
		if w := lipgloss.Width(h); w > headWidth {
			headWidth = w
		}
	}

	lines := make([]string, len(cmds))
	for i, c := range cmds {
		row := head[i] + strings.Repeat(" ", headWidth-lipgloss.Width(head[i])) + "  " + c.Summary
		if i == index {
			lines[i] = focusedButtonStyle.Render(focusedMarkLeft + row)
		} else {
			lines[i] = "  " + buttonStyle.Render(row)
		}
	}
	return frameReply(lines, nil)
}

func frameReply(text, buttons []string) string {
	b := lipgloss.RoundedBorder()

	// Widths are measured with lipgloss.Width, not len: the button cells carry
	// ANSI, which has no display width, and every line has to pad to the same
	// column or the right edge frays.
	inner := 0
	for _, line := range slices.Concat(text, buttons) {
		if w := lipgloss.Width(line); w > inner {
			inner = w
		}
	}

	side := func(s string) string { return borderStyle.Render(s) }
	rule := borderStyle.Render(strings.Repeat(b.Top, inner+2))
	row := func(line string) string {
		return side(b.Left) + " " + line + strings.Repeat(" ", inner-lipgloss.Width(line)) + " " + side(b.Right)
	}

	out := make([]string, 0, len(text)+len(buttons)+3)
	out = append(out, side(b.TopLeft)+rule+side(b.TopRight))
	for _, line := range text {
		out = append(out, row(line))
	}
	if len(buttons) > 0 {
		// Only when there are buttons: a reply with no keyboard — /help, the
		// free-text notice, an error — is one zone, and a rule across it would
		// divide nothing.
		out = append(out, side(b.MiddleLeft)+rule+side(b.MiddleRight))
		for _, line := range buttons {
			out = append(out, row(line))
		}
	}
	out = append(out, side(b.BottomLeft)+rule+side(b.BottomRight))
	return strings.Join(out, "\n")
}

// renderError renders a Processor failure as the bot message it becomes in the
// transcript (REQ: errors-render-in-transcript).
//
// The wording is this package's own. The Processor hands back a bare error by
// contract (chat-messenger#req:errors-are-returned-not-formatted), so naming it
// as a failure falls to the renderer — the only layer that knows how an error
// should look on a terminal. The prefix is the same one internal/tui puts on its
// error text, for the same reason the styles below are restated rather than
// imported: the two packages read alike without depending on each other.
//
// It is marked as a failure in text and not by colour alone, for the reason the
// focused button is: lipgloss emits no colour when it is not writing to a
// terminal, so a block distinguished only by colour would reach a piped or
// copied transcript looking exactly like an ordinary reply.
func renderError(err error) string {
	return errStyle.Render(errorPrefix + err.Error())
}

// renderUserEcho renders what the user did, for scrollback: the text they
// submitted, or the label of the button they pressed (REQ: scrollback-commit).
//
// Both read the same way, because to the transcript they are the same thing —
// the user's turn — and a reader scanning back through it should not have to
// know which kind of input a line came from to follow the conversation.
//
// It carries the input's own prompt, so the committed line reads exactly as the
// input line it replaces did.
func renderUserEcho(s string) string {
	return userStyle.Render(inputPrompt + s)
}

// --- styles ---
//
// Restated here rather than imported from internal/tui: those are that
// package's unexported vars, and the two packages stay independent.

var (
	userStyle = lipgloss.NewStyle().Faint(true)

	// replyStyle frames a bot message so the transcript reads as a conversation
	// rather than a wall of lines. The border is dimmed: it groups the message
	// with its buttons without competing with them for attention.
	//
	// Its characters are runes, not ANSI, so they survive a non-terminal writer
	// and appear in View() — which is why the tests that read committed blocks
	// look for their content rather than matching a whole frame.
	borderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// A button carries its state twice over: in glyphs, which any terminal
	// shows and a test can read, and in colour, which only a terminal shows.
	// The glyphs are the carrier of record (see focusedMarkLeft); these styles
	// are the part that makes it look like a button.
	buttonStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	inertButtonStyle   = lipgloss.NewStyle().Faint(true)
	focusedButtonStyle = lipgloss.NewStyle().Bold(true).Reverse(true)
	footerStyle        = lipgloss.NewStyle().Faint(true).Padding(0, 1)
	pendingStyle       = lipgloss.NewStyle().Faint(true)
	errStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

// errorPrefix names a committed failure as one (renderError). It carries the
// meaning on its own, without the colour errStyle adds: a transcript that has
// been piped, copied or redirected has no colour left in it, and the reader still
// has to be able to tell a failure from a reply.
const errorPrefix = "Error: "

// inputPrompt marks the input line, and with it the committed echo of a line
// submitted from there (renderUserEcho), so the two cannot drift apart.
const inputPrompt = "> "

// noFocus is a button position no button occupies. It renders a block with
// nothing focused, which is how a committed reply's inert buttons render.
const noFocus = -1

// The focused button is marked with glyphs rather than by colour alone. Colour
// is the wrong carrier for the only difference that matters here: a monochrome
// terminal drops it, and lipgloss emits none at all when it is not writing to a
// terminal, which would leave the mark invisible to a test. Both marks are two
// display cells wide, like the "[ " and " ]" they replace, so a row of buttons
// does not shift as focus moves along it.
const (
	focusedMarkLeft  = "▶ "
	focusedMarkRight = " ◀"
)

// The hint lines footerHelp chooses between, one per state the key table has.
// No one of them is a substring of another, so a test can assert both the hint a
// state shows and the hints it does not.
const (
	footerHelpInput     = "enter send · esc quit · ^c quit"
	footerHelpInputLive = "enter send · ↑ buttons · esc quit · ^c quit"
	footerHelpButtons   = "enter press · ↑↓←→ move · esc back · ^c quit"
	footerHelpPending   = "waiting for a reply · ^c quit"
	footerHelpPalette   = "enter run · ↑↓ move · esc dismiss · ^c quit"
)

// pendingLabel names what the typing indicator is waiting for. It sits beside
// the spinner: the words say what is happening, the motion says the session is
// working rather than hung (REQ: pending-is-visible).
const pendingLabel = "Sneat is typing…"
