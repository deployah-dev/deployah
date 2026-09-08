package status

import (
	"context"
	"errors"
	"fmt"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/action"
	"deployah.dev/deployah/internal/cli"
	"deployah.dev/deployah/internal/cmd/cmdopts"
	"deployah.dev/deployah/internal/k8s"
	"deployah.dev/deployah/internal/session"

	v1 "helm.sh/helm/v4/pkg/release/v1"
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
	cmdopts.WarnContextFallback(c, cluster, opts.Environment)
	helmClient, err := cluster.Helm()
	if err != nil {
		return fmt.Errorf("helm client: %w", err)
	}

	releases, err := statusReleases(c, helmClient, *opts)
	if err != nil {
		return wrapStatusError(err)
	}

	var k8sClient *k8s.Client
	if opts.Detailed {
		clientset, k8sErr := cluster.Kubernetes()
		if k8sErr != nil {
			return fmt.Errorf("k8s client: %w", k8sErr)
		}
		k8sClient = k8s.NewClient(clientset, cluster.Namespace())
	}

	headers, rows, viewModels := statusOutput(releases, func(rel *v1.Release) cli.ReleaseViewModel {
		if opts.Detailed && k8sClient != nil {
			return cli.ReleaseToViewModelWithPods(c, k8sClient, rel)
		}
		return cli.ReleaseToViewModel(rel)
	})
	return cli.Render(c, opts.OutputFormat, headers, rows, viewModels)
}

func statusReleases(ctx context.Context, getter action.ReleaseGetter, opts Options) ([]*v1.Release, error) {
	return action.NewStatus(getter).Run(ctx, action.StatusParams{
		Project:     opts.Project,
		Environment: opts.Environment,
	})
}

func wrapStatusError(err error) error {
	if errors.Is(err, action.ErrNoReleases) {
		return fmt.Errorf("%w\n\nHint: Use 'deployah list' to see all available projects and environments", err)
	}
	return err
}

func statusOutput(releases []*v1.Release, view func(*v1.Release) cli.ReleaseViewModel) (headers []string, rows [][]string, viewModels []cli.ReleaseViewModel) {
	headers = []string{"PROJECT", "ENV", "INSTANCE", "STATUS", "REV", "AGE", "NAMESPACE"}
	rows = make([][]string, 0, len(releases))
	viewModels = make([]cli.ReleaseViewModel, 0, len(releases))

	for _, rel := range releases {
		vm := view(rel)
		rows = append(rows, []string{
			vm.Project,
			vm.Environment,
			vm.Instance,
			fmt.Sprintf("● %s", vm.Status),
			fmt.Sprintf("%d", vm.Revision),
			vm.Age,
			vm.Namespace,
		})
		viewModels = append(viewModels, vm)
	}
	return headers, rows, viewModels
}
