package tui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	searchDirForward  = 1
	searchDirBackward = -1
)

// searchState holds vi-like search state for a TUI layout.
type searchState struct {
	screen     *Screen
	footer     *tview.TextView
	footerText func() string

	input    *tview.InputField
	bottom   *tview.Pages
	target   *tview.Table
	term     string
	startRow int
	dir      int
	active   bool
}

// newSearchState creates search state for a layout. The returned bottom pages
// should be placed where the footer would go; the search prompt replaces the
// footer while searching. footerText returns the footer's normal text.
func newSearchState(screen *Screen, footer *tview.TextView, footerText func() string) *searchState {
	input := tview.NewInputField().
		SetLabel("/").
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetLabelColor(tcell.ColorYellow)

	ss := &searchState{
		screen:     screen,
		footer:     footer,
		footerText: footerText,
		input:      input,
		bottom: tview.NewPages().
			AddPage("footer", footer, true, true).
			AddPage("search", input, true, false),
		dir: searchDirForward,
	}
	ss.init()

	return ss
}

func (ss *searchState) init() {
	ss.input.
		SetChangedFunc(func(text string) {
			// Incremental search from where the search started.
			if text == "" || ss.target == nil {
				return
			}
			r, ok := findRow(ss.target, text, ss.startRow, ss.dir, true)
			if !ok {
				r = ss.startRow
			}
			if cur, _ := ss.target.GetSelection(); cur != r {
				ss.target.Select(r, 0)
			}
		}).
		SetDoneFunc(func(key tcell.Key) {
			target := ss.target
			text := ss.input.GetText()

			ss.active = false
			ss.bottom.SwitchToPage("footer")
			ss.screen.SetFocus(target)

			switch key {
			case tcell.KeyEnter:
				if text == "" {
					return
				}
				ss.term = text
				ss.jump(target, ss.startRow, ss.dir, true)
			case tcell.KeyEscape:
				// Cancel: restore the original selection.
				target.Select(ss.startRow, 0)
			}
		})
}

// start opens the search prompt for the given table.
func (ss *searchState) start(target *tview.Table, dir int) {
	r, _ := target.GetSelection()

	ss.target = target
	ss.startRow = r
	ss.dir = dir
	ss.input.SetLabel(map[int]string{searchDirForward: "/", searchDirBackward: "?"}[dir])
	ss.input.SetText("")
	ss.active = true
	ss.bottom.SwitchToPage("search")
	ss.screen.SetFocus(ss.input)
}

// next jumps to the next (or previous when reverse is set) match of the
// last search term in the given table, like vi's 'n' and 'N'.
func (ss *searchState) next(target *tview.Table, reverse bool) {
	if ss.term == "" {
		return
	}
	dir := ss.dir
	if reverse {
		dir = -dir
	}
	r, _ := target.GetSelection()
	ss.jump(target, r, dir, false)
}

func (ss *searchState) jump(target *tview.Table, from, dir int, includeFrom bool) {
	r, ok := findRow(target, ss.term, from, dir, includeFrom)
	if !ok {
		ss.footer.SetText(pad(fmt.Sprintf("Pattern not found: %s", ss.term), 1))
		if cur, _ := target.GetSelection(); cur != from {
			target.Select(from, 0)
		}
		return
	}
	ss.footer.SetText(pad(ss.footerText(), 1))
	target.Select(r, 0)
}

// handleKey handles the search keys ('/', '?', 'n', 'N') for the given table.
// It reports whether the key was handled.
func (ss *searchState) handleKey(target *tview.Table, r rune) bool {
	switch r {
	case '/':
		ss.start(target, searchDirForward)
	case '?':
		ss.start(target, searchDirBackward)
	case 'n':
		ss.next(target, false)
	case 'N':
		ss.next(target, true)
	default:
		return false
	}
	return true
}

// findRow finds the next selectable row containing term, starting after (or at,
// when includeFrom is set) row from and wrapping around. The match is case
// insensitive unless term contains an uppercase letter (vi's smartcase).
func findRow(t *tview.Table, term string, from, dir int, includeFrom bool) (int, bool) {
	rows := t.GetRowCount()
	if rows == 0 || term == "" {
		return 0, false
	}

	caseSensitive := strings.IndexFunc(term, unicode.IsUpper) >= 0
	if !caseSensitive {
		term = strings.ToLower(term)
	}

	start := 1
	if includeFrom {
		start = 0
	}
	for i := start; i <= rows; i++ {
		r := ((from+dir*i)%rows + rows) % rows

		first := t.GetCell(r, 0)
		if first == nil || first.NotSelectable {
			continue
		}

		var sb strings.Builder
		for c := 0; c < t.GetColumnCount(); c++ {
			if cell := t.GetCell(r, c); cell != nil {
				sb.WriteString(cell.Text)
				sb.WriteByte(' ')
			}
		}
		text := sb.String()
		if !caseSensitive {
			text = strings.ToLower(text)
		}
		if strings.Contains(text, term) {
			return r, true
		}
	}
	return 0, false
}
