package view

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ankitpokhrel/jira-cli/pkg/jira"
)

// epicSortField is a client-side sort field for epics and their issues in
// the epic explorer.
type epicSortField struct {
	name string
	// less reports whether a sorts before b in the field's default direction.
	// A nil less keeps the order the issues were fetched in.
	less func(a, b *jira.Issue) bool
	// desc reports whether the field's default direction is descending; used
	// only for the direction arrow in the footer.
	desc bool
}

var epicSortFields = []epicSortField{
	{name: "default"},
	{name: "updated", less: func(a, b *jira.Issue) bool {
		return parseJiraTime(a.Fields.Updated).After(parseJiraTime(b.Fields.Updated))
	}, desc: true},
	{name: "created", less: func(a, b *jira.Issue) bool {
		return parseJiraTime(a.Fields.Created).After(parseJiraTime(b.Fields.Created))
	}, desc: true},
	{name: "status", less: func(a, b *jira.Issue) bool {
		ra, rb := statusCategoryRank(a), statusCategoryRank(b)
		if ra != rb {
			return ra < rb
		}
		return strings.ToLower(a.Fields.Status.Name) < strings.ToLower(b.Fields.Status.Name)
	}},
	{name: "key", less: func(a, b *jira.Issue) bool {
		return lessIssueKey(a.Key, b.Key)
	}},
	{name: "summary", less: func(a, b *jira.Issue) bool {
		return strings.ToLower(strings.TrimSpace(a.Fields.Summary)) < strings.ToLower(strings.TrimSpace(b.Fields.Summary))
	}},
}

// sortState is the client-side sort of a list of issues.
type sortState struct {
	idx      int
	reversed bool
}

// cycle selects the next sort field, or reverses the order when reverse is set.
func (s *sortState) cycle(reverse bool) {
	if reverse {
		s.reversed = !s.reversed
		return
	}
	s.idx = (s.idx + 1) % len(epicSortFields)
	s.reversed = false
}

// apply returns a copy of issues sorted by the sort state.
func (s sortState) apply(issues []*jira.Issue) []*jira.Issue {
	sorted := make([]*jira.Issue, len(issues))
	copy(sorted, issues)

	field := epicSortFields[s.idx]
	if field.less != nil {
		sort.SliceStable(sorted, func(i, j int) bool {
			return field.less(sorted[i], sorted[j])
		})
	}
	if s.reversed {
		for i, j := 0, len(sorted)-1; i < j; i, j = i+1, j-1 {
			sorted[i], sorted[j] = sorted[j], sorted[i]
		}
	}
	return sorted
}

// label describes the sort, eg: "updated ↓". defaultLabel describes the
// order the issues were fetched in.
func (s sortState) label(defaultLabel string) string {
	if s.idx == 0 {
		label := defaultLabel
		if label == "" {
			label = "default"
		}
		if s.reversed {
			label += " (reversed)"
		}
		return label
	}
	field := epicSortFields[s.idx]
	arrow := "↑"
	if field.desc != s.reversed {
		arrow = "↓"
	}
	return fmt.Sprintf("%s %s", field.name, arrow)
}

// sortedEpics returns a copy of the epics sorted by the current epic sort.
func (el *EpicList) sortedEpics() []*jira.Issue {
	el.stateMu.RLock()
	defer el.stateMu.RUnlock()
	return el.epicSort.apply(el.Data)
}

// sortedIssues returns a copy of the issues sorted by the current issue sort.
func (el *EpicList) sortedIssues(issues []*jira.Issue) []*jira.Issue {
	el.stateMu.RLock()
	defer el.stateMu.RUnlock()
	return el.issueSort.apply(issues)
}

// cycleSort changes the epic sort: the next field, or the reverse order when
// reverse is set.
func (el *EpicList) cycleSort(reverse bool) {
	el.stateMu.Lock()
	defer el.stateMu.Unlock()
	el.epicSort.cycle(reverse)
}

// cycleIssueSort changes the sort of the issues within epics.
func (el *EpicList) cycleIssueSort(reverse bool) {
	el.stateMu.Lock()
	defer el.stateMu.Unlock()
	el.issueSort.cycle(reverse)
}

// sortLabel describes the current epic sort, eg: "updated ↓".
func (el *EpicList) sortLabel() string {
	el.stateMu.RLock()
	defer el.stateMu.RUnlock()
	return el.epicSort.label(el.DefaultOrder)
}

// issueSortLabel describes the current sort of issues within epics.
func (el *EpicList) issueSortLabel() string {
	el.stateMu.RLock()
	defer el.stateMu.RUnlock()
	return el.issueSort.label("")
}

// statusCategoryRank orders statuses by workflow: to do, in progress, done.
func statusCategoryRank(i *jira.Issue) int {
	switch i.Fields.Status.StatusCategory.Key {
	case "new":
		return 0
	case "indeterminate":
		return 1
	case "done":
		return 2
	default:
		return 3
	}
}

// lessIssueKey compares issue keys like PROJ-12 by project and then numerically.
func lessIssueKey(a, b string) bool {
	pa, na := splitIssueKey(a)
	pb, nb := splitIssueKey(b)
	if pa != pb {
		return pa < pb
	}
	if na != nb {
		return na < nb
	}
	return a < b
}

func splitIssueKey(key string) (string, int) {
	idx := strings.LastIndex(key, "-")
	if idx < 0 {
		return key, 0
	}
	n, err := strconv.Atoi(key[idx+1:])
	if err != nil {
		return key, 0
	}
	return key[:idx], n
}

func parseJiraTime(s string) time.Time {
	for _, layout := range []string{jira.RFC3339MilliLayout, jira.RFC3339, time.RFC3339Nano} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
