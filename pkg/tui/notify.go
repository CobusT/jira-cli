package tui

import "github.com/rivo/tview"

// notifier shows a message in the footer of the layout that is currently painted.
var notifier func(string)

// Notify shows a short message in the footer of the active layout, eg: to
// confirm a copy to the clipboard. It must be called on the UI goroutine, eg:
// from a key or selection handler. It does nothing when no layout is active.
func Notify(msg string) {
	if notifier != nil && msg != "" {
		notifier(msg)
	}
}

func setNotifier(footer *tview.TextView) {
	notifier = func(msg string) {
		footer.SetText(pad(msg, 1))
	}
}
