package deploy

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	"helm.sh/helm/v4/pkg/postrenderer"
	"k8s.io/client-go/kubernetes"
	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/cmd/cmdopts"
	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/k8s"
	"deployah.dev/deployah/internal/readiness"
	"deployah.dev/deployah/internal/session"
	"deployah.dev/deployah/internal/spec"

	planengine "deployah.dev/deployah/internal/plan"
)

// Options holds command-line flags for deploy.
type Options struct {
	Environment         string `nabat:"environment"`
	Explain             bool   `nabat:"explain"`
	ForceHostnameChange bool   `nabat:"force-hostname-change"`
	ResizeVolumes       bool   `nabat:"resize-volumes"`
	SkipCRDs            bool   `nabat:"skip-crds"`
}

// Register adds the deploy command to app.
func Register(app *nabat.App) {
	app.MustCommand("deploy",
		nabat.WithDescription("Deploy a project to a Kubernetes cluster on a given environment"),
		nabat.WithLongDescription("Deploy a project to a Kubernetes cluster for an environment. Deployah validates the spec and runs its deploy guards, then runs Helm: an install for a new release, an upgrade for an existing one. An existing release is always upgraded, even when nothing changed, so Helm creates a new revision and runs upgrade hooks. To inspect changes first, run `deployah plan <environment>`."),
		nabat.WithArg("environment", "", nabat.WithRequired(), nabat.WithUsage("Environment to deploy to"), nabat.WithPrompt("Environment", "", nabat.WithHint("e.g. prod, staging"))),
		nabat.WithFlag("explain", false, nabat.WithUsage("Print the resolution report before cluster checks (visible even when cluster is unreachable)")),
		nabat.WithFlag("force-hostname-change", false, nabat.WithUsage("Allow changing the resolved hostname even though it may break existing traffic (skips the hostname guard)")),
		nabat.WithFlag("resize-volumes", false, nabat.WithUsage("Allow persistence.size increases by expanding PVCs; StatefulSet controllers are orphan-deleted when needed so volumeClaimTemplates can be rewritten")),
		nabat.WithFlag("skip-crds", false, nabat.WithUsage("Skip installing CustomResourceDefinitions from the chart on a fresh Helm install")),
		nabat.WithExample(`
# Deploy to production using the default spec path (./deployah.yaml)
deployah deploy prod

# Deploy to staging with an explicit spec path
deployah deploy staging -s ./path/to/deployah.yaml

# Skip chart CRDs on a first install
deployah deploy prod --skip-crds

# Show resolution report before deploying
deployah deploy prod --explain

# Inspect changes, then deploy
deployah plan prod
deployah deploy prod`),
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
	ws := sess.Workspace()

	// The platform file owns the environment registry --environment is
	// validated against, so it loads before the spec. The same pointer
	// is passed to Load and Resolve so both see one platform state.
	platform, platformErr := ws.Platform()
	if platformErr != nil {
		return fmt.Errorf("load platform file: %w", platformErr)
	}

	manifest, substReport, err := spec.Load(c, ws.SpecPath(), opts.Environment, platform)
	if err != nil {
		return fmt.Errorf("load spec: %w", err)
	}

	c.Logger().Debug("spec loaded", "env", opts.Environment)

	// Fail closed when any component uses expose and platform is absent.
	if platform == nil && cmdopts.HasExposeComponents(manifest) {
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
	resolvedSpec, report, err := spec.Resolve(manifest, platform, envIdentity, substReport)
	if err != nil {
		if report != nil && report.ErrorCode != "" {
			return fmt.Errorf("resolution failed (%s): %w", report.ErrorCode, err)
		}
		return fmt.Errorf("resolution failed: %w", err)
	}

	if opts.Explain && resolvedSpec != nil {
		printExplain(c, resolvedSpec)
	}

	effective, effErr := spec.EffectiveTasks(manifest, opts.Environment, resolvedSpec)
	if effErr != nil {
		return effErr
	}
	tasks := make(map[string]spec.Task, len(effective))
	for name, rt := range effective {
		tasks[name] = rt.Task
	}
	if timeoutErr := spec.CheckHookTaskTimeouts(tasks, sess.Timeout()); timeoutErr != nil {
		return timeoutErr
	}

	cluster, err := sess.Target(c, opts.Environment)
	if err != nil {
		return fmt.Errorf("target cluster: %w", err)
	}

	helmClient, err := cluster.Helm()
	if err != nil {
		return fmt.Errorf("helm client: %w%s", err, cmdopts.ClusterHint(err))
	}

	// Fail fast before the spinner so a bad context surfaces as a clean error
	// rather than a panic (helm/helm#32183 is triggered by a second
	// IsReachable call inside InstallApp on an already-poisoned client).
	if reachErr := helmClient.IsReachable(); reachErr != nil {
		return fmt.Errorf("%w%s", reachErr, cmdopts.ClusterHint(reachErr))
	}

	cmdopts.WarnContextFallback(c, cluster, opts.Environment)

	// Fetch the Kubernetes clientset once and thread it through, so a
	// transient failure produces one consistent outcome for this invocation.
	k8sClient, k8sErr := cluster.Kubernetes()
	if k8sErr != nil {
		c.Logger().Debug("kubernetes client unavailable", "err", k8sErr)
	}

	// Materialize self-signed TLS certs once, before Helm, so the resize
	// preflight and the real apply see identical bytes.
	if resolvedSpec != nil {
		if tlsErr := cmdopts.MaterializeSelfSignedTLS(c, k8sClient, k8sErr, cluster.Namespace(), resolvedSpec); tlsErr != nil {
			return fmt.Errorf("materialize self-signed TLS: %w", tlsErr)
		}
	}

	restCfg, restErr := cluster.RESTConfig()
	if restErr != nil {
		c.Logger().Debug("rest config unavailable for extras scope discovery", "err", restErr)
	}
	bundle, err := extras.LoadFromSpec(ws.SpecPath(), manifest, platform, opts.Environment, cluster.Namespace(), restCfg)
	if err != nil {
		return fmt.Errorf("load extras: %w", err)
	}
	postRenderer := bundle.PostRendererFor()

	// Hostname guard: block FQDN changes unless --force-hostname-change.
	if resolvedSpec != nil {
		if guardErr := checkHostnameGuard(c, helmClient, manifest.Project, opts.Environment, resolvedSpec, opts.ForceHostnameChange); guardErr != nil {
			return guardErr
		}
	}

	prevResolved, prevErr := loadPreviousResolvedComponents(c, helmClient, manifest.Project, opts.Environment)
	if prevErr != nil {
		return prevErr
	}
	if guardErr := checkWorkloadGuards(manifest, opts.Environment, prevResolved); guardErr != nil {
		return guardErr
	}
	emitWorkloadWarnings(c, manifest, opts.Environment, prevResolved)

	resizes := detectPersistenceResizes(manifest, opts.Environment, resolvedSpec, prevResolved)
	if resizeFlagErr := requireResizeFlag(resizes, opts.ResizeVolumes); resizeFlagErr != nil {
		return resizeFlagErr
	}

	if k8sErr == nil {
		if hasStatefulWithPersistence(manifest, opts.Environment) {
			if verErr := k8s.CheckMinimumVersion(
				k8sClient,
				k8s.MinStatefulMajor,
				k8s.MinStatefulMinor,
				"kind: stateful with persistence requires Kubernetes 1.32+",
			); verErr != nil {
				return verErr
			}
		}
	}

	return applyDeploy(c, sess, cluster, helmClient, platform, manifest, opts, resolvedSpec, envIdentity.ReleaseName(manifest.Project), k8sClient, k8sErr, bundle, postRenderer, resizes)
}

// helmPreflight renders the chart on the client before a volume resize
// and does not keep the result. If rendering fails, the resize does not
// start, so PVCs stay as they are and StatefulSets are not deleted.
// Call it only when a resize is pending.
func helmPreflight(c *nabat.Context, helmClient session.HelmClient, resolved *spec.ResolvedSpec, postRenderer postrenderer.PostRenderer, crds []extras.RawFile) error {
	_, cleanup, err := helmClient.RenderManifests(c, resolved, postRenderer, crds)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return fmt.Errorf("helm preflight before volume resize: %w%s", err, cmdopts.ClusterHint(err))
	}
	return nil
}

// applyDeploy runs Helm install or upgrade. When resizes is non-empty, a
// client-side render preflight runs first, then PVC expansion (and
// StatefulSet orphan-delete when needed) run before Helm. releaseName is
// the Helm release for this project and environment.
func applyDeploy(c *nabat.Context, sess *session.Session, cluster *session.Cluster, helmClient session.HelmClient, platform *spec.PlatformConfig, manifest *spec.Spec, opts *Options, resolved *spec.ResolvedSpec, releaseName string, k8sClient kubernetes.Interface, k8sErr error, bundle *extras.Bundle, postRenderer postrenderer.PostRenderer, resizes []persistenceResize) error {
	if len(resizes) > 0 {
		if k8sErr != nil {
			return fmt.Errorf("resize volumes: kubernetes client unavailable: %w", k8sErr)
		}
		if preflightErr := helmPreflight(c, helmClient, resolved, postRenderer, bundle.CRDs); preflightErr != nil {
			return preflightErr
		}
		c.Printf("Resizing volumes for %d component(s)...\n", len(resizes))
		if resizeErr := resizeVolumes(c, k8sClient, cluster.Namespace(), releaseName, resizes); resizeErr != nil {
			return fmt.Errorf("%s: %w", resizeFailureHint(resizes), resizeErr)
		}
	}

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

	// k8sClient/k8sErr come from runDeploy's single cluster.Kubernetes()
	// call.
	var watcher *DeployWatcher
	if k8sErr != nil {
		// K8s client is best-effort: skip the event watcher rather than
		// failing the whole deploy.
		c.Logger().Debug("skipping deploy watcher: k8s client unavailable", "err", k8sErr)
	} else {
		watcher = NewDeployWatcher(k8sClient, cluster.Namespace(), releaseName)
	}

	err := c.Status(func(st *nabat.Status) error {
		var wg sync.WaitGroup
		var cancel context.CancelFunc
		if watcher != nil {
			var watchCtx context.Context
			watchCtx, cancel = context.WithCancel(c)
			wg.Go(func() {
				watcher.Run(watchCtx, st)
			})
		}
		helmErr := helmClient.InstallApp(c, false, resolved, postRenderer, bundle.CRDs, opts.SkipCRDs)
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
		return fmt.Errorf("deploy failed: %w%s", err, cmdopts.ClusterHint(err))
	}

	summary := buildSummaryMsg(watcher)
	c.Success("Deployed"+summary, "project", manifest.Project, "environment", opts.Environment)
	return nil
}

// checkHostnameGuard blocks FQDN changes relative to the last successful
// release, unless force is set. Skips when there is no prior successful
// release (first install or every revision failed). History fetch errors
// fail closed so a transient API failure cannot bypass the guard.
func checkHostnameGuard(c *nabat.Context, helmClient session.HelmClient, project, environment string, resolved *spec.ResolvedSpec, force bool) error {
	rel, _, err := planengine.LastSuccessfulRelease(c, helmClient, project, environment)
	if err != nil {
		return fmt.Errorf("hostname guard: %w", err)
	}
	if rel == nil {
		// No prior successful release: nothing to compare against.
		// Uses the last SUCCESSFUL release, not simply the latest, since a
		// FAILED revision may already record the new FQDN.
		return nil
	}

	// Chart Values are what's actually installed; Release.Config is never
	// populated, since deployah's InstallApp always passes an empty values
	// override map.
	var chartValues map[string]any
	if rel.Chart != nil {
		chartValues = rel.Chart.Values
	}

	changed := hostnameChanges(chartValues, resolved)
	if len(changed) == 0 {
		return nil
	}

	if force {
		// Downgrade the block to a per-change warning instead of applying
		// silently, so a bypassed guard is never silent.
		for _, line := range changed {
			c.Warn(fmt.Sprintf("hostname change (may drop live traffic), continuing because --force-hostname-change is set:%s", line))
		}
		return nil
	}

	return fmt.Errorf(
		"hostname change detected for %s/%s (pass --force-hostname-change to override):\n%s",
		project, environment, strings.Join(changed, "\n"),
	)
}

// hostnameChanges returns one "  component: old -> new" line per component
// whose FQDN changed in the deployah.resolved values block.
func hostnameChanges(config map[string]any, resolved *spec.ResolvedSpec) []string {
	deployahBlock, ok := config["deployah"]
	if !ok {
		return nil
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
	// resolved.Components is a map, so iteration order is randomized; sort
	// for stable, comparable output across runs.
	slices.Sort(changed)
	return changed
}

// buildSummaryMsg formats a component readiness summary from the watcher.
// It returns an empty string when no watcher or no summary data is available.
func buildSummaryMsg(w *DeployWatcher) string {
	if w == nil {
		return ""
	}
	summary := readiness.Summary(w.Summary())
	if summary == "" {
		return ""
	}
	return " (" + summary + ")"
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
