package tui

import (
	"fmt"
	"os"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const sidebarMaxWidth = 60

// PreviewData is the data to be shown in preview layout.
type PreviewData struct {
	Key      string
	Menu     string
	Contents func(string) interface{}
}

// PreviewRefreshFunc reloads the preview data. It returns the new data and
// the footer text to display with it.
type PreviewRefreshFunc func() ([]PreviewData, string, error)

// PreviewSortFunc re-sorts the preview data when a user presses 's' (next sort
// field, reverse is false) or 'S' (reverse sort order, reverse is true). It
// returns the sorted data and the footer text to display with it.
type PreviewSortFunc func(reverse bool) ([]PreviewData, string)

// ContentsSortFunc changes the sort of the contents pane when a user presses
// 's' (next sort field) or 'S' (reverse sort order) there. It returns the
// footer text to display; the contents are then reloaded with Contents.
type ContentsSortFunc func(reverse bool) string

// Preview is the preview layout.
//
// It contains 2 tables internally, viz: sidebar and contents.
type Preview struct {
	screen              *Screen
	painter             *tview.Pages
	sidebar             *tview.Table
	contents            *Table
	footer              *tview.TextView
	data                []PreviewData
	initialText         string
	footerText          string
	sidebarSelectedFunc SelectedFunc
	contentsCache       map[string]interface{}
	refreshFunc         PreviewRefreshFunc
	refreshing          bool
	sortFunc            PreviewSortFunc
	contentsSortFunc    ContentsSortFunc
	// reselectKey is the key of the contents row to select once the
	// contents are reloaded after a sort.
	reselectKey      string
	sidebarTitle     string
	contentsTitle    string
	sidebarPane      *paneStyle
	sidebarViewFunc  ViewFunc
	switchLayoutFunc SwitchLayoutFunc
	initialKey       string
	contentsPane     *paneStyle
	cacheGen         int
	search           *searchState
	rowActions       map[rune]RowAction
	dataFunc         func() ([]PreviewData, string)
	actions          *actionHost
}

// PreviewOption is a functional option that wraps preview properties.
type PreviewOption func(*Preview)

// NewPreview constructs a new preview layout.
func NewPreview(opts ...PreviewOption) *Preview {
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorDefault

	pv := Preview{
		screen:        NewScreen(),
		sidebar:       tview.NewTable(),
		contents:      NewTable(),
		footer:        tview.NewTextView(),
		contentsCache: make(map[string]interface{}),
	}
	for _, opt := range opts {
		opt(&pv)
	}
	pv.init()

	return &pv
}

// WithInitialText sets initial text that is displayed in the contents screen.
func WithInitialText(text string) PreviewOption {
	return func(p *Preview) {
		p.initialText = text
	}
}

// WithPreviewFooterText sets footer text that is displayed after the preview layout.
func WithPreviewFooterText(text string) PreviewOption {
	return func(p *Preview) {
		p.footerText = text
	}
}

// WithSidebarSelectedFunc sets a function that is called when any option in sidebar is selected.
func WithSidebarSelectedFunc(fn SelectedFunc) PreviewOption {
	return func(p *Preview) {
		p.sidebarSelectedFunc = fn
	}
}

// WithContentTableOpts sets contents table options.
func WithContentTableOpts(opts ...TableOption) PreviewOption {
	return func(p *Preview) {
		for _, opt := range opts {
			opt(p.contents)
		}
	}
}

// WithPreviewRefreshFunc sets a function that is called to reload the preview
// data when a user presses 'r', 'CTRL+R' or 'F5'.
func WithPreviewRefreshFunc(fn PreviewRefreshFunc) PreviewOption {
	return func(p *Preview) {
		p.refreshFunc = fn
	}
}

// ViewFunc returns the rendered details of the item with the given key.
type ViewFunc func(key string) (string, error)

// SwitchLayoutFunc is called with the selected key when a user presses 'V' to
// switch to another layout. The current layout stops after it is called.
type SwitchLayoutFunc func(selectedKey string)

// WithSidebarViewFunc sets a function that renders the details of the selected
// sidebar item when a user presses 'v' in the sidebar.
func WithSidebarViewFunc(fn ViewFunc) PreviewOption {
	return func(p *Preview) {
		p.sidebarViewFunc = fn
	}
}

// WithPreviewSwitchLayoutFunc sets a function that is called when a user presses 'V'.
func WithPreviewSwitchLayoutFunc(fn SwitchLayoutFunc) PreviewOption {
	return func(p *Preview) {
		p.switchLayoutFunc = fn
	}
}

// WithInitialSelection selects the sidebar item with the given key on start.
func WithInitialSelection(key string) PreviewOption {
	return func(p *Preview) {
		p.initialKey = key
	}
}

// WithPaneTitles sets the titles shown in the sidebar and contents frames.
func WithPaneTitles(sidebar, contents string) PreviewOption {
	return func(p *Preview) {
		p.sidebarTitle = sidebar
		p.contentsTitle = contents
	}
}

// WithPreviewSortFunc sets a function that is called to re-sort the sidebar
// when a user presses 's' or 'S'.
func WithPreviewSortFunc(fn PreviewSortFunc) PreviewOption {
	return func(p *Preview) {
		p.sortFunc = fn
	}
}

// WithContentsSortFunc sets a function that is called to re-sort the contents
// pane when a user presses 's' or 'S' there. Without it, the keys sort the sidebar.
func WithContentsSortFunc(fn ContentsSortFunc) PreviewOption {
	return func(p *Preview) {
		p.contentsSortFunc = fn
	}
}

// WithPreviewRowActions sets actions run when a user presses their key in
// either pane. In the sidebar the target is the selected item; in the contents
// pane it is the selected row, keyed by its KEY column.
func WithPreviewRowActions(actions map[rune]RowAction) PreviewOption {
	return func(p *Preview) {
		p.rowActions = actions
	}
}

// WithPreviewDataFunc sets a function that returns the current data and footer
// text. Row actions use it to reload the layout after a change.
func WithPreviewDataFunc(fn func() ([]PreviewData, string)) PreviewOption {
	return func(p *Preview) {
		p.dataFunc = fn
	}
}

// previewUI is the ActionUI of the preview layout.
type previewUI struct {
	*actionHost
	pv *Preview
}

func (u previewUI) Reload(_ []string, selectKey string) {
	u.pv.reload(selectKey)
}

// reload rebuilds the sidebar from the data func and reloads the contents,
// selecting the contents row with selectKey when the contents pane has focus.
func (pv *Preview) reload(selectKey string) {
	if pv.dataFunc == nil {
		return
	}
	data, footerText := pv.dataFunc()
	pv.contentsCache = make(map[string]interface{})
	pv.cacheGen++
	if pv.screen.GetFocus() == pv.contents.view {
		pv.reselectKey = selectKey
	}
	pv.setData(data, footerText, false)
}

// runRowAction runs the row action for the key, if any. It reports whether
// the key was handled.
func (pv *Preview) runRowAction(r rune, inContents bool) bool {
	action, ok := pv.rowActions[r]
	if !ok {
		return false
	}
	sr, _ := pv.sidebar.GetSelection()
	if sr <= 0 || sr >= len(pv.data) {
		return true // The help item.
	}
	parent := pv.data[sr].Key
	target := RowTarget{Key: parent, Parent: parent, IsParent: true}

	if inContents {
		target = RowTarget{Parent: parent}
		if data, ok := pv.contentsCache[parent].(TableData); ok {
			r, _ := pv.contents.view.GetSelection()
			if idx := data.GetIndex("KEY"); idx >= 0 && r > 0 && r < len(data) {
				target.Key = data.Get(r, idx)
			}
		}
	}
	action(previewUI{actionHost: pv.actions, pv: pv}, target)
	return true
}

// Paint paints the preview layout.
func (pv *Preview) Paint(pd []PreviewData) error {
	if len(pd) == 0 {
		return errNoData
	}

	pv.data = pd

	pv.sidebar.SetSelectionChangedFunc(func(r, c int) {
		pv.loadContents(r)
	})

	if pv.sidebarSelectedFunc != nil {
		pv.sidebar.SetSelectedFunc(func(r, c int) {
			if r > 0 && r < len(pv.data) {
				pv.sidebarSelectedFunc(r, c, pv.data[r])
			}
		})
	}

	pv.renderSidebar()
	pv.printText(pv.initialText)

	if pv.initialKey != "" {
		for i, d := range pv.data {
			if d.Key == pv.initialKey {
				pv.sidebar.Select(i, 0)
				break
			}
		}
	}

	setNotifier(pv.footer)

	return pv.screen.Paint(pv.painter)
}

func (pv *Preview) renderSidebar() {
	pv.sidebar.Clear()

	for i, d := range pv.data {
		style := tcell.StyleDefault
		if i == 0 {
			style = style.Bold(true)
		}

		cell := tview.NewTableCell(pad(d.Menu, 1)).
			SetMaxWidth(sidebarMaxWidth - 2).
			SetStyle(style)

		pv.sidebar.SetCell(i, 0, cell)
	}
}

// refresh reloads the preview data using the refresh func, clears the
// contents cache and re-renders the sidebar while keeping the current selection.
func (pv *Preview) refresh() {
	if pv.refreshFunc == nil || pv.refreshing {
		return
	}
	pv.refreshing = true
	pv.footer.SetText(pad("Refreshing...", 1))

	go func() {
		data, footerText, err := pv.refreshFunc()

		pv.screen.QueueUpdateDraw(func() {
			pv.refreshing = false

			if err != nil {
				pv.footer.SetText(pad(fmt.Sprintf("Refresh failed: %s", err), 1))
				return
			}
			if len(data) == 0 {
				pv.footer.SetText(pad("Refresh returned no results; showing previous data", 1))
				return
			}

			pv.setData(data, footerText, true)
		})
	}()
}

func (pv *Preview) switchLayout() {
	if pv.switchLayoutFunc == nil {
		return
	}
	var key string
	if sr, _ := pv.sidebar.GetSelection(); sr >= 0 && sr < len(pv.data) {
		key = pv.data[sr].Key
	}
	pv.switchLayoutFunc(key)
	pv.screen.Stop()
}

// sort re-sorts the sidebar using the sort func. When reverse is set the sort
// order is reversed, otherwise the next sort field is selected.
func (pv *Preview) sort(reverse bool) {
	if pv.sortFunc == nil {
		return
	}
	data, footerText := pv.sortFunc(reverse)
	pv.setData(data, footerText, false)
}

// sortContents re-sorts the contents pane using the contents sort func and
// reloads the contents, keeping the selected row's key selected.
func (pv *Preview) sortContents(reverse bool) {
	if pv.contentsSortFunc == nil {
		pv.sort(reverse)
		return
	}
	sr, _ := pv.sidebar.GetSelection()
	if sr < 0 || sr >= len(pv.data) {
		return
	}
	pv.reselectKey = ""
	if data, ok := pv.contentsCache[pv.data[sr].Key].(TableData); ok {
		r, _ := pv.contents.view.GetSelection()
		if idx := data.GetIndex("KEY"); idx >= 0 && r > 0 && r < len(data) {
			pv.reselectKey = data.Get(r, idx)
		}
	}

	pv.setFooter(pv.contentsSortFunc(reverse))
	pv.contentsCache = make(map[string]interface{})
	pv.cacheGen++
	pv.loadContents(sr)
}

func (pv *Preview) setFooter(text string) {
	pv.footerText = text
	pv.footer.SetText(pad(text, 1))
}

// setData replaces the sidebar data while keeping the current selection. It
// must be called on the UI goroutine. When resetCache is set, cached contents
// are discarded.
func (pv *Preview) setData(data []PreviewData, footerText string, resetCache bool) {
	var selectedKey string
	if sr, _ := pv.sidebar.GetSelection(); sr >= 0 && sr < len(pv.data) {
		selectedKey = pv.data[sr].Key
	}

	pv.data = data
	if resetCache {
		pv.contentsCache = make(map[string]interface{})
		pv.cacheGen++
	}
	pv.setFooter(footerText)
	pv.renderSidebar()

	row := 0
	for i, d := range data {
		if d.Key == selectedKey {
			row = i
			break
		}
	}
	// Select always fires the selection changed func, which reloads the contents.
	pv.sidebar.Select(row, 0)
}

// loadContents shows the contents for the given sidebar row. It must be called
// on the UI goroutine. Uncached contents are fetched in the background; the
// cache is only touched and the result only drawn on the UI goroutine, and a
// result is dropped if the selection moved on or the data was refreshed meanwhile.
func (pv *Preview) loadContents(r int) {
	if r < 0 || r >= len(pv.data) {
		return
	}
	pd := pv.data[r]

	pv.contents.view.Clear()
	if pd.Contents == nil {
		pv.printText("No contents defined.")
		return
	}
	if v, ok := pv.contentsCache[pd.Key]; ok {
		pv.showContents(v)
		return
	}

	pv.printText("Loading...")

	gen := pv.cacheGen
	go func() {
		v := pd.Contents(pd.Key)

		pv.screen.QueueUpdateDraw(func() {
			if gen != pv.cacheGen {
				return
			}
			pv.contentsCache[pd.Key] = v

			if sr, _ := pv.sidebar.GetSelection(); sr >= 0 && sr < len(pv.data) && pv.data[sr].Key == pd.Key {
				pv.contents.view.Clear()
				pv.showContents(v)
			}
		})
	}()
}

func (pv *Preview) showContents(v interface{}) {
	switch data := v.(type) {
	case string:
		pv.printText(data)
	case TableData:
		if len(data) == 1 {
			pv.printText("No results to show.")
			return
		}
		pv.contents.render(data)
		pv.applyReselect(data)
	}
}

// applyReselect selects the contents row with the key saved by sortContents.
func (pv *Preview) applyReselect(data TableData) {
	key := pv.reselectKey
	if key == "" {
		return
	}
	pv.reselectKey = ""
	idx := data.GetIndex("KEY")
	if idx < 0 {
		return
	}
	for r := 1; r < len(data); r++ {
		if data.Get(r, idx) == key {
			pv.contents.view.Select(r, 0)
			return
		}
	}
	pv.contents.view.Select(1, 0)
}

func (pv *Preview) init() {
	pv.initSidebar()
	pv.initContents()
	pv.initFooter()
	pv.search = newSearchState(pv.screen, pv.footer, func() string { return pv.footerText })

	grid := tview.NewGrid().
		SetRows(0, 1, 2).
		SetColumns(sidebarMaxWidth, 1, 0).
		AddItem(pv.sidebar, 0, 0, 2, 1, 0, 0, true).
		AddItem(pv.contents.view, 0, 2, 2, 1, 0, 0, false).
		AddItem(pv.search.bottom, 2, 0, 1, 3, 0, 0, false)

	pv.painter = tview.NewPages().
		AddPage("primary", grid, true, true).
		AddPage("secondary", getInfoModal(), true, false)

	pv.actions = &actionHost{
		screen:  pv.screen,
		pages:   pv.painter,
		footer:  pv.footer,
		restore: func() { pv.footer.SetText(pad(pv.footerText, 1)) },
	}

	pv.initLayout(pv.sidebar)
	pv.initLayout(pv.contents.view)
	pv.initPanes()
}

func (pv *Preview) initSidebar() {
	pv.sidebar.
		SetSelectable(true, false).
		SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
			if ev.Key() == tcell.KeyCtrlR || ev.Key() == tcell.KeyF5 {
				pv.refresh()
				return nil
			}
			if ev.Key() == tcell.KeyTab {
				pv.screen.SetFocus(pv.contents.view)
				pv.contents.view.SetSelectable(true, false).Select(1, 0)
			}
			if ev.Key() == tcell.KeyRune && pv.runRowAction(ev.Rune(), false) {
				return nil
			}
			if ev.Key() == tcell.KeyRune {
				switch ev.Rune() {
				case 'q':
					pv.screen.Stop()
					os.Exit(0)
				case 'w':
					pv.screen.SetFocus(pv.contents.view)
					pv.contents.view.SetSelectable(true, false).Select(1, 0)
				case 'r':
					pv.refresh()
					return nil
				case 's':
					pv.sort(false)
					return nil
				case 'S':
					pv.sort(true)
					return nil
				case 'V':
					pv.switchLayout()
					return nil
				case 'v':
					sr, _ := pv.sidebar.GetSelection()
					if pv.sidebarViewFunc == nil || sr <= 0 || sr >= len(pv.data) {
						return nil
					}
					key := pv.data[sr].Key
					showInPager(pv.screen, pv.painter, pv.sidebar, func() (string, error) {
						return pv.sidebarViewFunc(key)
					})
					return nil
				case '/', '?', 'n', 'N':
					pv.search.handleKey(pv.sidebar, ev.Rune())
					return nil
				default:
					pv.contents.view.SetSelectable(false, false)
				}
			}
			return ev
		})
}

func (pv *Preview) initContents() {
	pv.contents.view.
		SetBorder(true).
		SetBorderColor(tcell.ColorDarkGray).
		SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
			contents := func() interface{} {
				sr, _ := pv.sidebar.GetSelection()
				return pv.contentsCache[pv.data[sr].Key]
			}
			if ev.Key() == tcell.KeyCtrlR || ev.Key() == tcell.KeyF5 {
				pv.refresh()
				return nil
			}
			if ev.Key() == tcell.KeyCtrlK {
				if pv.contents.copyKeyFunc == nil {
					return ev
				}
				r, c := pv.contents.view.GetSelection()
				pv.contents.copyKeyFunc(r, c, contents())
			}
			if ev.Key() == tcell.KeyTab {
				pv.screen.SetFocus(pv.sidebar)
				pv.contents.view.SetSelectable(false, false)
			}
			if ev.Key() == tcell.KeyRune && pv.runRowAction(ev.Rune(), true) {
				return nil
			}
			if ev.Key() == tcell.KeyRune {
				switch ev.Rune() {
				case 'q':
					pv.screen.Stop()
					os.Exit(0)
				case 'w':
					pv.screen.SetFocus(pv.sidebar)
					pv.contents.view.SetSelectable(false, false)
				case 'r':
					pv.refresh()
					return nil
				case 's':
					pv.sortContents(false)
					return nil
				case 'S':
					pv.sortContents(true)
					return nil
				case 'V':
					pv.switchLayout()
					return nil
				case '/', '?', 'n', 'N':
					pv.search.handleKey(pv.contents.view, ev.Rune())
					return nil
				case 'c':
					if pv.contents.copyFunc == nil {
						break
					}
					r, c := pv.contents.view.GetSelection()
					pv.contents.copyFunc(r, c, contents())
				case 'v':
					if pv.contents.viewModeFunc == nil {
						break
					}
					sr, _ := pv.sidebar.GetSelection()
					r, c := pv.contents.view.GetSelection()
					contents := pv.contentsCache[pv.data[sr].Key]
					dataFn, renderFn := pv.contents.viewModeFunc(r, c, contents)

					showInPager(pv.screen, pv.painter, pv.contents.view, func() (string, error) {
						return renderFn(dataFn())
					})
				}
			}
			return ev
		})
}

func (pv *Preview) initFooter() {
	pv.footer.
		SetWordWrap(true).
		SetText(pad(pv.footerText, 1)).
		SetTextColor(tcell.ColorDefault)
}

func (pv *Preview) initLayout(view *tview.Table) {
	view.SetSelectedStyle(customTUIStyle(pv.contents.style)).
		SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEsc {
				pv.screen.Stop()
			}
		})

	view.SetFixed(1, int(pv.contents.colFixed))
}

func (pv *Preview) printText(s string) {
	lines := splitText(s)
	for i, line := range lines {
		pv.contents.view.SetCell(i, 0, tview.NewTableCell(pad(line, 1)).
			SetStyle(tcell.StyleDefault).
			SetSelectable(false))
	}
}
