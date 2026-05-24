package list

import (
	"fmt"

	"deployah.dev/deployah/internal/cli"
	"deployah.dev/deployah/internal/k8s"
	"deployah.dev/deployah/internal/runtime"
	"nabat.dev/nabat"
)

type Options struct {
	OutputFormat string `nabat:"output"`
	Project      string `nabat:"project"`
	Environment  string `nabat:"environment"`
}

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

	rt := runtime.FromContext(c)
	helmClient, err := rt.Helm()
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

	// Filter nil releases
	valid := releases[:0]
	for _, r := range releases {
		if r != nil {
			valid = append(valid, r)
		}
	}

	if len(valid) == 0 {
		c.Info("No releases found", "project", opts.Project, "environment", opts.Environment)
		return nil
	}

	headers := []string{"PROJECT", "ENV", "STATUS", "REV", "AGE", "NAMESPACE"}
	rows := make([][]string, 0, len(valid))
	jsonData := make([]map[string]any, 0, len(valid))

	for _, rel := range valid {
		vm := cli.ReleaseToViewModel(rel)
		rows = append(rows, []string{
			vm.Project,
			vm.Environment,
			fmt.Sprintf("● %s", vm.Status),
			fmt.Sprintf("%d", vm.Revision),
			vm.Age,
			vm.Namespace,
		})
		jsonData = append(jsonData, map[string]any{
			"project":     vm.Project,
			"environment": vm.Environment,
			"status":      vm.Status,
			"revision":    vm.Revision,
			"age":         vm.Age,
			"namespace":   vm.Namespace,
		})
	}

	return cli.Render(c, opts.OutputFormat, headers, rows, jsonData)
}
