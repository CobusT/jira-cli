// Package issueops holds issue operations shared by the commands and the TUI:
// transitions, assignment, edits, comments, moving issues between epics and
// creating issues in an epic. It has no prompts or output of its own.
package issueops

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/viper"

	"github.com/ankitpokhrel/jira-cli/api"
	"github.com/ankitpokhrel/jira-cli/internal/cmdcommon"
	"github.com/ankitpokhrel/jira-cli/pkg/adf"
	"github.com/ankitpokhrel/jira-cli/pkg/jira"
	"github.com/ankitpokhrel/jira-cli/pkg/md"
)

// Ops runs issue operations against a Jira server.
type Ops struct {
	Client       *jira.Client
	Project      string
	ProjectType  string
	Installation string
	// EpicField is the epic link field of classic projects.
	EpicField string
	// CustomFields are the custom fields configured in the config file.
	CustomFields []jira.IssueTypeField
	// EditFields are the custom fields offered in the edit form, eg: story points.
	EditFields []jira.IssueTypeField
	// IssueTypes are the names of issue types that can be created in an epic.
	IssueTypes []string
}

// New returns issue operations configured from the config file.
func New(client *jira.Client) *Ops {
	ops := &Ops{
		Client:       client,
		Project:      viper.GetString("project.key"),
		ProjectType:  viper.GetString("project.type"),
		Installation: viper.GetString("installation"),
		EpicField:    viper.GetString("epic.link"),
	}
	if cf, err := cmdcommon.GetConfiguredCustomFields(); err == nil {
		ops.CustomFields = cf
	}
	ops.EditFields = editFields(ops.CustomFields, viper.GetStringSlice("epic.edit_fields"), ops.ProjectType)

	var types []struct {
		Name    string `mapstructure:"name"`
		Subtask bool   `mapstructure:"subtask"`
	}
	if err := viper.UnmarshalKey("issue.types", &types); err == nil {
		for _, t := range types {
			if !t.Subtask && !strings.EqualFold(t.Name, jira.IssueTypeEpic) {
				ops.IssueTypes = append(ops.IssueTypes, t.Name)
			}
		}
	}
	if len(ops.IssueTypes) == 0 {
		ops.IssueTypes = []string{"Story", "Task", "Bug"}
	}
	return ops
}

// editFields picks the custom fields for the edit form: the names in the
// epic.edit_fields config or, by default, the story points field.
func editFields(configured []jira.IssueTypeField, names []string, projectType string) []jira.IssueTypeField {
	if len(names) == 0 {
		names = []string{"Story Points", "Story point estimate"}
		if projectType == jira.ProjectTypeNextGen {
			names = []string{"Story point estimate", "Story Points"}
		}
		// Only the first match is used by default.
		for _, n := range names {
			if f, ok := findField(configured, n); ok {
				return []jira.IssueTypeField{f}
			}
		}
		return nil
	}

	fields := make([]jira.IssueTypeField, 0, len(names))
	for _, n := range names {
		if f, ok := findField(configured, n); ok {
			fields = append(fields, f)
		}
	}
	return fields
}

func findField(fields []jira.IssueTypeField, name string) (jira.IssueTypeField, bool) {
	for _, f := range fields {
		if strings.EqualFold(strings.TrimSpace(f.Name), strings.TrimSpace(name)) || strings.EqualFold(f.Key, name) {
			return f, true
		}
	}
	return jira.IssueTypeField{}, false
}

// FieldIdentifier is how a custom field is referred to in an edit request.
func FieldIdentifier(f jira.IssueTypeField) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(f.Name)), " ", "-")
}

// Get fetches an issue.
func (o *Ops) Get(key string) (*jira.Issue, error) {
	return api.ProxyGetIssue(o.Client, key)
}

// Transitions returns the transitions available for an issue.
func (o *Ops) Transitions(key string) ([]*jira.Transition, error) {
	all, err := api.ProxyTransitions(o.Client, key)
	if err != nil {
		return nil, err
	}
	// Only Jira Cloud reliably reports whether a transition is available.
	if o.Installation != jira.InstallationTypeCloud {
		return all, nil
	}
	out := make([]*jira.Transition, 0, len(all))
	for _, t := range all {
		if t.IsAvailable {
			out = append(out, t)
		}
	}
	return out, nil
}

// Transition moves an issue through the given transition.
func (o *Ops) Transition(key string, tr *jira.Transition) error {
	_, err := o.Client.Transition(key, &jira.TransitionRequest{
		Transition: &jira.TransitionRequestData{ID: tr.ID.String(), Name: tr.Name},
	})
	return err
}

// SearchUsers returns users assignable in the project that match the query.
func (o *Ops) SearchUsers(query string) ([]*jira.User, error) {
	return api.ProxyUserSearch(o.Client, &jira.UserSearchOptions{
		Query:      query,
		Project:    o.Project,
		MaxResults: 50,
	})
}

// Me returns the current user as an assignable user.
func (o *Ops) Me() (*jira.User, error) {
	me, err := o.Client.Me()
	if err != nil {
		return nil, err
	}
	return &jira.User{AccountID: me.AccountID, Name: me.Login, DisplayName: me.Name, Email: me.Email}, nil
}

// Assign assigns an issue to the user, or unassigns it when user is nil.
func (o *Ops) Assign(key string, user *jira.User) error {
	return api.ProxyAssignIssue(o.Client, key, user, jira.AssigneeNone)
}

// Priorities returns the names of the issue priorities.
func (o *Ops) Priorities() ([]string, error) {
	ps, err := o.Client.Priorities()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.Name)
	}
	return names, nil
}

// CustomFieldValues returns the current values of the edit fields of an issue
// as text, keyed by field key.
func (o *Ops) CustomFieldValues(key string) (map[string]string, error) {
	out := make(map[string]string, len(o.EditFields))
	if len(o.EditFields) == 0 {
		return out, nil
	}
	raw, err := api.ProxyGetIssueRaw(o.Client, key)
	if err != nil {
		return nil, err
	}
	var data struct {
		Fields map[string]any `json:"fields"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, err
	}
	for _, f := range o.EditFields {
		out[f.Key] = fieldText(data.Fields[f.Key])
	}
	return out, nil
}

func fieldText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case map[string]any:
		for _, k := range []string{"value", "name", "displayName"} {
			if s, ok := t[k].(string); ok {
				return s
			}
		}
	}
	return fmt.Sprint(v)
}

// Description returns the description of an issue as markdown, and whether
// it is stored in the Atlassian document format.
func Description(issue *jira.Issue) (string, bool) {
	switch d := issue.Fields.Description.(type) {
	case *adf.ADF:
		return adf.NewTranslator(d, adf.NewJiraMarkdownTranslator()).Translate(), true
	case string:
		return d, false
	}
	return "", false
}

// Changes are the edits to an issue. Empty values are left unchanged.
type Changes struct {
	Summary  string
	Priority string
	// Labels is the full new set of labels; nil leaves the labels unchanged.
	Labels []string
	// CustomFields maps an edit field key to its new value.
	CustomFields map[string]string
	// Description is the new description as markdown.
	Description string
}

// Edit applies the changes to an issue. old is the issue before the changes.
func (o *Ops) Edit(old *jira.Issue, ch Changes) error {
	req := jira.EditRequest{
		Summary:  ch.Summary,
		Priority: ch.Priority,
	}
	if ch.Labels != nil {
		req.Labels = LabelChanges(old.Fields.Labels, ch.Labels)
	}
	if ch.Description != "" {
		_, isADF := Description(old)
		req.Body = ch.Description
		if isADF {
			req.Body = md.ToJiraMD(ch.Description)
		}
	}
	if len(ch.CustomFields) > 0 {
		req.CustomFields = make(map[string]string, len(ch.CustomFields))
		for key, val := range ch.CustomFields {
			if f, ok := findField(o.EditFields, key); ok {
				req.CustomFields[FieldIdentifier(f)] = val
			}
		}
		req.WithCustomFields(o.EditFields)
	}
	if req.Summary == "" && req.Priority == "" && len(req.Labels) == 0 && req.Body == "" && len(req.CustomFields) == 0 {
		return nil
	}
	// Send the current parent along, like `issue edit` does, so the edit
	// keeps the issue in its epic.
	if old.Fields.Parent != nil {
		req.ParentIssueKey = old.Fields.Parent.Key
	}
	return o.Client.Edit(old.Key, &req)
}

// LabelChanges returns the label edits that turn old into new: added labels,
// and removed ones prefixed with a minus.
func LabelChanges(old, updated []string) []string {
	var out []string
	for _, l := range updated {
		if !slices.Contains(old, l) {
			out = append(out, l)
		}
	}
	for _, l := range old {
		if !slices.Contains(updated, l) {
			out = append(out, "-"+l)
		}
	}
	return out
}

// SplitList splits a comma or space separated list, dropping empty items and duplicates.
func SplitList(s string) []string {
	out := []string{}
	for _, item := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		if !slices.Contains(out, item) {
			out = append(out, item)
		}
	}
	return out
}

// Comment adds a comment, written in markdown, to an issue.
func (o *Ops) Comment(key, body string) error {
	return o.Client.AddIssueComment(key, body, false)
}

// MoveToEpic moves an issue to another epic.
func (o *Ops) MoveToEpic(key, epic string) error {
	if o.ProjectType == jira.ProjectTypeNextGen {
		return o.Client.Edit(key, &jira.EditRequest{ParentIssueKey: epic})
	}
	return o.Client.EpicIssuesAdd(epic, key)
}

// NewIssue is an issue to create in an epic.
type NewIssue struct {
	Epic      string
	IssueType string
	Summary   string
	Priority  string
	// Assignee is optional.
	Assignee *jira.User
}

// Create creates an issue in an epic and returns its key.
func (o *Ops) Create(ni NewIssue) (string, error) {
	cr := jira.CreateRequest{
		Project:        o.Project,
		IssueType:      ni.IssueType,
		ParentIssueKey: ni.Epic,
		Summary:        ni.Summary,
		Priority:       ni.Priority,
		EpicField:      o.EpicField,
	}
	if ni.Assignee != nil {
		cr.Assignee = ni.Assignee.AccountID
		if o.Installation == jira.InstallationTypeLocal {
			cr.Assignee = ni.Assignee.Name
		}
	}
	cr.ForProjectType(o.ProjectType)
	cr.ForInstallationType(o.Installation)

	resp, err := o.Client.CreateV2(&cr)
	if err != nil {
		return "", err
	}
	return resp.Key, nil
}
