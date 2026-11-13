package issueops

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ankitpokhrel/jira-cli/pkg/jira"
)

func TestLabelChanges(t *testing.T) {
	assert.Equal(t, []string{"new", "-gone"}, LabelChanges([]string{"keep", "gone"}, []string{"keep", "new"}))
	assert.Nil(t, LabelChanges([]string{"a"}, []string{"a"}))
	assert.Equal(t, []string{"-a", "-b"}, LabelChanges([]string{"a", "b"}, nil))
}

func TestSplitList(t *testing.T) {
	assert.Equal(t, []string{"a", "b", "c"}, SplitList(" a, b,,c a "))
	assert.Equal(t, []string{}, SplitList(" , "))
}

func field(name, key, typ string) jira.IssueTypeField {
	f := jira.IssueTypeField{Name: name, Key: key}
	f.Schema.DataType = typ
	return f
}

func TestEditFields(t *testing.T) {
	configured := []jira.IssueTypeField{
		field("Story point estimate", "customfield_10029", "number"),
		field("Story Points", "customfield_10014", "number"),
		field("Team", "customfield_1", "option"),
	}

	assert.Equal(t, []jira.IssueTypeField{configured[1]}, editFields(configured, nil, "classic"))
	assert.Equal(t, []jira.IssueTypeField{configured[0]}, editFields(configured, nil, jira.ProjectTypeNextGen))
	assert.Equal(t, []jira.IssueTypeField{configured[2], configured[0]},
		editFields(configured, []string{"team", "customfield_10029", "missing"}, "classic"))
	assert.Nil(t, editFields(nil, nil, "classic"))
}

func TestFieldText(t *testing.T) {
	assert.Equal(t, "", fieldText(nil))
	assert.Equal(t, "3", fieldText(3.0))
	assert.Equal(t, "2.5", fieldText(2.5))
	assert.Equal(t, "Red", fieldText(map[string]any{"value": "Red"}))
	assert.Equal(t, "x", fieldText("x"))
}
