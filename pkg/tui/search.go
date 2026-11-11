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

// searchState holds vi-like search state for the preview layout.
type searchState struct {
	input    *tview.InputField
	bottom   *tview.Pages
	target   *tview.Table
	term     string
	startRow int
	dir      int
	active   bool
}

func newSearchState(footer tview.Primitive) *searchState {
	input := tview.NewInputField().
		SetLabel("/").
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetLabelColor(tcell.ColorYellow)

	return &searchState{
		input: input,
		bottom: tview.NewPages().
			AddPage("footer", footer, true, true).
			AddPage("search", input, true, false),
		dir: searchDirForward,
	}
}

func (pv *Preview) initSearch() {
	pv.search.input.
		SetChangedFunc(func(text string) {
			// Incremental search from where the search started.
			if text == "" || pv.search.target == nil {
				return
			}
			r, ok := findRow(pv.search.target, text, pv.search.startRow, pv.search.dir, true)
			if !ok {
				r = pv.search.startRow
			}
			if cur, _ := pv.search.target.GetSelection(); cur != r {
				pv.search.target.Select(r, 0)
			}
		}).
		SetDoneFunc(func(key tcell.Key) {
			target := pv.search.target
			text := pv.search.input.GetText()

			pv.search.active = false
			pv.search.bottom.SwitchToPage("footer")
			pv.screen.SetFocus(target)

			switch key {
			case tcell.KeyEnter:
				if text == "" {
					return
				}
				pv.search.term = text
				pv.searchJump(target, pv.search.startRow, pv.search.dir, true)
			case tcell.KeyEscape:
				// Cancel: restore the original selection.
				target.Select(pv.search.startRow, 0)
			}
		})
}

// startSearch opens the search prompt for the given table.
func (pv *Preview) startSearch(target *tview.Table, dir int) {
	r, _ := target.GetSelection()

	pv.search.target = target
	pv.search.startRow = r
	pv.search.dir = dir
	pv.search.input.SetLabel(map[int]string{searchDirForward: "/", searchDirBackward: "?"}[dir])
	pv.search.input.SetText("")
	pv.search.active = true
	pv.search.bottom.SwitchToPage("search")
	pv.screen.SetFocus(pv.search.input)
}

// searchNext jumps to the next (or previous when reverse is set) match of the
// last search term in the given table, like vi's 'n' and 'N'.
func (pv *Preview) searchNext(target *tview.Table, reverse bool) {
	if pv.search.term == "" {
		return
	}
	dir := pv.search.dir
	if reverse {
		dir = -dir
	}
	r, _ := target.GetSelection()
	pv.searchJump(target, r, dir, false)
}

func (pv *Preview) searchJump(target *tview.Table, from, dir int, includeFrom bool) {
	r, ok := findRow(target, pv.search.term, from, dir, includeFrom)
	if !ok {
		pv.footer.SetText(pad(fmt.Sprintf("Pattern not found: %s", pv.search.term), 1))
		if cur, _ := target.GetSelection(); cur != from {
			target.Select(from, 0)
		}
		return
	}
	pv.footer.SetText(pad(pv.footerText, 1))
	target.Select(r, 0)
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
