package tui

import (
	"fmt"
	"os"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TreeRow is a row in the tree layout. Cells are displayed in the header's
// column order; the first cell is prefixed with the tree markers.
type TreeRow struct {
	Key   string
	Cells []string
}

// TreeChildrenFunc returns the children of the root with the given key.
type TreeChildrenFunc func(key string) []TreeRow

// TreeRefreshFunc reloads the roots. It returns the new roots and footer text.
type TreeRefreshFunc func() ([]TreeRow, string, error)

// TreeSortFunc re-sorts the roots when a user presses 's' (next sort field,
// reverse is false) or 'S' (reverse sort order, reverse is true). It returns
// the sorted roots and footer text.
type TreeSortFunc func(reverse bool) ([]TreeRow, string)

// TreeKeyFunc is called with the key of the selected row.
type TreeKeyFunc func(key string)

type treeLineKind int

const (
	treeLineRoot treeLineKind = iota
	treeLineChild
	treeLinePlaceholder
)

// treeLine is a visible line in the tree.
type treeLine struct {
	kind    treeLineKind
	key     string
	rootKey string
	row     TreeRow
	last    bool
	text    string
}

// Tree is a single list layout where roots can be expanded in place to show
// their children.
type Tree struct {
	screen  *Screen
	painter *tview.Pages
	view    *tview.Table
	footer  *tview.TextView
	search  *searchState

	title       string
	header      []string
	footerText  string
	helpText    string
	style       TableStyle
	initialKey  string
	summaryCol  int
	maxColWidth int

	roots    []TreeRow
	lines    []treeLine
	expanded map[string]bool
	children map[string][]TreeRow
	loading  map[string]bool
	gen      int

	childrenFunc     TreeChildrenFunc
	refreshFunc      TreeRefreshFunc
	sortFunc         TreeSortFunc
	viewFunc         ViewFunc
	openFunc         TreeKeyFunc
	copyURLFunc      TreeKeyFunc
	copyKeyFunc      TreeKeyFunc
	switchLayoutFunc SwitchLayoutFunc
	refreshing       bool
}

// TreeOption is a functional option for the tree layout.
type TreeOption func(*Tree)

// NewTree constructs a new tree layout.
func NewTree(opts ...TreeOption) *Tree {
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorDefault

	t := &Tree{
		screen:      NewScreen(),
		view:        tview.NewTable(),
		footer:      tview.NewTextView(),
		summaryCol:  -1,
		maxColWidth: 60,
		expanded:    make(map[string]bool),
		children:    make(map[string][]TreeRow),
		loading:     make(map[string]bool),
	}
	for _, opt := range opts {
		opt(t)
	}
	t.init()

	return t
}

// WithTreeTitle sets the title of the tree frame.
func WithTreeTitle(title string) TreeOption { return func(t *Tree) { t.title = title } }

// WithTreeHeader sets the column headers.
func WithTreeHeader(header []string) TreeOption { return func(t *Tree) { t.header = header } }

// WithTreeSummaryColumn sets the index of a wide column (eg: summary) that gets
// truncated at the max column width.
func WithTreeSummaryColumn(idx int) TreeOption { return func(t *Tree) { t.summaryCol = idx } }

// WithTreeFooterText sets the footer text.
func WithTreeFooterText(text string) TreeOption { return func(t *Tree) { t.footerText = text } }

// WithTreeHelpText sets the key help shown below the footer.
func WithTreeHelpText(text string) TreeOption { return func(t *Tree) { t.helpText = text } }

// WithTreeStyle sets the table style.
func WithTreeStyle(style TableStyle) TreeOption { return func(t *Tree) { t.style = style } }

// WithTreeInitialSelection selects the root with the given key on start.
func WithTreeInitialSelection(key string) TreeOption { return func(t *Tree) { t.initialKey = key } }

// WithTreeExpanded sets the map that tracks expanded roots. The tree updates
// it in place, so the caller can keep the expanded state across instances.
func WithTreeExpanded(expanded map[string]bool) TreeOption {
	return func(t *Tree) {
		if expanded != nil {
			t.expanded = expanded
		}
	}
}

// WithTreeChildrenFunc sets the function that loads the children of a root.
func WithTreeChildrenFunc(fn TreeChildrenFunc) TreeOption {
	return func(t *Tree) { t.childrenFunc = fn }
}

// WithTreeRefreshFunc sets the function called on 'r', 'CTRL+R' or 'F5'.
func WithTreeRefreshFunc(fn TreeRefreshFunc) TreeOption {
	return func(t *Tree) { t.refreshFunc = fn }
}

// WithTreeSortFunc sets the function called on 's' and 'S'.
func WithTreeSortFunc(fn TreeSortFunc) TreeOption { return func(t *Tree) { t.sortFunc = fn } }

// WithTreeViewFunc sets the function that renders the details of a row on 'v'.
func WithTreeViewFunc(fn ViewFunc) TreeOption { return func(t *Tree) { t.viewFunc = fn } }

// WithTreeOpenFunc sets the function called on ENTER.
func WithTreeOpenFunc(fn TreeKeyFunc) TreeOption { return func(t *Tree) { t.openFunc = fn } }

// WithTreeCopyURLFunc sets the function called on 'c'.
func WithTreeCopyURLFunc(fn TreeKeyFunc) TreeOption { return func(t *Tree) { t.copyURLFunc = fn } }

// WithTreeCopyKeyFunc sets the function called on 'CTRL+K'.
func WithTreeCopyKeyFunc(fn TreeKeyFunc) TreeOption { return func(t *Tree) { t.copyKeyFunc = fn } }

// WithTreeSwitchLayoutFunc sets the function called on 'V'.
func WithTreeSwitchLayoutFunc(fn SwitchLayoutFunc) TreeOption {
	return func(t *Tree) { t.switchLayoutFunc = fn }
}

// Paint paints the tree layout.
func (t *Tree) Paint(roots []TreeRow) error {
	t.roots = roots
	// Load the children of roots that start expanded.
	for key := range t.expanded {
		t.expand(key)
	}
	t.render()

	if len(t.lines) > 0 {
		t.view.Select(1, 0)
	}
	if t.initialKey != "" {
		t.selectKey(t.initialKey, t.initialKey)
	}

	setNotifier(t.footer)

	return t.screen.Paint(t.painter)
}

func (t *Tree) init() {
	t.footer.
		SetWordWrap(true).
		SetText(pad(t.footerText, 1)).
		SetTextColor(tcell.ColorDefault)

	help := tview.NewTextView().
		SetText(pad(t.helpText, 1)).
		SetTextColor(tcell.ColorGray)

	t.search = newSearchState(t.screen, t.footer, func() string { return t.footerText })

	t.view.
		SetSelectable(true, false).
		SetSelectedStyle(customTUIStyle(t.style)).
		SetFixed(1, 0).
		SetBorder(true).
		SetBorderColor(focusedBorderColor).
		SetTitleColor(focusedBorderColor).
		SetTitleAlign(tview.AlignLeft).
		SetTitle(fmt.Sprintf(" [::b]%s[::-] ", t.title))

	t.view.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEsc {
			t.screen.Stop()
		}
	})
	t.view.SetSelectedFunc(func(r, _ int) {
		if line, ok := t.lineAt(r); ok && line.kind != treeLinePlaceholder && t.openFunc != nil {
			t.openFunc(line.key)
		}
	})
	t.view.SetInputCapture(t.handleKey)

	grid := tview.NewGrid().
		SetRows(0, 1, 1).
		AddItem(t.view, 0, 0, 1, 1, 0, 0, true).
		AddItem(t.search.bottom, 1, 0, 1, 1, 0, 0, false).
		AddItem(help, 2, 0, 1, 1, 0, 0, false)

	t.painter = tview.NewPages().
		AddPage("primary", grid, true, true).
		AddPage("secondary", getInfoModal(), true, false)
}

func (t *Tree) handleKey(ev *tcell.EventKey) *tcell.EventKey {
	r, _ := t.view.GetSelection()
	line, ok := t.lineAt(r)

	switch ev.Key() {
	case tcell.KeyTab:
		if ok {
			t.toggle(line)
		}
		return nil
	case tcell.KeyRight:
		if ok && line.kind == treeLineRoot && !t.expanded[line.key] {
			t.toggle(line)
		}
		return nil
	case tcell.KeyLeft:
		if ok && (line.kind != treeLineRoot || t.expanded[line.key]) {
			t.collapse(line.rootKey)
		}
		return nil
	case tcell.KeyCtrlR, tcell.KeyF5:
		t.refresh()
		return nil
	case tcell.KeyCtrlK:
		if ok && line.kind != treeLinePlaceholder && t.copyKeyFunc != nil {
			t.copyKeyFunc(line.key)
		}
		return nil
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'q':
			t.screen.Stop()
			os.Exit(0)
		case 'l':
			if ok && line.kind == treeLineRoot && !t.expanded[line.key] {
				t.toggle(line)
			}
			return nil
		case 'h':
			if ok && (line.kind != treeLineRoot || t.expanded[line.key]) {
				t.collapse(line.rootKey)
			}
			return nil
		case 'r':
			t.refresh()
			return nil
		case 's', 'S':
			t.sort(ev.Rune() == 'S')
			return nil
		case 'v':
			if ok && line.kind != treeLinePlaceholder && t.viewFunc != nil {
				key := line.key
				showInPager(t.screen, t.painter, t.view, func() (string, error) {
					return t.viewFunc(key)
				})
			}
			return nil
		case 'V':
			if t.switchLayoutFunc != nil {
				key := ""
				if ok {
					key = line.rootKey
				}
				t.switchLayoutFunc(key)
				t.screen.Stop()
			}
			return nil
		case 'c':
			if ok && line.kind != treeLinePlaceholder && t.copyURLFunc != nil {
				t.copyURLFunc(line.key)
			}
			return nil
		case '/', '?', 'n', 'N':
			t.search.handleKey(t.view, ev.Rune())
			return nil
		}
	}
	return ev
}

// lineAt returns the visible line at the given table row.
func (t *Tree) lineAt(r int) (treeLine, bool) {
	i := r - 1 // Row 0 is the header.
	if i < 0 || i >= len(t.lines) {
		return treeLine{}, false
	}
	return t.lines[i], true
}

func (t *Tree) toggle(line treeLine) {
	if line.kind != treeLineRoot {
		t.collapse(line.rootKey)
		return
	}
	if t.expanded[line.key] {
		t.collapse(line.key)
		return
	}
	t.expand(line.key)
}

func (t *Tree) collapse(rootKey string) {
	delete(t.expanded, rootKey)
	t.render()
	t.selectKey(rootKey, "")
}

func (t *Tree) expand(rootKey string) {
	t.expanded[rootKey] = true

	if _, ok := t.children[rootKey]; !ok && !t.loading[rootKey] && t.childrenFunc != nil {
		t.loading[rootKey] = true
		gen := t.gen

		go func() {
			rows := t.childrenFunc(rootKey)

			t.screen.QueueUpdateDraw(func() {
				if gen != t.gen {
					return
				}
				delete(t.loading, rootKey)
				t.children[rootKey] = rows
				t.render()
			})
		}()
	}

	t.render()
}

func (t *Tree) refresh() {
	if t.refreshFunc == nil || t.refreshing {
		return
	}
	t.refreshing = true
	t.footer.SetText(pad("Refreshing...", 1))

	go func() {
		roots, footerText, err := t.refreshFunc()

		t.screen.QueueUpdateDraw(func() {
			t.refreshing = false
			if err != nil {
				t.footer.SetText(pad(fmt.Sprintf("Refresh failed: %s", err), 1))
				return
			}

			t.gen++
			t.children = make(map[string][]TreeRow)
			t.loading = make(map[string]bool)
			t.roots = roots
			t.setFooter(footerText)

			// Reload the children of roots that are still expanded.
			present := make(map[string]bool, len(roots))
			for _, root := range roots {
				present[root.Key] = true
			}
			for key := range t.expanded {
				if !present[key] {
					delete(t.expanded, key)
				}
			}
			for key := range t.expanded {
				t.expand(key)
			}
			t.render()
		})
	}()
}

func (t *Tree) sort(reverse bool) {
	if t.sortFunc == nil {
		return
	}
	roots, footerText := t.sortFunc(reverse)
	t.roots = roots
	t.setFooter(footerText)
	t.render()
}

func (t *Tree) setFooter(text string) {
	t.footerText = text
	t.footer.SetText(pad(text, 1))
}

// render rebuilds the table from the roots and expanded state, keeping the
// current selection where possible.
func (t *Tree) render() {
	var selKey, selRoot string
	if r, _ := t.view.GetSelection(); r > 0 {
		if line, ok := t.lineAt(r); ok {
			selKey, selRoot = line.key, line.rootKey
		}
	}

	t.lines = t.lines[:0]
	for _, root := range t.roots {
		t.lines = append(t.lines, treeLine{kind: treeLineRoot, key: root.Key, rootKey: root.Key, row: root})
		if !t.expanded[root.Key] {
			continue
		}
		if t.loading[root.Key] {
			t.lines = append(t.lines, treeLine{kind: treeLinePlaceholder, rootKey: root.Key, text: "Loading..."})
			continue
		}
		children := t.children[root.Key]
		if len(children) == 0 {
			t.lines = append(t.lines, treeLine{kind: treeLinePlaceholder, rootKey: root.Key, text: "No issues to show"})
			continue
		}
		for i, child := range children {
			t.lines = append(t.lines, treeLine{
				kind: treeLineChild, key: child.Key, rootKey: root.Key, row: child, last: i == len(children)-1,
			})
		}
	}

	t.view.Clear()
	for c, h := range t.header {
		t.view.SetCell(0, c, tview.NewTableCell(" "+h).
			SetStyle(tcell.StyleDefault.Bold(true)).
			SetSelectable(false).
			SetTextColor(tcell.ColorSnow).
			SetBackgroundColor(tcell.ColorDarkCyan))
	}
	for i, line := range t.lines {
		t.renderLine(i+1, line)
	}

	if selKey != "" || selRoot != "" {
		t.selectKey(selKey, selRoot)
	}
}

func (t *Tree) renderLine(r int, line treeLine) {
	switch line.kind {
	case treeLinePlaceholder:
		t.view.SetCell(r, 0, tview.NewTableCell(pad("    "+line.text, 1)).
			SetTextColor(tcell.ColorGray).
			SetSelectable(false))
		for c := 1; c < len(t.header); c++ {
			t.view.SetCell(r, c, tview.NewTableCell("").SetSelectable(false))
		}
		return
	case treeLineRoot:
		marker := "▸ "
		if t.expanded[line.key] {
			marker = "▾ "
		}
		t.setRowCells(r, line.row.Cells, marker, tcell.StyleDefault.Bold(true))
	case treeLineChild:
		marker := "  ├─ "
		if line.last {
			marker = "  └─ "
		}
		t.setRowCells(r, line.row.Cells, marker, tcell.StyleDefault)
	}
}

func (t *Tree) setRowCells(r int, cells []string, marker string, style tcell.Style) {
	for c := 0; c < len(t.header); c++ {
		text := ""
		if c < len(cells) {
			text = cells[c]
		}
		if c == 0 {
			text = marker + text
		}
		cell := tview.NewTableCell(pad(text, 1)).SetStyle(style)
		if c == t.summaryCol {
			cell.SetMaxWidth(t.maxColWidth)
		}
		t.view.SetCell(r, c, cell)
	}
}

// selectKey selects the line with the given key, or the given root when the
// key is no longer visible.
func (t *Tree) selectKey(key, rootKey string) {
	rootRow := -1
	for i, line := range t.lines {
		if line.kind == treeLinePlaceholder {
			continue
		}
		if key != "" && line.key == key && (rootKey == "" || line.rootKey == rootKey) {
			t.view.Select(i+1, 0)
			return
		}
		if line.kind == treeLineRoot && line.key == rootKey {
			rootRow = i + 1
		}
	}
	if rootRow > 0 {
		t.view.Select(rootRow, 0)
	}
}
