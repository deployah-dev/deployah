package deploy

import (
	"fmt"

	"deployah.dev/deployah/internal/runtime"
	"nabat.dev/nabat"
)

type Options struct {
	Environment string `nabat:"environment"`
	DryRun      bool   `nabat:"dry-run"`
}

func Register(app *nabat.App) {
	app.MustCommand("deploy",
		nabat.WithDescription("Deploy a project to a Kubernetes cluster on a given environment"),
		nabat.WithLongDescription("Deploy a project to a Kubernetes cluster on a given environment."),
		nabat.WithArg("environment", "", nabat.WithRequired(), nabat.WithUsage("Environment to deploy to"), nabat.WithPrompt("Environment", "", nabat.WithHint("e.g. prod, staging"))),
		nabat.WithFlag("dry-run", false, nabat.WithShort('d'), nabat.WithUsage("Perform a dry run (render templates without installing)")),
		nabat.WithExample(`
# Deploy to production using default manifest path (./deployah.yaml)
deployah deploy prod

# Deploy to staging with an explicit manifest path
deployah deploy staging -c ./path/to/deployah.yaml

# Deploy to production with a dry run
deployah deploy prod --dry-run`),
		nabat.WithRun(runDeploy),
	)
}

func runDeploy(c *nabat.Context) error {
	opts := &Options{}
	if err := c.Bind(opts); err != nil {
		return fmt.Errorf("binding options: %w", err)
	}

	c.Logger().Debug("starting deployment process")

	rt := runtime.FromContext(c)

	manifest, err := rt.Manifest(c, opts.Environment)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}

	c.Logger().Debug("manifest loaded", "env", opts.Environment)

	helmClient, err := rt.Helm()
	if err != nil {
		return fmt.Errorf("helm client: %w", err)
	}

	title := fmt.Sprintf("Deploying to '%s'...", opts.Environment)
	if opts.DryRun {
		title = fmt.Sprintf("Dry run for '%s'...", opts.Environment)
	}

	err = c.Spinner(title, func() error {
		return helmClient.InstallApp(c, manifest, opts.Environment, opts.DryRun)
	})
	if err != nil {
		if opts.DryRun {
			return fmt.Errorf("dry run failed: %w", err)
		}
		return fmt.Errorf("deploy failed: %w", err)
	}

	if opts.DryRun {
		c.Success("Dry run completed", "project", manifest.Project, "environment", opts.Environment)
	} else {
		c.Success("Deployed", "project", manifest.Project, "environment", opts.Environment)
	}

	return nil
}
