package deploy

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/k8s"
	"deployah.dev/deployah/internal/session"
	"deployah.dev/deployah/internal/spec"
)

// Options holds command-line flags for deploy.
type Options struct {
	Environment string `nabat:"environment"`
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

	sess := session.FromContext(c)

	cluster, err := sess.Target(c, opts.Environment)
	if err != nil {
		return fmt.Errorf("target cluster: %w", err)
	}

	manifest, err := sess.Spec(c, opts.Environment)
	if err != nil {
		return fmt.Errorf("load spec: %w", err)
	}

	c.Logger().Debug("spec loaded", "env", opts.Environment)

	helmClient, err := cluster.Helm()
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

	var watcher *DeployWatcher

	if !opts.DryRun {
		k8sClient, k8sErr := cluster.Kubernetes()
		if k8sErr != nil {
			// K8s client is best-effort: skip both the pre-flight check and the
			// event watcher rather than failing the whole deploy.
			c.Logger().Debug("skipping k8s checks: client unavailable", "err", k8sErr)
		} else {
			if reqs := requiredAPIs(manifest, opts.Environment); len(reqs) > 0 {
				if capErr := k8s.CheckAPIRequirements(k8sClient, reqs); capErr != nil {
					return capErr
				}
			}
			releaseName := helm.GenerateReleaseName(manifest.Project, opts.Environment)
			watcher = NewDeployWatcher(k8sClient, cluster.Namespace(), releaseName)
		}
	}

	err = c.Status(func(st *nabat.Status) error {
		var wg sync.WaitGroup
		var cancel context.CancelFunc
		if watcher != nil {
			var watchCtx context.Context
			watchCtx, cancel = context.WithCancel(c)
			wg.Go(func() {
				watcher.Run(watchCtx, st)
			})
		}
		helmErr := helmClient.InstallApp(c, manifest, opts.Environment, opts.DryRun)
		if cancel != nil {
			cancel()
		}
		wg.Wait()
		return helmErr
	}, nabat.WithTitle(title))
	if err != nil {
		if watcher != nil {
			for _, w := range watcher.Warnings() {
				c.Warn(fmt.Sprintf("[%s] %s: %s", w.Object, w.Reason, w.Message))
			}
		}
		if opts.DryRun {
			return fmt.Errorf("dry run failed: %w", err)
		}
		return fmt.Errorf("deploy failed: %w%s", err, clusterHint(err))
	}

	if opts.DryRun {
		c.Success("Dry run completed", "project", manifest.Project, "environment", opts.Environment)
	} else {
		summary := buildSummaryMsg(watcher)
		c.Success("Deployed"+summary, "project", manifest.Project, "environment", opts.Environment)
	}

	return nil
}

// buildSummaryMsg formats a component readiness summary from the watcher.
// It returns an empty string when no watcher or no summary data is available.
func buildSummaryMsg(w *DeployWatcher) string {
	if w == nil {
		return ""
	}
	statuses := w.Summary()
	if len(statuses) == 0 {
		return ""
	}
	var parts []string
	for _, s := range statuses {
		parts = append(parts, fmt.Sprintf("%s: %d/%d", s.Name, s.ReadyPods, s.TotalPods))
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// requiredAPIs derives the Kubernetes API group/version requirements for the
// components that will be deployed in the target environment. Only components
// whose Environments list includes the target (or have no restriction) are
// considered. Multiple components needing the same API group are deduplicated.
//
// The API version choices mirror what the embedded Helm chart templates select:
//   - autoscaling.enabled → autoscaling/v2 or autoscaling/v2beta2
//     (hpa.yaml: common.capabilities.hpa.apiVersion)
//   - ingress → networking.k8s.io/v1
//     (ingress.yaml: common.capabilities.ingress.apiVersion)
func requiredAPIs(manifest *spec.Spec, environment string) []k8s.APIRequirement {
	type entry struct {
		groupVersions []string
		components    []string
	}

	entries := make(map[string]*entry) // keyed by canonical group/version string

	add := func(groupVersions []string, componentName string) {
		key := strings.Join(groupVersions, "|")
		e := entries[key]
		if e == nil {
			e = &entry{groupVersions: groupVersions}
			entries[key] = e
		}
		e.components = append(e.components, fmt.Sprintf("%q", componentName))
	}

	for name, component := range manifest.Components {
		if len(component.Environments) > 0 && !slices.Contains(component.Environments, environment) {
			continue
		}
		if component.Autoscaling != nil && component.Autoscaling.Enabled {
			add([]string{"autoscaling/v2", "autoscaling/v2beta2"}, name)
		}
		if component.Ingress != nil {
			add([]string{"networking.k8s.io/v1"}, name)
		}
	}

	reqs := make([]k8s.APIRequirement, 0, len(entries))
	for _, e := range entries {
		noun := "component"
		if len(e.components) > 1 {
			noun = "components"
		}
		reqs = append(reqs, k8s.APIRequirement{
			GroupVersions: e.groupVersions,
			Reason:        fmt.Sprintf("required by %s %s", noun, strings.Join(e.components, ", ")),
		})
	}
	return reqs
}

// clusterHint returns an actionable suffix for errors that look like the target
// cluster or context is missing or unreachable. It returns an empty string for
// unrelated errors.
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
