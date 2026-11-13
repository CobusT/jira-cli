package list

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/ankitpokhrel/jira-cli/api"
	"github.com/ankitpokhrel/jira-cli/internal/cmd/issue/list"
	"github.com/ankitpokhrel/jira-cli/internal/cmdutil"
	"github.com/ankitpokhrel/jira-cli/internal/query"
	"github.com/ankitpokhrel/jira-cli/internal/view"
	"github.com/ankitpokhrel/jira-cli/pkg/jira"
	"github.com/ankitpokhrel/jira-cli/pkg/tui"
)

const (
	helpText = `List lists top 100 epics.

By default epics are displayed in an explorer view. You can use --table
and --plain flags to display output in different modes.

Epics and the issues within them are filtered to open ones (status category is
not Done) by default. Use --include-done to show done epics and issues too, or
pass --status/--resolution to filter explicitly.

The default sort order of epics can be set in the config file, eg:

  epic:
    order_by: rank   # any JQL order by field, eg: rank, updated, created
    order: asc       # asc or desc; defaults to asc for rank, desc otherwise

The --order-by and --reverse flags take precedence over the config. In the
explorer view, press 's' to cycle the sort field and 'S' to reverse it. They
sort the focused pane: epics in the sidebar, issues in the issues pane (or,
in the tree layout, issues when an issue row is selected).

The explorer has two layouts: "panes" (epics on the left, the selected epic's
issues on the right) and "tree" (a single list where TAB expands an epic in
place). Press 'V' to switch between them. The initial layout can be set with
--layout or in the config:

  epic:
    layout: tree   # panes (default) or tree`

	examples = `# Display epics in an explorer view
$ jira epic list

# Display epics or epic issues in an interactive table view
$ jira epic list --table
$ jira epic list <KEY>

# Display epics or epic issues in a plain table view
$ jira epic list --table --plain
$ jira epic list <KEY> --plain

# Display epics or epic issues in a plain table view without headers
$ jira epic list --table --plain --no-headers
$ jira epic list <KEY> --plain --no-headers

# Include done epics and done issues within epics
$ jira epic list --include-done
$ jira epic list <KEY> --include-done

# Open the explorer in the single list (tree) layout
$ jira epic list --layout tree

# Order epics by board rank (backlog order)
$ jira epic list --order-by rank --reverse

# Display some columns of epic or epic issues in a plain table view
$ jira epic list --table --plain --columns key,summary,status
$ jira epic list <KEY> --plain --columns type,key,summary`
)

// NewCmdList is a list command.
func NewCmdList() *cobra.Command {
	return &cobra.Command{
		Use:     "list [EPIC-KEY]",
		Short:   "List lists issues in a project",
		Long:    helpText,
		Example: examples,
		Aliases: []string{"lists", "ls"},
		Annotations: map[string]string{
			"help:args": "[EPIC-KEY]\tKey for the issue of type epic, eg: ISSUE-1",
		},
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			err := cmd.Flags().Set("type", "Epic")
			cmdutil.ExitIfError(err)

			epicList(cmd, args)
		},
	}
}

// SetFlags sets flags supported by an epic list command.
func SetFlags(cmd *cobra.Command) {
	setFlags(cmd)
	hideFlags(cmd)
}

func epicList(cmd *cobra.Command, args []string) {
	server := viper.GetString("server")
	project := viper.GetString("project.key")
	projectType := viper.GetString("project.type")

	debug, err := cmd.Flags().GetBool("debug")
	cmdutil.ExitIfError(err)

	client := api.DefaultClient(debug)

	if len(args) == 0 {
		cmdutil.ExitIfError(applyEpicOrderConfig(cmd))
		epicExplorerView(cmd, cmd.Flags(), project, projectType, server, client)
	} else {
		key := cmdutil.GetJiraIssueKey(project, args[0])
		singleEpicView(cmd.Flags(), key, project, projectType, server, client)
	}
}

func singleEpicView(flags query.FlagParser, key, project, projectType, server string, client *jira.Client) {
	err := flags.Set("type", "") // Unset issue type.
	cmdutil.ExitIfError(err)

	issues, err := func() ([]*jira.Issue, error) {
		s := cmdutil.Info("Fetching epic issues...")
		defer s.Stop()

		q, err := query.NewIssue(project, flags)
		if err != nil {
			return nil, err
		}
		if err := applyOpenOnlyFilter(flags, q); err != nil {
			return nil, err
		}

		var resp *jira.SearchResult

		if projectType == jira.ProjectTypeNextGen {
			q.Params().Parent = key
			q.Params().IssueType = ""

			resp, err = client.Search(q.Get(), q.Params().Limit)
		} else {
			resp, err = client.EpicIssues(key, q.Get(), q.Params().From, q.Params().Limit)
		}

		if err != nil {
			return nil, err
		}
		return resp.Issues, nil
	}()
	cmdutil.ExitIfError(err)

	if len(issues) == 0 {
		fmt.Println()
		cmdutil.Failed("No result found for given query in project %q", project)
		return
	}

	plain, err := flags.GetBool("plain")
	cmdutil.ExitIfError(err)

	delimiter, err := flags.GetString("delimiter")
	cmdutil.ExitIfError(err)

	csv, err := flags.GetBool("csv")
	cmdutil.ExitIfError(err)

	noHeaders, err := flags.GetBool("no-headers")
	cmdutil.ExitIfError(err)

	noTruncate, err := flags.GetBool("no-truncate")
	cmdutil.ExitIfError(err)

	fixedColumns, err := flags.GetUint("fixed-columns")
	cmdutil.ExitIfError(err)

	columns, err := flags.GetString("columns")
	cmdutil.ExitIfError(err)

	v := view.IssueList{
		Project: project,
		Server:  server,
		Data:    issues,
		Refresh: func() {
			singleEpicView(flags, key, project, projectType, server, client)
		},
		Display: view.DisplayFormat{
			Plain:        plain,
			Delimiter:    delimiter,
			CSV:          csv,
			NoHeaders:    noHeaders,
			NoTruncate:   noTruncate,
			FixedColumns: fixedColumns,
			Columns: func() []string {
				if columns != "" {
					return strings.Split(columns, ",")
				}
				return []string{}
			}(),
			TableStyle: cmdutil.GetTUIStyleConfig(),
			Timezone:   viper.GetString("timezone"),
		},
	}

	cmdutil.ExitIfError(v.Render())
}

func epicExplorerView(cmd *cobra.Command, flags query.FlagParser, project, projectType, server string, client *jira.Client) {
	q, err := query.NewIssue(project, flags)
	cmdutil.ExitIfError(err)
	userJQL := q.Params().JQL

	openOnly, err := isOpenOnly(flags)
	cmdutil.ExitIfError(err)

	// fetchEpics builds a fresh query each time since the issues func below
	// mutates the shared query params for next-gen projects.
	fetchEpics := func() ([]*jira.Issue, error) {
		eq, err := query.NewIssue(project, flags)
		if err != nil {
			return nil, err
		}
		if openOnly {
			eq.Params().JQL = withOpenOnly(eq.Params().JQL)
		}
		resp, err := api.ProxySearch(client, eq.Get(), eq.Params().From, eq.Params().Limit)
		if err != nil {
			return nil, err
		}
		return resp.Issues, nil
	}

	epics, err := func() ([]*jira.Issue, error) {
		s := cmdutil.Info("Fetching epics...")
		defer s.Stop()

		return fetchEpics()
	}()
	cmdutil.ExitIfError(err)

	if len(epics) == 0 {
		fmt.Println()
		cmdutil.Failed("No result found for given query in project %q", project)
		return
	}

	fixedColumns, err := flags.GetUint("fixed-columns")
	cmdutil.ExitIfError(err)

	v := view.EpicList{
		Project: project,
		Server:  server,
		Data:    epics,
		// Issues is called concurrently from the TUI, so it must not share
		// mutable state: it uses its own err and, for next-gen projects, its own query.
		Issues: func(key string) []*jira.Issue {
			var (
				resp *jira.SearchResult
				err  error
			)

			if projectType == jira.ProjectTypeNextGen {
				iq, qerr := query.NewIssue(project, flags)
				if qerr != nil {
					return []*jira.Issue{}
				}
				iq.Params().Parent = key
				iq.Params().IssueType = ""
				if openOnly {
					iq.Params().JQL = withOpenOnly(userJQL)
				}

				resp, err = client.Search(iq.Get(), iq.Params().Limit)
			} else {
				childJQL := ""
				if openOnly {
					childJQL = openOnlyJQL
				}
				resp, err = client.EpicIssues(key, childJQL, q.Params().From, q.Params().Limit)
			}
			if err != nil {
				return []*jira.Issue{}
			}
			return resp.Issues
		},
		Refresh:      fetchEpics,
		Layout:       epicLayout(flags),
		DefaultOrder: orderLabel(q.Params().OrderBy, q.Params().Reverse),
		Display: view.DisplayFormat{
			FixedColumns: fixedColumns,
			TableStyle:   cmdutil.GetTUIStyleConfig(),
			Timezone:     viper.GetString("timezone"),
		},
	}

	plain, err := flags.GetBool("plain")
	cmdutil.ExitIfError(err)

	table, err := flags.GetBool("table")
	cmdutil.ExitIfError(err)

	if table || plain || tui.IsDumbTerminal() || tui.IsNotTTY() {
		if openOnly {
			cmdutil.ExitIfError(cmd.Flags().Set("jql", withOpenOnly(userJQL)))
		}
		list.List(cmd, nil)
	} else {
		cmdutil.ExitIfError(v.Render())
	}
}

const openOnlyJQL = "statusCategory != Done"

// applyEpicOrderConfig applies the default epic order from the config
// (epic.order_by and epic.order) unless --order-by or --reverse were passed.
func applyEpicOrderConfig(cmd *cobra.Command) error {
	flags := cmd.Flags()
	if flags.Changed("order-by") || flags.Changed("reverse") {
		return nil
	}
	orderBy := strings.TrimSpace(viper.GetString("epic.order_by"))
	if orderBy == "" {
		return nil
	}
	if err := flags.Set("order-by", orderBy); err != nil {
		return err
	}

	order := strings.ToLower(strings.TrimSpace(viper.GetString("epic.order")))
	switch order {
	case "":
		if strings.EqualFold(orderBy, "rank") {
			order = "asc"
		}
	case "asc", "desc":
	default:
		return fmt.Errorf("invalid epic.order %q in config: must be asc or desc", order)
	}
	// The --reverse flag switches the default DESC order to ASC.
	if order == "asc" {
		return flags.Set("reverse", "true")
	}
	return nil
}

// epicLayout returns the explorer layout from the --layout flag or the
// epic.layout config, defaulting to panes.
func epicLayout(flags query.FlagParser) view.EpicLayout {
	layout, err := flags.GetString("layout")
	cmdutil.ExitIfError(err)
	if layout == "" {
		layout = viper.GetString("epic.layout")
	}
	layout = strings.ToLower(strings.TrimSpace(layout))

	switch view.EpicLayout(layout) {
	case "", view.EpicLayoutPanes:
		return view.EpicLayoutPanes
	case view.EpicLayoutTree:
		return view.EpicLayoutTree
	}
	cmdutil.Failed("Invalid layout %q: must be one of %s", layout, strings.Join(view.ValidEpicLayouts(), ", "))
	return view.EpicLayoutPanes
}

func orderLabel(orderBy string, ascending bool) string {
	if ascending {
		return orderBy + " ↑"
	}
	return orderBy + " ↓"
}

// isOpenOnly reports whether epic child issues should be limited to open ones.
// This is the default unless --include-done is passed or the user filters by
// status or resolution explicitly.
func isOpenOnly(flags query.FlagParser) (bool, error) {
	includeDone, err := flags.GetBool("include-done")
	if err != nil {
		return false, err
	}
	if includeDone {
		return false, nil
	}
	status, err := flags.GetStringArray("status")
	if err != nil {
		return false, err
	}
	resolution, err := flags.GetString("resolution")
	if err != nil {
		return false, err
	}
	return len(status) == 0 && resolution == "", nil
}

func withOpenOnly(jql string) string {
	if jql == "" {
		return openOnlyJQL
	}
	return fmt.Sprintf("(%s) AND %s", jql, openOnlyJQL)
}

func applyOpenOnlyFilter(flags query.FlagParser, q *query.Issue) error {
	openOnly, err := isOpenOnly(flags)
	if err != nil {
		return err
	}
	if openOnly {
		q.Params().JQL = withOpenOnly(q.Params().JQL)
	}
	return nil
}

func setFlags(cmd *cobra.Command) {
	list.SetFlags(cmd)
	cmd.Flags().Bool("table", false, "Display epics in table view")
	cmd.Flags().String("layout", "", "Explorer layout: panes or tree (default from config epic.layout, else panes)")
	cmd.Flags().Bool("include-done", false, "Include done issues when listing issues in an epic (default shows only open issues)")
}

func hideFlags(cmd *cobra.Command) {
	cmdutil.ExitIfError(cmd.Flags().MarkHidden("type"))
	cmdutil.ExitIfError(cmd.Flags().MarkHidden("parent"))
}
