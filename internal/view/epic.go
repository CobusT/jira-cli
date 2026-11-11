package view

import (
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"

	"github.com/ankitpokhrel/jira-cli/api"
	"github.com/ankitpokhrel/jira-cli/internal/cmdutil"
	"github.com/ankitpokhrel/jira-cli/pkg/jira"
	"github.com/ankitpokhrel/jira-cli/pkg/jira/filter/issue"
	"github.com/ankitpokhrel/jira-cli/pkg/tui"
)

// EpicIssueFunc provides issues for the epic.
type EpicIssueFunc func(string) []*jira.Issue

// EpicRefreshFunc reloads the list of epics.
type EpicRefreshFunc func() ([]*jira.Issue, error)

// EpicLayout is a layout of the epic explorer.
type EpicLayout string

const (
	// EpicLayoutPanes shows epics in a sidebar and the selected epic's issues
	// in a contents pane.
	EpicLayoutPanes EpicLayout = "panes"
	// EpicLayoutTree shows a single list where epics expand in place to show
	// their issues.
	EpicLayoutTree EpicLayout = "tree"
)

// ValidEpicLayouts returns the supported epic explorer layouts.
func ValidEpicLayouts() []string {
	return []string{string(EpicLayoutPanes), string(EpicLayoutTree)}
}

// EpicList is a list view for epics.
type EpicList struct {
	Project string
	Server  string
	Data    []*jira.Issue
	Issues  EpicIssueFunc
	Refresh EpicRefreshFunc
	Display DisplayFormat
	// DefaultOrder describes the order epics are fetched in, eg: "rank ↑".
	DefaultOrder string
	// Layout is the initial layout; 'V' switches between layouts.
	Layout EpicLayout

	// stateMu guards Data and the sort state, which refreshes update from
	// background goroutines while the UI reads them.
	stateMu      sync.RWMutex
	sortIdx      int
	sortReversed bool

	issuesMu    sync.Mutex
	issuesCache map[string][]*jira.Issue

	// treeExpanded keeps expanded epics across layout switches.
	treeExpanded map[string]bool
}

// Render renders the epic explorer view. A user can switch between the
// layouts with 'V', keeping the selected epic, sort order and loaded issues.
func (el *EpicList) Render() error {
	renderer, err := MDRenderer()
	if err != nil {
		return err
	}

	layout := el.Layout
	if layout == "" {
		layout = EpicLayoutPanes
	}

	selected := ""
	for {
		switchTo := ""
		onSwitch := func(key string) {
			selected = key
			switchTo = "yes"
		}

		if layout == EpicLayoutTree {
			err = el.renderTree(renderer, selected, onSwitch)
		} else {
			err = el.renderPanes(renderer, selected, onSwitch)
		}
		if err != nil || switchTo == "" {
			return err
		}

		if layout == EpicLayoutTree {
			layout = EpicLayoutPanes
		} else {
			layout = EpicLayoutTree
		}
	}
}

// issuesFor returns the issues of the given epic, using a cache shared by
// both layouts. It is safe for concurrent use.
func (el *EpicList) issuesFor(key string) []*jira.Issue {
	el.issuesMu.Lock()
	if issues, ok := el.issuesCache[key]; ok {
		el.issuesMu.Unlock()
		return issues
	}
	el.issuesMu.Unlock()

	issues := el.Issues(key)

	el.issuesMu.Lock()
	if el.issuesCache == nil {
		el.issuesCache = make(map[string][]*jira.Issue)
	}
	el.issuesCache[key] = issues
	el.issuesMu.Unlock()

	return issues
}

func (el *EpicList) clearIssuesCache() {
	el.issuesMu.Lock()
	el.issuesCache = nil
	el.issuesMu.Unlock()
}

// refreshEpics reloads the epics and clears cached issues.
func (el *EpicList) refreshEpics() error {
	epics, err := el.Refresh()
	if err != nil {
		return err
	}
	el.stateMu.Lock()
	el.Data = epics
	el.stateMu.Unlock()
	el.clearIssuesCache()
	return nil
}

// viewIssue renders the details of the issue with the given key.
func (el *EpicList) viewIssue(renderer *glamour.TermRenderer, key string) (string, error) {
	iss, err := api.ProxyGetIssue(api.DefaultClient(false), key, issue.NewNumCommentsFilter(1))
	if err != nil {
		return "", err
	}
	out := Issue{
		Server:  el.Server,
		Data:    iss,
		Options: IssueOption{NumComments: 1},
	}
	return out.RenderedOut(renderer)
}

//nolint:dupl
func (el *EpicList) renderPanes(renderer *glamour.TermRenderer, selected string, onSwitch tui.SwitchLayoutFunc) error {
	data := el.data()
	opts := []tui.PreviewOption{
		tui.WithPreviewFooterText(el.footerText()),
		tui.WithPaneTitles("Epics", "Issues"),
		tui.WithInitialText(el.helpText()),
		tui.WithInitialSelection(selected),
		tui.WithSidebarSelectedFunc(navigate(el.Server)),
		tui.WithSidebarViewFunc(func(key string) (string, error) {
			return el.viewIssue(renderer, key)
		}),
		tui.WithPreviewSwitchLayoutFunc(onSwitch),
		tui.WithContentTableOpts(
			tui.WithTableStyle(el.Display.TableStyle),
			tui.WithFixedColumns(el.Display.FixedColumns),
			tui.WithSelectedFunc(navigate(el.Server)),
			tui.WithViewModeFunc(func(r, c int, d any) (func() any, func(any) (string, error)) {
				dataFn := func() any {
					data := d.(tui.TableData)
					return data.Get(r, data.GetIndex(fieldKey))
				}
				renderFn := func(k any) (string, error) {
					return el.viewIssue(renderer, k.(string))
				}
				return dataFn, renderFn
			}),
			tui.WithCopyFunc(copyURL(el.Server)),
			tui.WithCopyKeyFunc(copyKey()),
		),
	}
	if el.Refresh != nil {
		opts = append(opts, tui.WithPreviewRefreshFunc(func() ([]tui.PreviewData, string, error) {
			if err := el.refreshEpics(); err != nil {
				return nil, "", err
			}
			return el.data(), el.footerText(), nil
		}))
	}
	opts = append(opts, tui.WithPreviewSortFunc(func(reverse bool) ([]tui.PreviewData, string) {
		el.cycleSort(reverse)
		return el.data(), el.footerText()
	}))

	return tui.NewPreview(opts...).Paint(data)
}

var epicTreeHeader = []string{"KEY", "TYPE", "SUMMARY", "STATUS", "ASSIGNEE", "UPDATED"}

const epicTreeSummaryCol = 2

func (el *EpicList) treeRow(i *jira.Issue) tui.TreeRow {
	return tui.TreeRow{
		Key: i.Key,
		Cells: []string{
			i.Key,
			i.Fields.IssueType.Name,
			prepareTitle(i.Fields.Summary),
			i.Fields.Status.Name,
			i.Fields.Assignee.Name,
			formatDateTime(i.Fields.Updated, jira.RFC3339, el.Display.Timezone),
		},
	}
}

func (el *EpicList) treeRoots() []tui.TreeRow {
	epics := el.sortedEpics()
	rows := make([]tui.TreeRow, 0, len(epics))
	for _, e := range epics {
		rows = append(rows, el.treeRow(e))
	}
	return rows
}

func (el *EpicList) treeHelpText() string {
	help := "TAB/→/l: expand · ←/h: collapse · v: view · ENTER: open · c/CTRL+K: copy URL/key · /: search · s/S: sort"
	if el.Refresh != nil {
		help += " · r: refresh"
	}
	return help + " · V: 2-pane view · q: quit"
}

func (el *EpicList) renderTree(renderer *glamour.TermRenderer, selected string, onSwitch tui.SwitchLayoutFunc) error {
	if el.treeExpanded == nil {
		el.treeExpanded = make(map[string]bool)
	}
	opts := []tui.TreeOption{
		tui.WithTreeTitle("Epics"),
		tui.WithTreeHeader(epicTreeHeader),
		tui.WithTreeSummaryColumn(epicTreeSummaryCol),
		tui.WithTreeFooterText(el.footerText()),
		tui.WithTreeHelpText(el.treeHelpText()),
		tui.WithTreeStyle(el.Display.TableStyle),
		tui.WithTreeInitialSelection(selected),
		tui.WithTreeExpanded(el.treeExpanded),
		tui.WithTreeChildrenFunc(func(key string) []tui.TreeRow {
			issues := el.issuesFor(key)
			rows := make([]tui.TreeRow, 0, len(issues))
			for _, i := range issues {
				rows = append(rows, el.treeRow(i))
			}
			return rows
		}),
		tui.WithTreeSortFunc(func(reverse bool) ([]tui.TreeRow, string) {
			el.cycleSort(reverse)
			return el.treeRoots(), el.footerText()
		}),
		tui.WithTreeViewFunc(func(key string) (string, error) {
			return el.viewIssue(renderer, key)
		}),
		tui.WithTreeOpenFunc(func(key string) {
			openURL(cmdutil.GenerateServerBrowseURL(el.Server, key))
		}),
		tui.WithTreeCopyURLFunc(func(key string) {
			copyText(cmdutil.GenerateServerBrowseURL(el.Server, key), "URL")
		}),
		tui.WithTreeCopyKeyFunc(func(key string) {
			copyText(key, "key")
		}),
		tui.WithTreeSwitchLayoutFunc(onSwitch),
	}
	if el.Refresh != nil {
		opts = append(opts, tui.WithTreeRefreshFunc(func() ([]tui.TreeRow, string, error) {
			if err := el.refreshEpics(); err != nil {
				return nil, "", err
			}
			return el.treeRoots(), el.footerText(), nil
		}))
	}

	return tui.NewTree(opts...).Paint(el.treeRoots())
}

func (el *EpicList) epicCount() int {
	el.stateMu.RLock()
	defer el.stateMu.RUnlock()
	return len(el.Data)
}

func (el *EpicList) footerText() string {
	return fmt.Sprintf("Showing %d results for project %q · sorted by %s (s: next sort, S: reverse)",
		el.epicCount(), el.Project, el.sortLabel())
}

func (el *EpicList) helpText() string {
	epicHelp := "\n\tEpics (from either screen):" +
		"\n\t  - Press 's' to cycle the sort field (default, updated, created, status, key, summary)." +
		"\n\t  - Press 'SHIFT+S' to reverse the sort order." +
		"\n\t  - Press 'v' in the sidebar to view the selected epic's details." +
		"\n\t  - Press 'SHIFT+V' to switch to the single list view, where TAB expands an epic in place."
	if el.Refresh != nil {
		epicHelp += "\n\t  - Press 'r' (or 'CTRL+R' / 'F5') to refresh the epics and their issues."
	}
	epicHelp += "\n\t\n\tPress 'q' / ESC / CTRL+C to quit."

	return strings.Replace(helpText, "\n\tPress 'q' / ESC / CTRL+C to quit.", epicHelp, 1)
}

func (el *EpicList) data() []tui.PreviewData {
	data := make([]tui.PreviewData, 0, el.epicCount()+1)

	data = append(data, tui.PreviewData{
		Key:  "help",
		Menu: "?",
		Contents: func(_ string) any {
			return el.helpText()
		},
	})
	for _, issue := range el.sortedEpics() {
		data = append(data, tui.PreviewData{
			Key:  issue.Key,
			Menu: fmt.Sprintf("➤ %s: %s", issue.Key, prepareTitle(issue.Fields.Summary)),
			Contents: func(key string) any {
				issues := el.issuesFor(key)
				return el.tabularize(issues)
			},
		})
	}

	return data
}

func (el *EpicList) tabularize(issues []*jira.Issue) tui.TableData {
	data := make(tui.TableData, 0, 1+len(issues))

	data = append(data, []string{
		"TYPE",
		"KEY",
		"SUMMARY",
		"STATUS",
		"ASSIGNEE",
		"REPORTER",
		"PRIORITY",
		"RESOLUTION",
		"CREATED",
		"UPDATED",
	})
	for _, issue := range issues {
		data = append(data, []string{
			issue.Fields.IssueType.Name,
			issue.Key,
			prepareTitle(issue.Fields.Summary),
			issue.Fields.Status.Name,
			issue.Fields.Assignee.Name,
			issue.Fields.Reporter.Name,
			issue.Fields.Priority.Name,
			issue.Fields.Resolution.Name,
			formatDateTime(issue.Fields.Created, jira.RFC3339, el.Display.Timezone),
			formatDateTime(issue.Fields.Updated, jira.RFC3339, el.Display.Timezone),
		})
	}

	return data
}
