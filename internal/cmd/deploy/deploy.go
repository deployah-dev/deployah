package deploy

import (
	"fmt"
	"strings"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/runtime"
	"deployah.dev/deployah/internal/spec"
)

// Options holds command-line flags for deploy.
type Options struct {
	Environment string `nabat:"environment"`
	Context     string `nabat:"context"`
	DryRun      bool   `nabat:"dry-run"`
}

// Register adds the deploy command to app.
func Register(app *nabat.App) {
	app.MustCommand("deploy",
		nabat.WithDescription("Deploy a project to a Kubernetes cluster on a given environment"),
		nabat.WithLongDescription("Deploy a project to a Kubernetes cluster on a given environment."),
		nabat.WithArg("environment", "", nabat.WithRequired(), nabat.WithUsage("Environment to deploy to"), nabat.WithPrompt("Environment", "", nabat.WithHint("e.g. prod, staging"))),
		nabat.WithFlag("dry-run", false, nabat.WithUsage("Perform a dry run (render templates without installing)")),
		nabat.WithExample(`
# Deploy to production using the default spec path (./deployah.yaml)
deployah deploy prod

# Deploy to staging with an explicit spec path
deployah deploy staging -s ./path/to/deployah.yaml

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

	manifest, err := rt.Spec(c, opts.Environment)
	if err != nil {
		return fmt.Errorf("load spec: %w", err)
	}

	c.Logger().Debug("spec loaded", "env", opts.Environment)

	// Resolve the target Kubernetes context: the --context flag wins, otherwise
	// fall back to the selected environment's "context" field. Apply it before
	// the Helm client is built so the right cluster is targeted.
	if kubeContext := opts.Context; kubeContext != "" {
		rt.SetKubeContext(kubeContext)
	} else if envContext := environmentContext(manifest, opts.Environment); envContext != "" {
		rt.SetKubeContext(envContext)
	}

	helmClient, err := rt.Helm()
	if err != nil {
		return fmt.Errorf("helm client: %w%s", err, clusterHint(err))
	}

	// Fail fast before the spinner so a bad context surfaces as a clean error
	// rather than a panic (helm/helm#32183 is triggered by a second
	// IsReachable call inside InstallApp on an already-poisoned client).
	if reachErr := helmClient.IsReachable(); reachErr != nil {
		return fmt.Errorf("%w%s", reachErr, clusterHint(reachErr))
	}

	title := fmt.Sprintf("Deploying to '%s'...", opts.Environment)
	if opts.DryRun {
		title = fmt.Sprintf("Dry run for '%s'...", opts.Environment)
	}

	err = c.Spinner(title, func(_ *nabat.Spinner) error {
		return helmClient.InstallApp(c, manifest, opts.Environment, opts.DryRun)
	})
	if err != nil {
		if opts.DryRun {
			return fmt.Errorf("dry run failed: %w", err)
		}
		return fmt.Errorf("deploy failed: %w%s", err, clusterHint(err))
	}

	if opts.DryRun {
		c.Success("Dry run completed", "project", manifest.Project, "environment", opts.Environment)
	} else {
		c.Success("Deployed", "project", manifest.Project, "environment", opts.Environment)
	}

	return nil
}

// environmentContext returns the "context" field of the named environment in
// the spec, or an empty string when the environment is not found or has no
// context set.
func environmentContext(m *spec.Spec, name string) string {
	if m == nil {
		return ""
	}
	for _, env := range m.Environments {
		if env.Name == name {
			return env.Context
		}
	}
	return ""
}

// clusterHint returns an actionable suffix for errors that look like the target
// cluster or context is missing or unreachable, pointing users at the local
// cluster workflow. It returns an empty string for unrelated errors.
func clusterHint(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "context") && (strings.Contains(msg, "does not exist") || strings.Contains(msg, "not found")),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "dial tcp"),
		strings.Contains(msg, "no configuration has been provided"),
		strings.Contains(msg, "couldn't get current server api group list"):
		return "\n\nHint: the target cluster/context may be unavailable. For a local cluster, run 'deployah cluster up' (and pass --context kind-deployah or set the environment's 'context' field)."
	default:
		return ""
	}
}
