package list

import (
	"context"
	"fmt"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/action"
	"deployah.dev/deployah/internal/cli"
	"deployah.dev/deployah/internal/cmd/cmdopts"
	"deployah.dev/deployah/internal/session"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// Options holds command-line flags for list.
type Options struct {
	OutputFormat string `nabat:"output"`
	Project      string `nabat:"project"`
	Environment  string `nabat:"environment"`
}

// Register adds the list command to app.
func Register(app *nabat.App) {
	app.MustCommand("list",
		nabat.WithDescription("List deployed projects"),
		nabat.WithLongDescription("List all deployed projects in the current namespace."),
		nabat.WithSelectFlag("output", cli.OutputFormatTable, cli.OutputFormats, nabat.WithShort('o'), nabat.WithUsage("Output format")),
		nabat.WithFlag("project", "", nabat.WithShort('p'), nabat.WithUsage("Filter by project name")),
		nabat.WithFlag("environment", "", nabat.WithShort('e'), nabat.WithUsage("Filter by environment name")),
		nabat.WithExample(`
# List all deployed projects
deployah list

# List projects by project name
deployah list --project my-app

# List projects by environment name
deployah list --environment prod

# List projects filtered by both project and environment
deployah list --project my-app --environment prod`,
		),
		nabat.WithRun(runList),
	)
}

func runList(c *nabat.Context) error {
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

	releases, err := listReleases(c, helmClient, *opts)
	if err != nil {
		return err
	}

	if len(releases) == 0 {
		args := []any{}
		if opts.Project != "" {
			args = append(args, "project", opts.Project)
		}
		if opts.Environment != "" {
			args = append(args, "environment", opts.Environment)
		}
		c.Info("No releases found", args...)
		return nil
	}

	headers, rows, jsonData := listOutput(releases)
	return cli.Render(c, opts.OutputFormat, headers, rows, jsonData)
}

func listReleases(ctx context.Context, lister action.ReleaseLister, opts Options) ([]*v1.Release, error) {
	return action.NewList(lister).Run(ctx, action.ListParams{
		Project:     opts.Project,
		Environment: opts.Environment,
	})
}

func listOutput(releases []*v1.Release) (headers []string, rows [][]string, jsonData []map[string]any) {
	headers = []string{"PROJECT", "ENV", "INSTANCE", "STATUS", "REV", "AGE", "NAMESPACE"}
	rows = make([][]string, 0, len(releases))
	jsonData = make([]map[string]any, 0, len(releases))

	for _, rel := range releases {
		vm := cli.ReleaseToViewModel(rel)
		rows = append(rows, []string{
			vm.Project,
			vm.Environment,
			vm.Instance,
			fmt.Sprintf("● %s", vm.Status),
			fmt.Sprintf("%d", vm.Revision),
			vm.Age,
			vm.Namespace,
		})
		jsonData = append(jsonData, map[string]any{
			"project":     vm.Project,
			"environment": vm.Environment,
			"instance":    vm.Instance,
			"release":     vm.Release,
			"status":      vm.Status,
			"revision":    vm.Revision,
			"age":         vm.Age,
			"namespace":   vm.Namespace,
		})
	}
	return headers, rows, jsonData
}
