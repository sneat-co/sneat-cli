// S10: restores chat-messenger's bot-keyboard behaviour (a Reply's
// Keyboard, e.g. /spaces' space-picker buttons) as a focusable
// transcript.Block, which the earlier tui/chatshell cutover left as plain
// text only (see the now-removed comment in handleSlash: "no chatshell
// equivalent in this MVP slice"). buttonsBlock renders a botkb.Keyboard's
// rows, arrow keys move a row/col cursor, and Enter activates the button
// under it via buttonActivatedMsg -- handler.OnMsg does the actual dispatch
// (chat.Processor.PressButton for a data button, chat.Processor.SendText for
// a text button), the same "Block emits, handler acts" shape as
// grid.RowActivatedMsg and controls.ItemActivatedMsg.
package chatapp

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/bots-go-framework/bots-go-core/botkb"
	"github.com/strongo/aichat/tui/transcript"
)

// buttonActivatedMsg is emitted when Enter is pressed over a buttonsBlock's
// cursor button.
type buttonActivatedMsg struct {
	Button botkb.Button
}

// buttonsBlock renders one Reply's Keyboard. It implements transcript.Block
// (not EntityBlock: a button is not a session.EntityRef).
type buttonsBlock struct {
	rows     [][]botkb.Button
	row, col int
}

// newButtonsBlock builds a buttonsBlock from kb, or nil when kb is nil, not
// a *botkb.MessageKeyboard (the only Keyboard implementation this codebase
// builds -- see internal/chat), or has no rows -- a caller checks for nil
// before calling AppendBlock, the same way it already checks r.Keyboard !=
// nil.
func newButtonsBlock(kb botkb.Keyboard) *buttonsBlock {
	mk, ok := kb.(*botkb.MessageKeyboard)
	if !ok || mk == nil || len(mk.Buttons) == 0 {
		return nil
	}
	return &buttonsBlock{rows: mk.Buttons}
}

func (b *buttonsBlock) Focusable() bool { return b.currentButton() != nil }

func (b *buttonsBlock) currentButton() botkb.Button {
	if b.row < 0 || b.row >= len(b.rows) {
		return nil
	}
	row := b.rows[b.row]
	if b.col < 0 || b.col >= len(row) {
		return nil
	}
	return row[b.col]
}

func (b *buttonsBlock) clampCol() {
	row := b.rows[b.row]
	if b.col >= len(row) {
		b.col = len(row) - 1
	}
	if b.col < 0 {
		b.col = 0
	}
}

func (b *buttonsBlock) Update(msg tea.Msg) (transcript.Block, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return b, nil
	}
	switch key.String() {
	case "up", "k":
		if b.row > 0 {
			b.row--
			b.clampCol()
		}
	case "down", "j":
		if b.row < len(b.rows)-1 {
			b.row++
			b.clampCol()
		}
	case "left", "h":
		if b.col > 0 {
			b.col--
		}
	case "right", "l":
		if b.row >= 0 && b.row < len(b.rows) && b.col < len(b.rows[b.row])-1 {
			b.col++
		}
	case "enter":
		if btn := b.currentButton(); btn != nil {
			return b, func() tea.Msg { return buttonActivatedMsg{Button: btn} }
		}
	}
	return b, nil
}

func (b *buttonsBlock) View(width int, focused bool) string {
	var sb strings.Builder
	for ri, row := range b.rows {
		if ri > 0 {
			sb.WriteString("\n")
		}
		for ci, btn := range row {
			label := "[ " + btn.GetText() + " ]"
			if focused && ri == b.row && ci == b.col {
				label = lipgloss.NewStyle().Reverse(true).Render(label)
			}
			sb.WriteString(label)
			if ci < len(row)-1 {
				sb.WriteString(" ")
			}
		}
	}
	return clampWidth(sb.String(), width)
}

// clampWidth matches internal/aichat/controls's own helper -- kept as a
// small unexported duplicate rather than exporting controls' version, since
// this package must not grow a dependency the other direction.
func clampWidth(s string, width int) string {
	if width <= 0 {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}
