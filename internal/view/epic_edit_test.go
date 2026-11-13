package view

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ankitpokhrel/jira-cli/internal/issueops"
	"github.com/ankitpokhrel/jira-cli/pkg/jira"
)

func TestFormChanges(t *testing.T) {
	issue := &jira.Issue{Key: "BART-1"}
	issue.Fields.Summary = "Old"
	issue.Fields.Priority.Name = "Medium"
	issue.Fields.Labels = []string{"a", "b"}

	points := jira.IssueTypeField{Name: "Story Points", Key: "customfield_10014"}
	points.Schema.DataType = "number"
	fields := []jira.IssueTypeField{points}
	custom := map[string]string{"customfield_10014": "3"}

	// Unchanged values produce no changes.
	unchanged := map[string]string{
		formSummary: "Old", formPriority: "Medium", formLabels: "a, b", "customfield_10014": "3",
	}
	assert.True(t, isNoChange(formChanges(issue, custom, unchanged, fields)))

	// An empty custom field is left as is.
	cleared := map[string]string{
		formSummary: "Old", formPriority: "Medium", formLabels: "b,a", "customfield_10014": "",
	}
	assert.True(t, isNoChange(formChanges(issue, custom, cleared, fields)))

	changed := map[string]string{
		formSummary: "New", formPriority: "High", formLabels: "b c", "customfield_10014": "5",
	}
	assert.Equal(t, issueops.Changes{
		Summary:      "New",
		Priority:     "High",
		Labels:       []string{"b", "c"},
		CustomFields: map[string]string{"customfield_10014": "5"},
	}, formChanges(issue, custom, changed, fields))
}

func TestEditFormIncludesCurrentPriority(t *testing.T) {
	issue := &jira.Issue{Key: "BART-1"}
	issue.Fields.Priority.Name = "Blocker"

	form := editForm(issue, nil, []string{"High", "Low"}, nil)
	assert.Equal(t, []string{"Blocker", "High", "Low"}, form.Fields[1].Options)
	assert.Equal(t, "Blocker", form.Fields[1].Value)
}

func TestRowActionsNeedOps(t *testing.T) {
	assert.Nil(t, (&EpicList{}).rowActions())
	assert.Len(t, (&EpicList{Ops: &issueops.Ops{}}).rowActions(), 7)
}
