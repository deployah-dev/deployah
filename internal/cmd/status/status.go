package status

import (
	"errors"
	"fmt"
	"sort"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/cli"
	"deployah.dev/deployah/internal/cmd/common"
	"deployah.dev/deployah/internal/k8s"
	"deployah.dev/deployah/internal/session"
)

// Options holds command-line flags for status.
type Options struct {
	Project      string `nabat:"project"`
	OutputFormat string `nabat:"output"`
	Environment  string `nabat:"environment"`
	Detailed     bool   `nabat:"detailed"`
}

// Register adds the status command to app.
func Register(app *nabat.App) {
	app.MustCommand("status",
		nabat.WithDescription("Display the status of a project"),
		nabat.WithLongDescription("Display detailed status information about a deployed project, including its current state, revision, and resources."),
		nabat.WithArg("project", "", nabat.WithRequired(), nabat.WithUsage("Project name to show status for"), nabat.WithPrompt("Project name", "", nabat.WithHint("e.g. my-app"))),
		nabat.WithSelectFlag("output", cli.OutputFormatTable, cli.OutputFormats, nabat.WithShort('o'), nabat.WithUsage("Output format")),
		nabat.WithFlag("environment", "", nabat.WithShort('e'), nabat.WithUsage("Environment to display status for")),
		nabat.WithFlag("detailed", false, nabat.WithUsage("Show detailed pod information")),
		nabat.WithExample(`
# Display status for a specific project
deployah status my-app

# Display status for a specific project in a specific environment
deployah status my-app --environment prod

# Show detailed pod information
deployah status my-app --detailed`),
		nabat.WithRun(runStatus),
	)
}

func runStatus(c *nabat.Context) error {
	opts := &Options{}
	if err := c.Bind(opts); err != nil {
		return fmt.Errorf("binding options: %w", err)
	}

	rt := session.FromContext(c)
	cluster, err := rt.Target(c, opts.Environment)
	if err != nil {
		return fmt.Errorf("target cluster: %w", err)
	}
	common.WarnContextFallback(c, cluster, opts.Environment)
	helmClient, err := cluster.Helm()
	if err != nil {
		return fmt.Errorf("helm client: %w", err)
	}

	selector, err := k8s.BuildLabelSelector(opts.Project, opts.Environment)
	if err != nil {
		return fmt.Errorf("build selector: %w", err)
	}

	releases, err := helmClient.ListReleases(c, selector)
	if err != nil {
		return fmt.Errorf("list releases: %w", err)
	}

	if len(releases) == 0 {
		msg := fmt.Sprintf("no releases found for project '%s'", opts.Project)
		if opts.Environment != "" {
			msg += fmt.Sprintf(" in environment '%s'", opts.Environment)
		}
		return errors.New(msg + "\n\nHint: Use 'deployah list' to see all available projects and environments")
	}

	sort.Slice(releases, func(i, j int) bool {
		return releases[i].Name < releases[j].Name
	})

	var k8sClient *k8s.Client
	if opts.Detailed {
		clientset, k8sErr := cluster.Kubernetes()
		if k8sErr != nil {
			return fmt.Errorf("k8s client: %w", k8sErr)
		}
		k8sClient = k8s.NewClient(clientset, cluster.Namespace())
	}

	headers := []string{"PROJECT", "ENV", "STATUS", "REV", "AGE", "NAMESPACE"}
	if opts.Detailed {
		headers = append(headers, "PODS", "READY")
	}

	rows := make([][]string, 0, len(releases))
	viewModels := make([]cli.ReleaseViewModel, 0, len(releases))

	for _, rel := range releases {
		var vm cli.ReleaseViewModel
		if opts.Detailed && k8sClient != nil {
			vm = cli.ReleaseToViewModelWithPods(c, k8sClient, rel)
		} else {
			vm = cli.ReleaseToViewModel(rel)
		}

		row := []string{
			vm.Project,
			vm.Environment,
			fmt.Sprintf("● %s", vm.Status),
			fmt.Sprintf("%d", vm.Revision),
			vm.Age,
			vm.Namespace,
		}
		if opts.Detailed {
			if vm.PodCount > 0 {
				row = append(row, fmt.Sprintf("%d", vm.PodCount), vm.PodStatus)
			} else {
				row = append(row, "", "")
			}
		}

		rows = append(rows, row)
		viewModels = append(viewModels, vm)
	}

	return cli.Render(c, opts.OutputFormat, headers, rows, viewModels)
}
