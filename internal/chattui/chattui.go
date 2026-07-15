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

	// pending reports whether a reply is in flight. While set, keys other than
	// ctrl+c are ignored (REQ: input-locked-while-pending).
	pending bool

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
	in.Prompt = "> "
	in.Focus()
	return Model{
		proc:  proc,
		input: in,
		focus: focusInput,
	}
}

// Init starts the input's cursor blinking.
func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles the terminal's size and the always-available quit, and passes
// everything else to the input.
//
// TODO(chat-tui Task 4): the key table (focus movement, enter, esc) and the
// pending lock belong here and are not implemented — every key other than
// ctrl+c currently reaches the input regardless of focus or pending.
// TODO(chat-tui Task 3): submitting text must commit the user's line to
// scrollback and run proc.SendText as a tea.Cmd.
// TODO(chat-tui Task 5): pressing a focused button must commit the live reply
// and an echo of the button's label, then run proc.PressButton.
// TODO(chat-tui Task 6): a Processor error must be rendered as a bot message in
// the transcript, leaving the session running.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		// ctrl+c quits from any focus, and even while a reply is in flight: a
		// slow backend must never trap the user (REQ: input-locked-while-pending).
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// View renders the live region — the only part of the chat that is repainted.
// Everything above it is committed scrollback the terminal owns
// (REQ: scrollback-commit).
//
// TODO(chat-tui Task 3): the live reply's text and its focusable buttons belong
// at the top of this region and are not rendered; only the input and the footer
// hint are.
func (m Model) View() string {
	return m.input.View() + "\n" + footerStyle.Render(footerHelp)
}

// --- styles ---
//
// Restated here rather than imported from internal/tui: those are that
// package's unexported vars, and the two packages stay independent.

var footerStyle = lipgloss.NewStyle().Faint(true).Padding(0, 1)

// footerHelp is the live region's hint line (REQ: inline-rendering).
//
// TODO(chat-tui Task 4): the focus and button keys this must name are not
// implemented, so it lists only what works.
const footerHelp = "enter send · ^c quit"
