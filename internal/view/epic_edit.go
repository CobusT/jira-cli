package view

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ankitpokhrel/jira-cli/internal/issueops"
	"github.com/ankitpokhrel/jira-cli/pkg/jira"
	"github.com/ankitpokhrel/jira-cli/pkg/tui"
)

const (
	pickMe         = "me"
	pickUnassigned = "unassigned"
)

// epicEditHelp describes the edit keys of the epic explorer.
const epicEditHelp = "\n\tEditing (on the selected epic or issue):" +
	"\n\t  - 'm': change the status." +
	"\n\t  - 'a': assign to me, unassign, or search for a user." +
	"\n\t  - 'e': edit the summary, priority, labels and story points." +
	"\n\t  - 'SHIFT+E': edit the description in $JIRA_EDITOR / $EDITOR." +
	"\n\t  - 'SHIFT+C': add a comment in your editor." +
	"\n\t  - 'SHIFT+M': move the selected issue to another epic." +
	"\n\t  - 'o': create an issue in the selected epic."

// rowActions returns the edit actions of the epic explorer, or nil when
// editing isn't available.
func (el *EpicList) rowActions() map[rune]tui.RowAction {
	if el.Ops == nil {
		return nil
	}
	return map[rune]tui.RowAction{
		'm': el.transitionAction,
		'a': el.assignAction,
		'e': el.editAction,
		'E': el.descriptionAction,
		'C': el.commentAction,
		'M': el.moveAction,
		'o': el.createAction,
	}
}

// change describes a change to an issue made from the explorer.
type change struct {
	status  string
	success string
	// row is the row the change was made on.
	row tui.RowTarget
	// alsoEpics are other epics whose issues change, eg: the epic an issue
	// moves to.
	alsoEpics []string
	// selectKey is selected after the change; defaults to the row's key.
	selectKey string
	fn        func() error
	// result, when set, gives the success message and the key to select
	// once fn ran, eg: for a created issue whose key isn't known before.
	result func() (success, selectKey string)
}

// apply runs the change in the background, reloads what it touched and
// reports the result.
func (el *EpicList) apply(ui tui.ActionUI, c change) {
	epics := append([]string{c.row.Parent}, c.alsoEpics...)

	ui.Run(c.status, func() error {
		if err := c.fn(); err != nil {
			return err
		}
		if c.row.IsParent {
			epic, err := el.Ops.Get(c.row.Key)
			if err != nil {
				return err
			}
			el.replaceEpic(epic)
		}
		for _, e := range epics {
			el.dropIssues(e)
		}
		return nil
	}, func(err error) {
		if err != nil {
			return
		}
		success, sel := c.success, c.selectKey
		if c.result != nil {
			success, sel = c.result()
		}
		if sel == "" {
			sel = c.row.Key
		}
		ui.Reload(epics, sel)
		ui.Notify(success)
	})
}

// replaceEpic replaces the epic with the same key in the list.
func (el *EpicList) replaceEpic(epic *jira.Issue) {
	el.stateMu.Lock()
	defer el.stateMu.Unlock()
	for i, e := range el.Data {
		if e.Key == epic.Key {
			el.Data[i] = epic
			return
		}
	}
}

// dropIssues forgets the cached issues of an epic so they are fetched again.
func (el *EpicList) dropIssues(epic string) {
	el.issuesMu.Lock()
	defer el.issuesMu.Unlock()
	delete(el.issuesCache, epic)
}

func needKey(ui tui.ActionUI, row tui.RowTarget) bool {
	if row.Key == "" {
		ui.Notify("Select an issue first")
		return false
	}
	return true
}

func (el *EpicList) transitionAction(ui tui.ActionUI, row tui.RowTarget) {
	if !needKey(ui, row) {
		return
	}
	key := row.Key

	var transitions []*jira.Transition
	ui.Run(fmt.Sprintf("Loading transitions for %s...", key), func() (err error) {
		transitions, err = el.Ops.Transitions(key)
		return err
	}, func(err error) {
		if err != nil {
			return
		}
		if len(transitions) == 0 {
			ui.Notify(fmt.Sprintf("No transitions available for %s", key))
			return
		}
		items := make([]tui.PickerItem, 0, len(transitions))
		for _, t := range transitions {
			items = append(items, tui.PickerItem{Label: t.Name, Value: t})
		}
		ui.Pick(tui.Picker{
			Title: fmt.Sprintf("Move %s to", key),
			Items: items,
			OnPick: func(it tui.PickerItem) {
				tr := it.Value.(*jira.Transition)
				el.apply(ui, change{
					status:  fmt.Sprintf("Moving %s to %q...", key, tr.Name),
					success: fmt.Sprintf("Moved %s to %q", key, tr.Name),
					row:     row,
					fn:      func() error { return el.Ops.Transition(key, tr) },
				})
			},
		})
	})
}

func (el *EpicList) assignAction(ui tui.ActionUI, row tui.RowTarget) {
	if !needKey(ui, row) {
		return
	}
	key := row.Key

	ui.Pick(tui.Picker{
		Title: fmt.Sprintf("Assign %s", key),
		Fixed: []tui.PickerItem{
			{Label: "Me", Value: pickMe},
			{Label: "Unassigned", Value: pickUnassigned},
		},
		Search: func(q string) ([]tui.PickerItem, error) {
			users, err := el.Ops.SearchUsers(q)
			if err != nil {
				return nil, err
			}
			items := make([]tui.PickerItem, 0, len(users))
			for _, u := range users {
				if !u.Active {
					continue
				}
				label := u.DisplayName
				if u.Email != "" {
					label += " <" + u.Email + ">"
				}
				items = append(items, tui.PickerItem{Label: label, Value: u})
			}
			return items, nil
		},
		OnPick: func(it tui.PickerItem) {
			var (
				user *jira.User
				name string
			)
			switch v := it.Value.(type) {
			case *jira.User:
				user, name = v, v.DisplayName
			case string:
				name = it.Label
			}
			el.apply(ui, change{
				status:  fmt.Sprintf("Assigning %s...", key),
				success: assignMessage(key, name, user == nil && it.Value == pickUnassigned),
				row:     row,
				fn: func() error {
					if it.Value == pickMe {
						me, err := el.Ops.Me()
						if err != nil {
							return err
						}
						user = me
					}
					return el.Ops.Assign(key, user)
				},
			})
		},
	})
}

func assignMessage(key, name string, unassigned bool) string {
	if unassigned {
		return fmt.Sprintf("Unassigned %s", key)
	}
	return fmt.Sprintf("Assigned %s to %s", key, name)
}

// Edit form field names.
const (
	formSummary  = "summary"
	formPriority = "priority"
	formLabels   = "labels"
	formType     = "type"
	formAssignee = "assignee"
)

func (el *EpicList) editAction(ui tui.ActionUI, row tui.RowTarget) {
	if !needKey(ui, row) {
		return
	}
	key := row.Key

	var (
		issue      *jira.Issue
		custom     map[string]string
		priorities []string
	)
	ui.Run(fmt.Sprintf("Loading %s...", key), func() (err error) {
		if issue, err = el.Ops.Get(key); err != nil {
			return err
		}
		if custom, err = el.Ops.CustomFieldValues(key); err != nil {
			return err
		}
		// The priorities are optional; the field falls back to free text.
		priorities, _ = el.Ops.Priorities()
		return nil
	}, func(err error) {
		if err != nil {
			return
		}
		form := editForm(issue, custom, priorities, el.Ops.EditFields)
		form.OnSubmit = func(values map[string]string) {
			ch := formChanges(issue, custom, values, el.Ops.EditFields)
			if isNoChange(ch) {
				ui.Notify("No changes")
				return
			}
			el.apply(ui, change{
				status:  fmt.Sprintf("Updating %s...", key),
				success: fmt.Sprintf("Updated %s", key),
				row:     row,
				fn:      func() error { return el.Ops.Edit(issue, ch) },
			})
		}
		ui.Form(form)
	})
}

func editForm(issue *jira.Issue, custom map[string]string, priorities []string, fields []jira.IssueTypeField) tui.Form {
	prio := issue.Fields.Priority.Name
	if len(priorities) > 0 && prio != "" && !slices.Contains(priorities, prio) {
		priorities = append([]string{prio}, priorities...)
	}

	form := tui.Form{
		Title: fmt.Sprintf("Edit %s", issue.Key),
		Fields: []tui.FormField{
			{Name: formSummary, Label: "Summary", Value: issue.Fields.Summary},
			{Name: formPriority, Label: "Priority", Value: prio, Options: priorities},
			{Name: formLabels, Label: "Labels", Value: strings.Join(issue.Fields.Labels, ", ")},
		},
	}
	for _, f := range fields {
		form.Fields = append(form.Fields, tui.FormField{
			Name:    f.Key,
			Label:   f.Name,
			Value:   custom[f.Key],
			Numeric: f.Schema.DataType == "number",
		})
	}
	return form
}

// formChanges returns the changes between the issue and the form values.
func formChanges(issue *jira.Issue, custom, values map[string]string, fields []jira.IssueTypeField) issueops.Changes {
	var ch issueops.Changes

	if s := values[formSummary]; s != "" && s != issue.Fields.Summary {
		ch.Summary = s
	}
	if p := values[formPriority]; p != "" && p != issue.Fields.Priority.Name {
		ch.Priority = p
	}
	labels := issueops.SplitList(values[formLabels])
	if len(issueops.LabelChanges(issue.Fields.Labels, labels)) > 0 {
		ch.Labels = labels
	}
	for _, f := range fields {
		// Clearing a field isn't supported; an empty value is left as is.
		if v := values[f.Key]; v != "" && v != custom[f.Key] {
			if ch.CustomFields == nil {
				ch.CustomFields = make(map[string]string)
			}
			ch.CustomFields[f.Key] = v
		}
	}
	return ch
}

func isNoChange(ch issueops.Changes) bool {
	return ch.Summary == "" && ch.Priority == "" && ch.Labels == nil &&
		len(ch.CustomFields) == 0 && ch.Description == ""
}

func (el *EpicList) descriptionAction(ui tui.ActionUI, row tui.RowTarget) {
	if !needKey(ui, row) {
		return
	}
	key := row.Key

	var issue *jira.Issue
	ui.Run(fmt.Sprintf("Loading %s...", key), func() (err error) {
		issue, err = el.Ops.Get(key)
		return err
	}, func(err error) {
		if err != nil {
			return
		}
		old, _ := issueops.Description(issue)
		ui.EditText(old, func(text string, err error) {
			switch {
			case err != nil:
				ui.Notify(fmt.Sprintf("Error: %s", err))
			case strings.TrimSpace(text) == strings.TrimSpace(old):
				ui.Notify("Description unchanged")
			case strings.TrimSpace(text) == "":
				ui.Notify("Clearing a description isn't supported; description unchanged")
			default:
				el.apply(ui, change{
					status:  fmt.Sprintf("Updating the description of %s...", key),
					success: fmt.Sprintf("Updated the description of %s", key),
					row:     row,
					fn:      func() error { return el.Ops.Edit(issue, issueops.Changes{Description: text}) },
				})
			}
		})
	})
}

func (el *EpicList) commentAction(ui tui.ActionUI, row tui.RowTarget) {
	if !needKey(ui, row) {
		return
	}
	key := row.Key

	ui.EditText("", func(text string, err error) {
		switch {
		case err != nil:
			ui.Notify(fmt.Sprintf("Error: %s", err))
		case strings.TrimSpace(text) == "":
			ui.Notify("Empty comment discarded")
		default:
			ui.Run(fmt.Sprintf("Adding a comment to %s...", key), func() error {
				return el.Ops.Comment(key, text)
			}, func(err error) {
				if err == nil {
					ui.Notify(fmt.Sprintf("Commented on %s", key))
				}
			})
		}
	})
}

func (el *EpicList) moveAction(ui tui.ActionUI, row tui.RowTarget) {
	if row.IsParent {
		ui.Notify("Select an issue in an epic to move it to another epic")
		return
	}
	if !needKey(ui, row) {
		return
	}
	key := row.Key

	epics := el.sortedEpics()
	items := make([]tui.PickerItem, 0, len(epics))
	for _, e := range epics {
		if e.Key == row.Parent {
			continue
		}
		items = append(items, tui.PickerItem{Label: fmt.Sprintf("%s: %s", e.Key, prepareTitle(e.Fields.Summary)), Value: e.Key})
	}
	ui.Pick(tui.Picker{
		Title: fmt.Sprintf("Move %s to epic", key),
		Items: items,
		OnPick: func(it tui.PickerItem) {
			epic := it.Value.(string)
			el.apply(ui, change{
				status:    fmt.Sprintf("Moving %s to %s...", key, epic),
				success:   fmt.Sprintf("Moved %s to %s", key, epic),
				row:       row,
				alsoEpics: []string{epic},
				fn:        func() error { return el.Ops.MoveToEpic(key, epic) },
			})
		},
	})
}

func (el *EpicList) createAction(ui tui.ActionUI, row tui.RowTarget) {
	epic := row.Parent
	defaultType := el.Ops.IssueTypes[0]
	if slices.Contains(el.Ops.IssueTypes, "Story") {
		defaultType = "Story"
	}

	ui.Form(tui.Form{
		Title: fmt.Sprintf("New issue in %s", epic),
		Fields: []tui.FormField{
			{Name: formType, Label: "Type", Value: defaultType, Options: el.Ops.IssueTypes},
			{Name: formSummary, Label: "Summary"},
			{Name: formAssignee, Label: "Assignee", Value: "Me", Options: []string{"Me", "Unassigned"}},
		},
		OnSubmit: func(values map[string]string) {
			summary := values[formSummary]
			if summary == "" {
				ui.Notify("A summary is required; issue not created")
				return
			}
			var created string
			epicRow := tui.RowTarget{Key: epic, Parent: epic}
			el.expandInTree(epic)
			el.apply(ui, change{
				status: fmt.Sprintf("Creating an issue in %s...", epic),
				row:    epicRow,
				fn: func() error {
					ni := issueops.NewIssue{Epic: epic, IssueType: values[formType], Summary: summary}
					if values[formAssignee] == "Me" {
						me, err := el.Ops.Me()
						if err != nil {
							return err
						}
						ni.Assignee = me
					}
					key, err := el.Ops.Create(ni)
					created = key
					return err
				},
				result: func() (string, string) {
					return fmt.Sprintf("Created %s in %s", created, epic), created
				},
			})
		},
	})
}

// expandInTree marks an epic as expanded in the tree layout, so a new issue
// in it is visible there.
func (el *EpicList) expandInTree(epic string) {
	if el.treeExpanded != nil {
		el.treeExpanded[epic] = true
	}
}
