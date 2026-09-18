package deploy

import (
	"context"
	"errors"
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
	"deployah.dev/deployah/internal/render"
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
	Yes                 bool   `nabat:"yes"`
	Reapply             bool   `nabat:"reapply"`
	SkipCRDs            bool   `nabat:"skip-crds"`
}

// Register adds the deploy command to app.
func Register(app *nabat.App) {
	app.MustCommand("deploy",
		nabat.WithDescription("Deploy a project to a Kubernetes cluster on a given environment"),
		nabat.WithLongDescription("Deploy a project to a Kubernetes cluster on a given environment. Shows what would change and asks for confirmation before applying, unless --yes is set."),
		nabat.WithArg("environment", "", nabat.WithRequired(), nabat.WithUsage("Environment to deploy to"), nabat.WithPrompt("Environment", "", nabat.WithHint("e.g. prod, staging"))),
		nabat.WithFlag("explain", false, nabat.WithUsage("Print the resolution report before cluster checks (visible even when cluster is unreachable)")),
		nabat.WithFlag("force-hostname-change", false, nabat.WithUsage("Allow changing the resolved hostname even though it may break existing traffic (skips the hostname guard)")),
		nabat.WithFlag("resize-volumes", false, nabat.WithUsage("Allow persistence.size increases by expanding PVCs; StatefulSet controllers are orphan-deleted when needed so volumeClaimTemplates can be rewritten")),
		nabat.WithFlag("yes", false, nabat.WithShort('y'), nabat.WithUsage("Apply without an interactive confirmation prompt")),
		nabat.WithFlag("reapply", false, nabat.WithUsage("Upgrade the release even when the plan shows no changes")),
		nabat.WithFlag("skip-crds", false, nabat.WithUsage("Skip installing CustomResourceDefinitions from the chart on a fresh Helm install")),
		nabat.WithExample(`
# Deploy to production using the default spec path (./deployah.yaml)
deployah deploy prod

# Deploy to staging with an explicit spec path
deployah deploy staging -s ./path/to/deployah.yaml

# Deploy without an interactive confirmation prompt (e.g. in CI)
deployah deploy prod --yes

# Skip chart CRDs on a first install
deployah deploy prod --skip-crds

# Show resolution report before deploying
deployah deploy prod --explain

# Preview what a deploy would change, without touching the cluster
deployah plan prod --offline`),
		nabat.WithRun(runDeploy),
	)
}

// deployPlan bundles the shown diff with the render that produced it, so
// callers reuse one render instead of recomputing. cleanup releases the
// chart temp dir behind result.ChartPath; runDeploy defers it.
type deployPlan struct {
	diff    *planengine.Plan
	result  *render.RenderResult
	cleanup func()
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

	// Materialize self-signed TLS certs once, before any render, so the plan
	// render and the real apply see identical bytes (see applyDeploy).
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

	plan, err := computePlan(c, helmClient, cluster, resolvedSpec, postRenderer, bundle.CRDs)
	if err != nil {
		return err
	}
	defer plan.cleanup()

	if n := len(bundle.CRDs); n > 0 {
		c.Println(extras.CRDLifecycleNote(n, plan.result.IsUpgrade, opts.SkipCRDs))
	}

	textOpts := planengine.TextOptions{Mode: planengine.ModeCompact, Theme: c.Theme()}
	if renderErr := planengine.RenderText(c.IO().Out, plan.diff, textOpts); renderErr != nil {
		return fmt.Errorf("render plan: %w", renderErr)
	}

	// Hostname guard: block FQDN changes unless --force-hostname-change.
	// Runs after the plan diff is shown, so a block is never a surprise.
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

	helmIdle := skipHelmApply(plan.result.IsUpgrade, plan.diff.HasChanges(), opts.Reapply)
	if helmIdle {
		return skipDeploy(c, k8sClient, k8sErr, plan)
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

	proceed, confirmErr := confirmApply(c, opts, "Apply these changes?")
	if confirmErr != nil {
		return confirmErr
	}
	if !proceed {
		c.Println("Aborted.")
		return nil
	}

	return applyDeploy(c, sess, cluster, helmClient, platform, manifest, opts, resolvedSpec, plan, k8sClient, k8sErr, bundle, postRenderer, resizes)
}

// skipHelmApply reports whether deploy should exit without invoking Helm:
// an existing release whose rendered manifests are unchanged, unless
// --reapply is set. Fresh installs are never skipped, even when the
// ordinary Manifest is empty. Chart CRD files and --skip-crds do not
// affect this gate.
func skipHelmApply(isUpgrade, hasChanges, reapply bool) bool {
	return isUpgrade && !hasChanges && !reapply
}

// confirmApply gates the real apply behind --yes or an interactive prompt.
// proceed is false with a nil error on a clean "no"; err is non-nil when
// non-interactive without --yes ([nabat.ErrConfirmationRequired]), or the
// prompt fails. prompt must be non-empty.
func confirmApply(c *nabat.Context, opts *Options, prompt string) (proceed bool, err error) {
	confirmed, confirmErr := c.Confirm(prompt,
		nabat.WithYes(opts.Yes),
		nabat.WithBypassHint("--yes"),
	)
	if confirmErr != nil {
		return false, confirmErr
	}
	return confirmed, nil
}

// computePlan renders the chart client-side and diffs it against the last
// successful release. It never mutates the cluster or Helm's release history.
// The caller must invoke deployPlan.cleanup when finished with the result.
func computePlan(c *nabat.Context, helmClient session.HelmClient, cluster *session.Cluster, resolved *spec.ResolvedSpec, postRenderer postrenderer.PostRenderer, crds []extras.RawFile) (*deployPlan, error) {
	p, result, cleanup, err := planengine.BuildPlan(c, helmClient, cluster.Context(), resolved, postRenderer, crds)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("%w%s", err, cmdopts.ClusterHint(err))
	}
	return &deployPlan{diff: p, result: result, cleanup: cleanup}, nil
}

// skipDeploy handles an unchanged upgrade: Helm is never invoked, but pod
// readiness for the release is still shown for parity with a real deploy.
func skipDeploy(c *nabat.Context, k8sClient kubernetes.Interface, k8sErr error, plan *deployPlan) error {
	c.Success(fmt.Sprintf("No changes. Release %s unchanged (revision %d).", plan.diff.Header.Release, plan.diff.Header.Revision))
	return printReadiness(c, k8sClient, k8sErr, plan)
}

func printReadiness(c *nabat.Context, k8sClient kubernetes.Interface, k8sErr error, plan *deployPlan) error {
	if k8sErr != nil {
		c.Logger().Debug("skipping readiness summary: k8s client unavailable", "err", k8sErr)
		return nil
	}
	statuses, pollErr := readiness.Poll(c, k8sClient, plan.result.Namespace, plan.result.ReleaseName)
	if pollErr != nil {
		c.Logger().Debug("skipping readiness summary: poll failed", "err", pollErr)
		return nil
	}
	if summary := readiness.Summary(statuses); summary != "" {
		c.Println("Readiness: " + summary)
	}
	return nil
}

// applyDeploy re-renders and verifies determinism before the real Helm
// install/upgrade. When resizes is non-empty, PVC expansion (and
// StatefulSet orphan-delete when needed) run before Helm.
func applyDeploy(c *nabat.Context, sess *session.Session, cluster *session.Cluster, helmClient session.HelmClient, platform *spec.PlatformConfig, manifest *spec.Spec, opts *Options, resolved *spec.ResolvedSpec, plan *deployPlan, k8sClient kubernetes.Interface, k8sErr error, bundle *extras.Bundle, postRenderer postrenderer.PostRenderer, resizes []persistenceResize) error {
	verify, verifyCleanup, err := helmClient.RenderManifests(c, resolved, postRenderer, bundle.CRDs)
	if verifyCleanup != nil {
		defer verifyCleanup()
	}
	if err != nil {
		return fmt.Errorf("render manifests: %w%s", err, cmdopts.ClusterHint(err))
	}
	// A mismatch means the chart is non-deterministic (e.g. embeds a
	// timestamp), so what was shown isn't what would actually be installed.
	if verify.Manifest != plan.result.Manifest {
		return errors.New("rendered manifests changed between plan and apply; re-run 'deployah deploy' to see the current plan")
	}

	if len(resizes) > 0 {
		if k8sErr != nil {
			return fmt.Errorf("resize volumes: kubernetes client unavailable: %w", k8sErr)
		}
		c.Printf("Resizing volumes for %d component(s)...\n", len(resizes))
		if resizeErr := resizeVolumes(c, k8sClient, cluster.Namespace(), plan.result.ReleaseName, resizes); resizeErr != nil {
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
		watcher = NewDeployWatcher(k8sClient, cluster.Namespace(), plan.result.ReleaseName)
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
