package view

import (
	"fmt"

	"github.com/ankitpokhrel/jira-cli/api"
	"github.com/ankitpokhrel/jira-cli/pkg/jira"
	"github.com/ankitpokhrel/jira-cli/pkg/jira/filter/issue"
	"github.com/ankitpokhrel/jira-cli/pkg/tui"
)

// EpicIssueFunc provides issues for the epic.
type EpicIssueFunc func(string) []*jira.Issue

// EpicRefreshFunc reloads the list of epics.
type EpicRefreshFunc func() ([]*jira.Issue, error)

// EpicList is a list view for epics.
type EpicList struct {
	Project string
	Server  string
	Data    []*jira.Issue
	Issues  EpicIssueFunc
	Refresh EpicRefreshFunc
	Display DisplayFormat
}

// Render renders the epic explorer view.
//
//nolint:dupl
func (el *EpicList) Render() error {
	renderer, err := MDRenderer()
	if err != nil {
		return err
	}

	data := el.data()
	opts := []tui.PreviewOption{
		tui.WithPreviewFooterText(el.footerText()),
		tui.WithInitialText(el.helpText()),
		tui.WithSidebarSelectedFunc(navigate(el.Server)),
		tui.WithContentTableOpts(
			tui.WithTableStyle(el.Display.TableStyle),
			tui.WithFixedColumns(el.Display.FixedColumns),
			tui.WithSelectedFunc(navigate(el.Server)),
			tui.WithViewModeFunc(func(r, c int, d any) (func() any, func(any) (string, error)) {
				dataFn := func() any {
					data := d.(tui.TableData)
					ci := data.GetIndex(fieldKey)
					iss, _ := api.ProxyGetIssue(api.DefaultClient(false), data.Get(r, ci), issue.NewNumCommentsFilter(1))
					return iss
				}
				renderFn := func(i any) (string, error) {
					iss := Issue{
						Server:  el.Server,
						Data:    i.(*jira.Issue),
						Options: IssueOption{NumComments: 1},
					}
					return iss.RenderedOut(renderer)
				}
				return dataFn, renderFn
			}),
			tui.WithCopyFunc(copyURL(el.Server)),
			tui.WithCopyKeyFunc(copyKey()),
		),
	}
	if el.Refresh != nil {
		opts = append(opts, tui.WithPreviewRefreshFunc(func() ([]tui.PreviewData, string, error) {
			epics, err := el.Refresh()
			if err != nil {
				return nil, "", err
			}
			el.Data = epics
			return el.data(), el.footerText(), nil
		}))
	}

	return tui.NewPreview(opts...).Paint(data)
}

func (el *EpicList) footerText() string {
	return fmt.Sprintf("Showing %d results for project %q", len(el.Data), el.Project)
}

func (el *EpicList) helpText() string {
	if el.Refresh == nil {
		return helpText
	}
	return helpText + "\n\n\tPress 'r' (or 'CTRL+R' / 'F5') to refresh the epics and their issues."
}

func (el *EpicList) data() []tui.PreviewData {
	data := make([]tui.PreviewData, 0, len(el.Data))

	data = append(data, tui.PreviewData{
		Key:  "help",
		Menu: "?",
		Contents: func(_ string) any {
			return el.helpText()
		},
	})
	for _, issue := range el.Data {
		data = append(data, tui.PreviewData{
			Key:  issue.Key,
			Menu: fmt.Sprintf("➤ %s: %s", issue.Key, prepareTitle(issue.Fields.Summary)),
			Contents: func(key string) any {
				issues := el.Issues(key)
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
