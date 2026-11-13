package tui

import (
	"errors"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runTree paints a tree on a simulated screen and returns it with a func
// that injects keys.
func runTree(t *testing.T, actions map[rune]RowAction) (*Tree, func(tcell.Key, rune)) {
	t.Helper()

	sim := tcell.NewSimulationScreen("UTF-8")
	require.NoError(t, sim.Init())
	sim.SetSize(120, 40)

	roots := []TreeRow{
		{Key: "EPIC-1", Cells: []string{"EPIC-1", "Epic", "One"}},
		{Key: "EPIC-2", Cells: []string{"EPIC-2", "Epic", "Two"}},
	}
	tree := NewTree(
		WithTreeHeader([]string{"KEY", "TYPE", "SUMMARY"}),
		WithTreeRowActions(actions),
		WithTreeDataFunc(func() ([]TreeRow, string) { return roots, "reloaded" }),
		WithTreeChildrenFunc(func(string) []TreeRow { return nil }),
	)
	tree.screen.SetScreen(sim)

	done := make(chan struct{})
	go func() {
		_ = tree.Paint(roots)
		close(done)
	}()
	t.Cleanup(func() {
		tree.screen.Stop()
		<-done
	})

	inject := func(k tcell.Key, r rune) {
		ev := tcell.NewEventKey(k, r, tcell.ModNone)
		// Wait for the app to process the previous event.
		time.Sleep(30 * time.Millisecond)
		tree.screen.QueueEvent(ev)
	}
	time.Sleep(50 * time.Millisecond)
	return tree, inject
}

func waitFor(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
		return ""
	}
}

func TestTreeRowActionPicker(t *testing.T) {
	got := make(chan string, 1)
	_, inject := runTree(t, map[rune]RowAction{
		'x': func(ui ActionUI, row RowTarget) {
			assert.Equal(t, RowTarget{Key: "EPIC-1", Parent: "EPIC-1", IsParent: true}, row)
			ui.Pick(Picker{
				Title: "Pick",
				Fixed: []PickerItem{{Label: "None", Value: "none"}},
				Items: []PickerItem{{Label: "alpha", Value: "a"}, {Label: "beta", Value: "b"}},
				OnPick: func(it PickerItem) {
					got <- it.Value.(string)
				},
			})
		},
	})

	inject(tcell.KeyRune, 'x')
	// Filter to "bet", which leaves the fixed item and beta.
	inject(tcell.KeyRune, 'b')
	inject(tcell.KeyRune, 'e')
	inject(tcell.KeyRune, 't')
	inject(tcell.KeyDown, 0)
	inject(tcell.KeyEnter, 0)

	assert.Equal(t, "b", waitFor(t, got))
}

func TestTreeRowActionFormAndRun(t *testing.T) {
	got := make(chan string, 2)
	tree, inject := runTree(t, map[rune]RowAction{
		'f': func(ui ActionUI, row RowTarget) {
			ui.Form(Form{
				Title: "Edit",
				Fields: []FormField{
					{Name: "summary", Label: "Summary", Value: "old"},
					{Name: "prio", Label: "Priority", Value: "Low", Options: []string{"High", "Low"}},
				},
				OnSubmit: func(values map[string]string) {
					got <- values["summary"] + "|" + values["prio"]
					ui.Run("Saving...", func() error { return errors.New("boom") }, func(err error) {
						got <- err.Error()
					})
				},
			})
		},
	})

	inject(tcell.KeyDown, 0) // Select EPIC-2 first.
	inject(tcell.KeyRune, 'f')
	inject(tcell.KeyRune, '!')
	inject(tcell.KeyCtrlS, 0)

	assert.Equal(t, "old!|Low", waitFor(t, got))
	assert.Equal(t, "boom", waitFor(t, got))

	// The footer shows the error and the modal is gone.
	time.Sleep(50 * time.Millisecond)
	done := make(chan struct{})
	tree.screen.QueueUpdate(func() {
		assert.Contains(t, tree.footer.GetText(true), "Error: boom")
		assert.False(t, tree.painter.HasPage(modalPage))
		assert.Equal(t, tree.view, tree.screen.GetFocus())
		close(done)
	})
	<-done
}

func TestTreeReloadSelectsPendingKey(t *testing.T) {
	tree, inject := runTree(t, map[rune]RowAction{
		'r': func(ui ActionUI, _ RowTarget) { ui.Reload(nil, "EPIC-2") },
	})
	inject(tcell.KeyRune, 'r')
	time.Sleep(50 * time.Millisecond)

	done := make(chan struct{})
	tree.screen.QueueUpdate(func() {
		r, _ := tree.view.GetSelection()
		line, ok := tree.lineAt(r)
		assert.True(t, ok)
		assert.Equal(t, "EPIC-2", line.key)
		assert.Equal(t, "reloaded", tree.footerText)
		close(done)
	})
	<-done
}
