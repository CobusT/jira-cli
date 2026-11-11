package view

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ankitpokhrel/jira-cli/pkg/jira"
)

// epicSortField is a client-side sort field for the epic explorer.
type epicSortField struct {
	name string
	// less reports whether a sorts before b in the field's default direction.
	// A nil less keeps the order the epics were fetched in.
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

// sortedEpics returns a copy of the epics sorted by the current sort field.
func (el *EpicList) sortedEpics() []*jira.Issue {
	epics := make([]*jira.Issue, len(el.Data))
	copy(epics, el.Data)

	field := epicSortFields[el.sortIdx]
	if field.less != nil {
		sort.SliceStable(epics, func(i, j int) bool {
			return field.less(epics[i], epics[j])
		})
	}
	if el.sortReversed {
		for i, j := 0, len(epics)-1; i < j; i, j = i+1, j-1 {
			epics[i], epics[j] = epics[j], epics[i]
		}
	}
	return epics
}

// cycleSort selects the next sort field, or reverses the order when reverse is set.
func (el *EpicList) cycleSort(reverse bool) {
	if reverse {
		el.sortReversed = !el.sortReversed
		return
	}
	el.sortIdx = (el.sortIdx + 1) % len(epicSortFields)
	el.sortReversed = false
}

// sortLabel describes the current sort, eg: "updated ↓".
func (el *EpicList) sortLabel() string {
	if el.sortIdx == 0 {
		label := el.DefaultOrder
		if label == "" {
			label = "default"
		}
		if el.sortReversed {
			label += " (reversed)"
		}
		return label
	}
	field := epicSortFields[el.sortIdx]
	desc := field.desc != el.sortReversed
	arrow := "↑"
	if desc {
		arrow = "↓"
	}
	return fmt.Sprintf("%s %s", field.name, arrow)
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
