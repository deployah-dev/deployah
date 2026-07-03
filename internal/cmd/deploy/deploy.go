package deploy

import (
	"context"
	"fmt"
	"os"
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
	Environment         string `nabat:"environment"`
	DryRun              bool   `nabat:"dry-run"`
	Explain             bool   `nabat:"explain"`
	ForceHostnameChange bool   `nabat:"force-hostname-change"`
}

// Register adds the deploy command to app.
func Register(app *nabat.App) {
	app.MustCommand("deploy",
		nabat.WithDescription("Deploy a project to a Kubernetes cluster on a given environment"),
		nabat.WithLongDescription("Deploy a project to a Kubernetes cluster on a given environment."),
		nabat.WithArg("environment", "", nabat.WithRequired(), nabat.WithUsage("Environment to deploy to"), nabat.WithPrompt("Environment", "", nabat.WithHint("e.g. prod, staging"))),
		nabat.WithFlag("dry-run", false, nabat.WithUsage("Perform a dry run (render templates without installing)")),
		nabat.WithFlag("explain", false, nabat.WithUsage("Print the resolution report before cluster checks (visible even when cluster is unreachable)")),
		nabat.WithFlag("force-hostname-change", false, nabat.WithUsage("Allow changing the resolved hostname even though it may break existing traffic (skips the hostname guard)")),
		nabat.WithExample(`
# Deploy to production using the default spec path (./deployah.yaml)
deployah deploy prod

# Deploy to staging with an explicit spec path
deployah deploy staging -s ./path/to/deployah.yaml

# Deploy to production with a dry run
deployah deploy prod --dry-run

# Show resolution report before deploying
deployah deploy prod --explain`),
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

	// Prescan the raw (pre-envsubst) manifest for ${VAR} tokens so the
	// resolver can distinguish static from dynamic subdomains.
	rawSpec, _, rawErr := spec.ParseManifest(sess.SpecPath())
	if rawErr != nil {
		return fmt.Errorf("parse manifest: %w", rawErr)
	}
	substReport := spec.PrescanSubstitutionReport(rawSpec)

	// Load the fully substituted manifest (envsubst applied).
	manifest, err := spec.Load(c, sess.SpecPath(), opts.Environment)
	if err != nil {
		return fmt.Errorf("load spec: %w", err)
	}

	c.Logger().Debug("spec loaded", "env", opts.Environment)

	// Load platform file.
	platform, platformErr := sess.Platform()
	if platformErr != nil {
		return fmt.Errorf("load platform file: %w", platformErr)
	}

	// Fail closed when any component uses expose and platform is absent.
	if platform == nil && hasExposeComponents(manifest) {
		return fmt.Errorf(
			"one or more components use expose blocks but no platform file was found; "+
				"create %s or set DEPLOYAH_PLATFORM_FILE, or pass --platform-file",
			spec.DefaultPlatformPath,
		)
	}

	// Warn when --context overrides the platform file's context for the env.
	if platform != nil {
		warnContextMismatch(c, sess.KubeContext(), platform, opts.Environment)
	}

	// Resolve using the substituted manifest (so expanded subdomains pass DNS
	// validation) with the raw prescan report (so dynamic fields are skipped).
	envIdentity := spec.NormalizeEnv(opts.Environment)
	var resolvedSpec *spec.ResolvedSpec
	if platform != nil {
		var report *spec.ResolutionReport
		resolvedSpec, report, err = spec.Resolve(manifest, platform, envIdentity, substReport)
		if err != nil {
			if report != nil && report.ErrorCode != "" {
				return fmt.Errorf("resolution failed (%s): %w", report.ErrorCode, err)
			}
			return fmt.Errorf("resolution failed: %w", err)
		}
	}

	if opts.Explain && resolvedSpec != nil {
		printExplain(c, resolvedSpec)
	}

	cluster, err := sess.Target(c, opts.Environment)
	if err != nil {
		return fmt.Errorf("target cluster: %w", err)
	}

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

	// Build deploy title with the resolved Kubernetes context so the user knows
	// which cluster is being targeted. Append [override] when --context was
	// passed explicitly and differs from the platform-resolved context.
	resolvedCtx := cluster.Context()
	ctxSuffix := ""
	if resolvedCtx != "" {
		ctxSuffix = " (context: " + resolvedCtx
		if platform != nil && sess.KubeContext() != "" {
			platformCtx := spec.PlatformEnvContext(platform, opts.Environment)
			if platformCtx != "" && platformCtx != resolvedCtx {
				ctxSuffix += " [override]"
			}
		}
		ctxSuffix += ")"
	}
	title := fmt.Sprintf("Deploying to '%s'%s...", opts.Environment, ctxSuffix)
	if opts.DryRun {
		title = fmt.Sprintf("Dry run for '%s'%s...", opts.Environment, ctxSuffix)
	}

	var watcher *DeployWatcher

	if !opts.DryRun {
		k8sClient, k8sErr := cluster.Kubernetes()
		if k8sErr != nil {
			// K8s client is best-effort: skip both the pre-flight check and the
			// event watcher rather than failing the whole deploy.
			c.Logger().Debug("skipping k8s checks: client unavailable", "err", k8sErr)
		} else {
			if reqs := requiredAPIs(manifest, opts.Environment, resolvedSpec); len(reqs) > 0 {
				if capErr := k8s.CheckAPIRequirements(k8sClient, reqs); capErr != nil {
					return capErr
				}
			}
			releaseName := helm.GenerateReleaseName(manifest.Project, opts.Environment)
			watcher = NewDeployWatcher(k8sClient, cluster.Namespace(), releaseName)

			// Hostname guard: block FQDN changes unless --force-hostname-change.
			if !opts.ForceHostnameChange && resolvedSpec != nil {
				if guardErr := checkHostnameGuard(c, helmClient, manifest.Project, opts.Environment, resolvedSpec); guardErr != nil {
					return guardErr
				}
			}
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
		helmErr := helmClient.InstallApp(c, manifest, opts.Environment, opts.DryRun, resolvedSpec)
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

// checkHostnameGuard compares the resolved FQDNs against the prior release's
// deployah.resolved block and returns an error if any FQDN changed. It skips
// on first install (no prior release) and components without expose.
func checkHostnameGuard(c *nabat.Context, helmClient session.HelmClient, project, environment string, resolved *spec.ResolvedSpec) error {
	ctx := context.Background()
	rel, err := helmClient.GetRelease(ctx, project, environment)
	if err != nil {
		// No prior release (first install) or unrelated error: skip guard.
		return nil
	}
	if rel == nil {
		return nil
	}

	// Navigate: rel.Config["deployah"]["resolved"]["components"][compName]["fqdn"]
	deployahBlock, ok := rel.Config["deployah"]
	if !ok {
		// Old release without deployah.resolved block: try ingress.hostname fallback.
		return checkHostnameGuardLegacy(c, rel.Config, resolved, project, environment)
	}
	deployahMap, ok := deployahBlock.(map[string]any)
	if !ok {
		return nil
	}
	resolvedBlock, ok := deployahMap["resolved"].(map[string]any)
	if !ok {
		return nil
	}
	componentsBlock, ok := resolvedBlock["components"].(map[string]any)
	if !ok {
		return nil
	}

	var changed []string
	for compName, rc := range resolved.Components {
		if rc.FQDN == "" {
			continue
		}
		prevCompAny, exists := componentsBlock[compName]
		if !exists {
			continue
		}
		prevComp, prevOK := prevCompAny.(map[string]any)
		if !prevOK {
			continue
		}
		prevFQDN, fqdnOK := prevComp["fqdn"].(string)
		if !fqdnOK {
			continue
		}
		if prevFQDN != "" && prevFQDN != rc.FQDN {
			changed = append(changed, fmt.Sprintf("  %s: %s -> %s", compName, prevFQDN, rc.FQDN))
		}
	}

	if len(changed) > 0 {
		return fmt.Errorf(
			"hostname change detected for %s/%s (pass --force-hostname-change to override):\n%s",
			project, environment, strings.Join(changed, "\n"),
		)
	}
	return nil
}

// checkHostnameGuardLegacy checks for hostname changes in old v1-alpha.1 releases
// that stored hostname in the component ingress.hostname values path.
func checkHostnameGuardLegacy(_ *nabat.Context, config map[string]any, resolved *spec.ResolvedSpec, project, environment string) error {
	var changed []string
	for compName, rc := range resolved.Components {
		if rc.FQDN == "" {
			continue
		}
		compAny, exists := config[compName]
		if !exists {
			continue
		}
		compMap, ok := compAny.(map[string]any)
		if !ok {
			continue
		}
		ingressAny, exists := compMap["ingress"]
		if !exists {
			continue
		}
		ingressMap, ok := ingressAny.(map[string]any)
		if !ok {
			continue
		}
		prevHostname, hostnameOK := ingressMap["hostname"].(string)
		if !hostnameOK {
			continue
		}
		if prevHostname != "" && prevHostname != rc.FQDN {
			changed = append(changed, fmt.Sprintf("  %s: %s -> %s", compName, prevHostname, rc.FQDN))
		}
	}

	if len(changed) > 0 {
		return fmt.Errorf(
			"hostname change detected for %s/%s (pass --force-hostname-change to override):\n%s",
			project, environment, strings.Join(changed, "\n"),
		)
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
// The resolved spec is used to detect cert-manager requirements (TLS mode).
func requiredAPIs(manifest *spec.Spec, environment string, resolved *spec.ResolvedSpec) []k8s.APIRequirement {
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
		if component.Expose != nil {
			add([]string{"networking.k8s.io/v1"}, name)
		}
		if resolved != nil {
			if rc, ok := resolved.Components[name]; ok && rc.TLSMode == spec.TLSModeCertManager {
				add([]string{"cert-manager.io/v1"}, name)
			}
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

// warnContextMismatch emits a warning when the --context flag overrides the
// platform-file context for the target environment. Silenced by setting
// DEPLOYAH_ALLOW_CONTEXT_MISMATCH=1.
func warnContextMismatch(c *nabat.Context, kubeCtxOverride string, platform *spec.PlatformConfig, envName string) {
	if kubeCtxOverride == "" {
		return
	}
	if os.Getenv("DEPLOYAH_ALLOW_CONTEXT_MISMATCH") == "1" {
		return
	}
	platformCtx := spec.PlatformEnvContext(platform, envName)
	if platformCtx != "" && platformCtx != kubeCtxOverride {
		c.Warn(fmt.Sprintf(
			"--context %q overrides platform context %q for environment %q; "+
				"set DEPLOYAH_ALLOW_CONTEXT_MISMATCH=1 to suppress this warning",
			kubeCtxOverride, platformCtx, envName,
		))
	}
}

// hasExposeComponents reports whether any component in the spec declares an
// expose block, meaning platform resolution is required.
func hasExposeComponents(m *spec.Spec) bool {
	if m == nil {
		return false
	}
	for _, comp := range m.Components {
		if comp.Expose != nil {
			return true
		}
	}
	return false
}

// printExplain prints the resolution report before cluster checks.
func printExplain(c *nabat.Context, resolved *spec.ResolvedSpec) {
	c.Println("--- Resolution Report ---")
	c.Println(fmt.Sprintf("Environment: %s", resolved.Env.Original))
	if resolved.KubeContext != "" {
		c.Println(fmt.Sprintf("Context:     %s", resolved.KubeContext))
	}
	for name, rc := range resolved.Components {
		if rc.FQDN == "" {
			continue
		}
		c.Println(fmt.Sprintf("  %s: hostname=%s tls=%s", name, rc.FQDN, rc.TLSMode))
	}
	for _, w := range resolved.Warnings {
		c.Warn(w)
	}
	c.Println("--- End Report ---")
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
