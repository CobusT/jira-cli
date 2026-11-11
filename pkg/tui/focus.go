package tui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	focusedBorderColor   = tcell.ColorYellow
	unfocusedBorderColor = tcell.ColorDarkGray
)

// paneStyle tracks how a framed pane looks when it has focus and when it doesn't.
type paneStyle struct {
	view          *tview.Table
	title         string
	selectedStyle tcell.Style
}

// unfocusedSelectedStyle dims the selected row of a pane without focus, so it's
// still visible which row is selected but clear that keys go elsewhere.
var unfocusedSelectedStyle = tcell.StyleDefault.
	Background(tcell.ColorDefault).
	Foreground(tcell.ColorGray)

func (p *paneStyle) apply(focused bool) {
	if focused {
		p.view.SetBorderColor(focusedBorderColor).
			SetTitleColor(focusedBorderColor).
			SetTitle(fmt.Sprintf(" [::b]▶ %s[::-] ", p.title))
		p.view.SetSelectedStyle(p.selectedStyle)
		return
	}
	p.view.SetBorderColor(unfocusedBorderColor).
		SetTitleColor(tcell.ColorGray).
		SetTitle(fmt.Sprintf(" %s ", p.title))
	p.view.SetSelectedStyle(unfocusedSelectedStyle)
}

// initPanes frames the sidebar and contents and highlights whichever has focus.
func (pv *Preview) initPanes() {
	selected := customTUIStyle(pv.contents.style)

	pv.sidebarPane = &paneStyle{view: pv.sidebar, title: pv.sidebarTitle, selectedStyle: selected}
	pv.contentsPane = &paneStyle{view: pv.contents.view, title: pv.contentsTitle, selectedStyle: selected}

	for _, p := range []*paneStyle{pv.sidebarPane, pv.contentsPane} {
		p := p
		p.view.SetBorder(true).SetTitleAlign(tview.AlignLeft)
		p.view.SetFocusFunc(func() { p.apply(true) })
		p.view.SetBlurFunc(func() {
			// Keep the pane highlighted while its search prompt is open.
			if pv.search != nil && pv.search.target == p.view && pv.search.active {
				return
			}
			p.apply(false)
		})
	}

	// The sidebar has focus initially.
	pv.sidebarPane.apply(true)
	pv.contentsPane.apply(false)
}
