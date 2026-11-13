package view

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ankitpokhrel/jira-cli/pkg/jira"
)

func newSortTestEpic(key, summary, status, category, created, updated string) *jira.Issue {
	i := &jira.Issue{Key: key}
	i.Fields.Summary = summary
	i.Fields.Status.Name = status
	i.Fields.Status.StatusCategory.Key = category
	i.Fields.Created = created
	i.Fields.Updated = updated
	return i
}

func epicKeys(issues []*jira.Issue) []string {
	keys := make([]string, 0, len(issues))
	for _, i := range issues {
		keys = append(keys, i.Key)
	}
	return keys
}

func TestEpicSort(t *testing.T) {
	el := &EpicList{
		DefaultOrder: "rank ↑",
		Data: []*jira.Issue{
			newSortTestEpic("BART-10", "beta", "In Progress", "indeterminate", "2026-01-02T10:00:00.000-0800", "2026-03-01T10:00:00.000-0800"),
			newSortTestEpic("BART-9", "Alpha", "To Do", "new", "2026-01-03T10:00:00.000-0800", "2026-02-01T10:00:00.000-0800"),
			newSortTestEpic("BART-100", "gamma", "Under Review", "indeterminate", "2026-01-01T10:00:00.000-0800", "2026-04-01T10:00:00.000-0800"),
		},
	}

	steps := []struct {
		reverse bool
		label   string
		keys    []string
	}{
		{label: "rank ↑", keys: []string{"BART-10", "BART-9", "BART-100"}},
		{reverse: true, label: "rank ↑ (reversed)", keys: []string{"BART-100", "BART-9", "BART-10"}},
		{label: "updated ↓", keys: []string{"BART-100", "BART-10", "BART-9"}},
		{label: "created ↓", keys: []string{"BART-9", "BART-10", "BART-100"}},
		{label: "status ↑", keys: []string{"BART-9", "BART-10", "BART-100"}},
		{reverse: true, label: "status ↓", keys: []string{"BART-100", "BART-10", "BART-9"}},
		{label: "key ↑", keys: []string{"BART-9", "BART-10", "BART-100"}},
		{label: "summary ↑", keys: []string{"BART-9", "BART-10", "BART-100"}},
		{reverse: true, label: "summary ↓", keys: []string{"BART-100", "BART-10", "BART-9"}},
		{label: "rank ↑", keys: []string{"BART-10", "BART-9", "BART-100"}},
	}

	for i, step := range steps {
		if i > 0 {
			el.cycleSort(step.reverse)
		}
		assert.Equal(t, step.label, el.sortLabel(), "step %d", i)
		assert.Equal(t, step.keys, epicKeys(el.sortedEpics()), "step %d", i)
	}

	// Sorting must not reorder the fetched data.
	assert.Equal(t, []string{"BART-10", "BART-9", "BART-100"}, epicKeys(el.Data))
}

func TestEpicIssueSortIsIndependent(t *testing.T) {
	issues := []*jira.Issue{
		newSortTestEpic("BART-2", "beta", "To Do", "new", "2026-01-01T10:00:00.000-0800", "2026-01-05T10:00:00.000-0800"),
		newSortTestEpic("BART-1", "alpha", "To Do", "new", "2026-01-02T10:00:00.000-0800", "2026-01-04T10:00:00.000-0800"),
	}
	el := &EpicList{
		DefaultOrder: "rank ↑",
		Data:         issues,
	}

	assert.Equal(t, "default", el.issueSortLabel())
	assert.Equal(t, []string{"BART-2", "BART-1"}, epicKeys(el.sortedIssues(issues)))

	el.cycleIssueSort(false) // updated ↓
	el.cycleIssueSort(false) // created ↓
	assert.Equal(t, "created ↓", el.issueSortLabel())
	assert.Equal(t, []string{"BART-1", "BART-2"}, epicKeys(el.sortedIssues(issues)))

	el.cycleIssueSort(true)
	assert.Equal(t, "created ↑", el.issueSortLabel())
	assert.Equal(t, []string{"BART-2", "BART-1"}, epicKeys(el.sortedIssues(issues)))

	// The epic sort is untouched.
	assert.Equal(t, "rank ↑", el.sortLabel())
	assert.Equal(t, []string{"BART-2", "BART-1"}, epicKeys(el.sortedEpics()))

	// Changing the epic sort leaves the issue sort alone.
	el.cycleSort(false)
	assert.Equal(t, "updated ↓", el.sortLabel())
	assert.Equal(t, "created ↑", el.issueSortLabel())
}
