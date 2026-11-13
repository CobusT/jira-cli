package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	shellquote "github.com/kballard/go-shellquote"
	"github.com/rivo/tview"

	"github.com/ankitpokhrel/jira-cli/pkg/surveyext"
)

const modalPage = "modal"

// RowTarget is the row a row action acts on.
type RowTarget struct {
	// Key is the key of the selected row; it is empty when a parent has no
	// children to select.
	Key string
	// Parent is the key of the parent row, eg: the epic of an issue. For a
	// parent row, it is the row's own key.
	Parent string
	// IsParent reports whether the selected row is a parent row, eg: an epic.
	IsParent bool
}

// RowAction is run when a user presses its key on a row.
type RowAction func(ui ActionUI, row RowTarget)

// PickerItem is an item in a picker.
type PickerItem struct {
	Label string
	Value any
}

// Picker describes a list a user picks an item from.
type Picker struct {
	Title string
	// Items are filtered as the user types.
	Items []PickerItem
	// Search, when set, is called as the user types to fetch matching items
	// instead of filtering Items. It runs in the background.
	Search func(query string) ([]PickerItem, error)
	// Fixed items are always listed first, eg: "Unassigned".
	Fixed  []PickerItem
	OnPick func(PickerItem)
}

// FormField is a field in a form.
type FormField struct {
	Name  string
	Label string
	Value string
	// Options turns the field into a drop-down.
	Options []string
	// Numeric only accepts numbers.
	Numeric bool
}

// Form describes a form a user fills in.
type Form struct {
	Title  string
	Fields []FormField
	// OnSubmit is called with the field values by name.
	OnSubmit func(values map[string]string)
}

// ActionUI lets a row action interact with the user. All methods must be
// called on the UI goroutine; callbacks are called on the UI goroutine.
type ActionUI interface {
	// Run runs fn in the background, showing status in the footer, then
	// calls done with its error. Only one Run is active at a time.
	Run(status string, fn func() error, done func(error))
	// Pick shows a picker.
	Pick(p Picker)
	// Form shows a form.
	Form(f Form)
	// EditText opens text in the user's editor and calls done with the result.
	EditText(text string, done func(string, error))
	// Notify shows a message in the footer.
	Notify(msg string)
	// Reload reloads the data, re-fetching the children of the given parents,
	// and selects the row with selectKey when it is visible.
	Reload(parents []string, selectKey string)
}

// actionHost implements the parts of ActionUI shared by the layouts.
type actionHost struct {
	screen  *Screen
	pages   *tview.Pages
	footer  *tview.TextView
	busy    bool
	restore func() // restores the footer text
}

func (h *actionHost) Notify(msg string) {
	h.footer.SetText(pad(msg, 1))
}

func (h *actionHost) Run(status string, fn func() error, done func(error)) {
	if h.busy {
		h.Notify("Busy, please wait...")
		return
	}
	h.busy = true
	h.Notify(status)

	go func() {
		err := fn()
		h.screen.QueueUpdateDraw(func() {
			h.busy = false
			if err != nil {
				h.Notify(fmt.Sprintf("Error: %s", oneLine(err.Error())))
			} else if h.restore != nil {
				h.restore()
			}
			if done != nil {
				done(err)
			}
		})
	}()
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// showModal shows a primitive centered over the layout and returns a func
// that closes it and gives focus back to what had it.
func (h *actionHost) showModal(p tview.Primitive, width, height int) func() {
	back := h.screen.GetFocus()

	grid := tview.NewGrid().
		SetColumns(0, width, 0).
		SetRows(0, height, 0).
		AddItem(p, 1, 1, 1, 1, 0, 0, true)

	h.pages.AddPage(modalPage, grid, true, true)
	h.screen.SetFocus(p)

	return func() {
		h.pages.RemovePage(modalPage)
		if back != nil {
			h.screen.SetFocus(back)
		}
	}
}

func (h *actionHost) Pick(p Picker) {
	input := tview.NewInputField().
		SetLabel("Filter: ").
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetFieldTextColor(tcell.ColorDefault)
	list := tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true).
		SetSelectedBackgroundColor(tcell.ColorDarkCyan)
	status := tview.NewTextView().SetTextColor(tcell.ColorGray)

	box := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 1, 0, true).
		AddItem(list, 0, 1, false).
		AddItem(status, 1, 0, false)
	box.SetBorder(true).
		SetBorderColor(focusedBorderColor).
		SetTitleColor(focusedBorderColor).
		SetTitleAlign(tview.AlignLeft).
		SetTitle(fmt.Sprintf(" [::b]%s[::-] ", p.Title))

	var shown []PickerItem
	setItems := func(items []PickerItem) {
		shown = append(append([]PickerItem{}, p.Fixed...), items...)
		list.Clear()
		for _, it := range shown {
			list.AddItem(" "+it.Label, "", 0, nil)
		}
		if len(shown) == 0 {
			status.SetText(" No matches")
		} else {
			status.SetText(" ↑/↓: move · ENTER: pick · ESC: cancel")
		}
	}

	var (
		searchMu  sync.Mutex
		searchSeq int
		timer     *time.Timer
	)
	search := func(q string) {
		searchMu.Lock()
		searchSeq++
		seq := searchSeq
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(250*time.Millisecond, func() {
			items, err := p.Search(q)
			h.screen.QueueUpdateDraw(func() {
				searchMu.Lock()
				stale := seq != searchSeq
				searchMu.Unlock()
				if stale {
					return
				}
				if err != nil {
					status.SetText(" Error: " + oneLine(err.Error()))
					return
				}
				setItems(items)
			})
		})
		searchMu.Unlock()
		status.SetText(" Searching...")
	}

	filter := func(q string) {
		if p.Search != nil {
			search(q)
			return
		}
		q = strings.ToLower(strings.TrimSpace(q))
		items := make([]PickerItem, 0, len(p.Items))
		for _, it := range p.Items {
			if q == "" || strings.Contains(strings.ToLower(it.Label), q) {
				items = append(items, it)
			}
		}
		setItems(items)
	}

	width, height := 70, 20
	if n := len(p.Items) + len(p.Fixed); p.Search == nil && n+4 < height {
		height = n + 4
		if height < 7 {
			height = 7
		}
	}
	closeModal := h.showModal(box, width, height)

	input.SetChangedFunc(filter)
	input.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyDown, tcell.KeyCtrlN, tcell.KeyTab:
			if n := list.GetItemCount(); n > 0 {
				list.SetCurrentItem((list.GetCurrentItem() + 1) % n)
			}
			return nil
		case tcell.KeyUp, tcell.KeyCtrlP, tcell.KeyBacktab:
			if n := list.GetItemCount(); n > 0 {
				list.SetCurrentItem((list.GetCurrentItem() - 1 + n) % n)
			}
			return nil
		case tcell.KeyEnter:
			i := list.GetCurrentItem()
			if i < 0 || i >= len(shown) {
				return nil
			}
			item := shown[i]
			closeModal()
			if p.OnPick != nil {
				p.OnPick(item)
			}
			return nil
		case tcell.KeyEsc:
			closeModal()
			return nil
		}
		return ev
	})

	filter("")
}

func (h *actionHost) Form(f Form) {
	form := tview.NewForm().
		SetFieldBackgroundColor(tcell.ColorDarkSlateGray).
		SetFieldTextColor(tcell.ColorWhite).
		SetButtonBackgroundColor(tcell.ColorDarkCyan).
		SetButtonTextColor(tcell.ColorWhite).
		SetLabelColor(tcell.ColorDefault)
	form.SetBorder(true).
		SetBorderColor(focusedBorderColor).
		SetTitleColor(focusedBorderColor).
		SetTitleAlign(tview.AlignLeft).
		SetTitle(fmt.Sprintf(" [::b]%s[::-] ", f.Title))

	labelWidth := 0
	for _, fd := range f.Fields {
		if len(fd.Label) > labelWidth {
			labelWidth = len(fd.Label)
		}
	}
	label := func(s string) string { return s + strings.Repeat(" ", labelWidth-len(s)+1) }

	for _, fd := range f.Fields {
		if len(fd.Options) > 0 {
			current := 0
			for i, o := range fd.Options {
				if o == fd.Value {
					current = i
				}
			}
			form.AddDropDown(label(fd.Label), fd.Options, current, nil)
			continue
		}
		var accept func(string, rune) bool
		if fd.Numeric {
			accept = func(text string, _ rune) bool {
				_, err := fmt.Sscanf(text, "%g", new(float64))
				return text == "" || (err == nil && !strings.ContainsAny(text, " \t"))
			}
		}
		form.AddInputField(label(fd.Label), fd.Value, 0, accept, nil)
	}

	var closeModal func()
	submit := func() {
		values := make(map[string]string, len(f.Fields))
		for i, fd := range f.Fields {
			switch item := form.GetFormItem(i).(type) {
			case *tview.InputField:
				values[fd.Name] = strings.TrimSpace(item.GetText())
			case *tview.DropDown:
				_, values[fd.Name] = item.GetCurrentOption()
			}
		}
		closeModal()
		if f.OnSubmit != nil {
			f.OnSubmit(values)
		}
	}
	form.AddButton("Save", submit)
	form.AddButton("Cancel", func() { closeModal() })
	form.SetCancelFunc(func() { closeModal() })
	form.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyCtrlS {
			submit()
			return nil
		}
		return ev
	})

	help := tview.NewTextView().
		SetTextColor(tcell.ColorGray).
		SetText(" TAB: next field · CTRL+S: save · ESC: cancel")
	box := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(form, 0, 1, true).
		AddItem(help, 1, 0, false)

	closeModal = h.showModal(box, 80, 2*len(f.Fields)+6)
	h.screen.SetFocus(form)
}

func (h *actionHost) EditText(text string, done func(string, error)) {
	go func() {
		var (
			out string
			err error
		)
		h.screen.Suspend(func() {
			out, err = runEditor(text)
		})
		h.screen.QueueUpdateDraw(func() { done(out, err) })
	}()
}

// runEditor opens text in the user's editor and returns the edited text.
func runEditor(text string) (string, error) {
	f, err := os.CreateTemp("", "jira-*.md")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(f.Name()) }()

	if _, err := f.WriteString(text); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	args, err := shellquote.Split(surveyext.DefaultEditor())
	if err != nil {
		return "", err
	}
	if len(args) == 0 {
		return "", fmt.Errorf("no editor configured; set JIRA_EDITOR or EDITOR")
	}
	args = append(args, f.Name())

	cmd := exec.Command(args[0], args[1:]...) //nolint:gosec
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor: %w", err)
	}

	b, err := os.ReadFile(f.Name())
	if err != nil {
		return "", err
	}
	return string(b), nil
}
