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
	"nabat.dev/theme"

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
	Yes                 bool   `nabat:"yes"`
	Reapply             bool   `nabat:"reapply"`
	CRDs                string `nabat:"crds"`
}

// crdPolicies are the allowed values for --crds (same order as help text).
var crdPolicies = []string{string(extras.PolicyCreate), string(extras.PolicyCreateReplace)}

// Register adds the deploy command to app.
func Register(app *nabat.App) {
	app.MustCommand("deploy",
		nabat.WithDescription("Deploy a project to a Kubernetes cluster on a given environment"),
		nabat.WithLongDescription("Deploy a project to a Kubernetes cluster on a given environment. Shows what would change and asks for confirmation before applying, unless --yes is set."),
		nabat.WithArg("environment", "", nabat.WithRequired(), nabat.WithUsage("Environment to deploy to"), nabat.WithPrompt("Environment", "", nabat.WithHint("e.g. prod, staging"))),
		nabat.WithFlag("explain", false, nabat.WithUsage("Print the resolution report before cluster checks (visible even when cluster is unreachable)")),
		nabat.WithFlag("force-hostname-change", false, nabat.WithUsage("Allow changing the resolved hostname even though it may break existing traffic (skips the hostname guard)")),
		nabat.WithFlag("yes", false, nabat.WithShort('y'), nabat.WithUsage("Apply without an interactive confirmation prompt")),
		nabat.WithFlag("reapply", false, nabat.WithUsage("Upgrade the release even when the plan shows no changes")),
		nabat.WithSelectFlag("crds", string(extras.PolicyCreate), crdPolicies, nabat.WithUsage("CRD install policy: create (install if missing) or create-replace")),
		nabat.WithExample(`
# Deploy to production using the default spec path (./deployah.yaml)
deployah deploy prod

# Deploy to staging with an explicit spec path
deployah deploy staging -s ./path/to/deployah.yaml

# Deploy without an interactive confirmation prompt (e.g. in CI)
deployah deploy prod --yes

# Replace existing CRDs from .deployah/crds/ when deploying
deployah deploy prod --crds create-replace

# Show resolution report before deploying
deployah deploy prod --explain

# Preview what a deploy would change, without touching the cluster
deployah plan prod --offline`),
		nabat.WithRun(func(c *nabat.Context) error {
			// Captured here (not read from *nabat.Context, which has no
			// public accessor for it) so RenderText can color its output;
			// see internal/plan/format_text.go's TextOptions.Theme doc.
			return runDeploy(c, app.Theme())
		}),
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

func runDeploy(c *nabat.Context, resolvedTheme theme.ResolvedTheme) error {
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

	// The platform file owns the environment registry --environment is
	// validated against, so it loads before the spec.
	platform, platformErr := sess.Platform()
	if platformErr != nil {
		return fmt.Errorf("load platform file: %w", platformErr)
	}

	// Load the fully substituted manifest (envsubst applied).
	manifest, err := spec.Load(c, sess.SpecPath(), opts.Environment, platform)
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
	bundle, err := extras.LoadFromSpec(sess.SpecPath(), manifest, platform, opts.Environment, cluster.Namespace(), restCfg)
	if err != nil {
		return fmt.Errorf("load extras: %w", err)
	}
	postRenderer := bundle.PostRendererFor()

	plan, err := computePlan(c, helmClient, cluster, manifest, opts.Environment, resolvedSpec, postRenderer)
	if err != nil {
		return err
	}
	defer plan.cleanup()

	if n := len(bundle.CRDs); n > 0 {
		c.Printf("CRDs: %d from .deployah/crds/ (policy %s)\n", n, opts.CRDs)
	}

	textOpts := planengine.TextOptions{Mode: planengine.ModeCompact, Theme: resolvedTheme}
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

	helmIdle := !plan.diff.HasChanges() && !opts.Reapply
	if skipWhenIdle(helmIdle, len(bundle.CRDs)) {
		return skipDeploy(c, k8sClient, k8sErr, plan)
	}

	// Required-API check runs before confirmation: a missing CRD/API is a
	// precondition failure the user shouldn't have to confirm past first.
	// Skip when Helm is idle and only CRDs will apply (CRDs are the APIs).
	// Also skip group/versions that .deployah/crds/ will install in this deploy.
	if !helmIdle && k8sErr == nil {
		reqs := filterCoveredAPIs(requiredAPIs(manifest, opts.Environment, resolvedSpec), extras.GroupVersionsFromCRDs(bundle.CRDs))
		if len(reqs) > 0 {
			if capErr := k8s.CheckAPIRequirements(k8sClient, reqs); capErr != nil {
				return capErr
			}
		}
	}

	prompt := "Apply these changes?"
	if helmIdle {
		n := len(bundle.CRDs)
		prompt = fmt.Sprintf("Helm release is unchanged. Apply %d %s?", n, crdNoun(n))
	}
	proceed, confirmErr := confirmApply(c, opts, prompt)
	if confirmErr != nil {
		return confirmErr
	}
	if !proceed {
		c.Println("Aborted.")
		return nil
	}

	if helmIdle {
		return applyCRDsOnly(c, sess, cluster, k8sClient, k8sErr, plan, bundle, opts)
	}
	return applyDeploy(c, sess, cluster, helmClient, platform, manifest, opts, resolvedSpec, plan, k8sClient, k8sErr, bundle, postRenderer)
}

// skipWhenIdle reports whether deploy should exit without cluster writes:
// Helm plan idle and no CRDs to apply.
func skipWhenIdle(helmIdle bool, crdCount int) bool {
	return helmIdle && crdCount == 0
}

// confirmApply gates the real apply behind --yes or an interactive prompt.
// proceed is false with a nil error on a clean "no"; err is non-nil only when
// non-interactive without --yes, or the prompt fails. prompt must be non-empty.
func confirmApply(c *nabat.Context, opts *Options, prompt string) (proceed bool, err error) {
	if opts.Yes {
		return true, nil
	}
	if !c.IsInteractive() {
		return false, errors.New("refusing to deploy without confirmation; re-run with --yes")
	}
	confirmed, confirmErr := c.Confirm(prompt)
	if confirmErr != nil {
		return false, fmt.Errorf("confirmation: %w", confirmErr)
	}
	return confirmed, nil
}

// computePlan renders the chart client-side and diffs it against the last
// successful release. It never mutates the cluster or Helm's release history.
// The caller must invoke deployPlan.cleanup when finished with the result.
func computePlan(c *nabat.Context, helmClient session.HelmClient, cluster *session.Cluster, manifest *spec.Spec, environment string, resolved *spec.ResolvedSpec, postRenderer postrenderer.PostRenderer) (*deployPlan, error) {
	diff, result, cleanup, err := planengine.BuildPlan(c, helmClient, manifest, environment, cluster.Context(), resolved, postRenderer)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("%w%s", err, cmdopts.ClusterHint(err))
	}
	return &deployPlan{diff: diff, result: result, cleanup: cleanup}, nil
}

// skipDeploy handles a plan with no changes and no CRDs: Helm is never
// invoked, but pod readiness for the release is still shown for parity with
// a real deploy.
func skipDeploy(c *nabat.Context, k8sClient kubernetes.Interface, k8sErr error, plan *deployPlan) error {
	c.Success(fmt.Sprintf("No changes. Release %s unchanged (revision %d).", plan.diff.Header.Release, plan.diff.Header.Revision))
	return printReadiness(c, k8sClient, k8sErr, plan)
}

// applyCRDsOnly applies .deployah/crds when the Helm plan is idle. CRDs are
// outside the Helm release, so a no-op chart must not skip them.
func applyCRDsOnly(c *nabat.Context, sess *session.Session, cluster *session.Cluster, k8sClient kubernetes.Interface, k8sErr error, plan *deployPlan, bundle *extras.Bundle, opts *Options) error {
	stats, err := applyBundleCRDs(c, sess, cluster, bundle, opts)
	if err != nil {
		return err
	}
	c.Success(crdIdleSuccessMessage(stats, plan.diff.Header.Release, plan.diff.Header.Revision))
	return printReadiness(c, k8sClient, k8sErr, plan)
}

func crdIdleSuccessMessage(stats extras.CRDStats, release string, revision int) string {
	suffix := fmt.Sprintf("Release %s unchanged (revision %d).", release, revision)
	if stats.Created == 0 && stats.Replaced == 0 {
		return fmt.Sprintf("CRDs ready (%d already present). %s", stats.Ready, suffix)
	}
	parts := make([]string, 0, 2)
	if stats.Created > 0 {
		parts = append(parts, fmt.Sprintf("%d created", stats.Created))
	}
	if stats.Replaced > 0 {
		parts = append(parts, fmt.Sprintf("%d replaced", stats.Replaced))
	}
	return fmt.Sprintf("Applied %d %s (%s). %s", stats.Ready, crdNoun(stats.Ready), strings.Join(parts, ", "), suffix)
}

func crdNoun(n int) string {
	if n == 1 {
		return "CRD"
	}
	return "CRDs"
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

func applyBundleCRDs(c *nabat.Context, sess *session.Session, cluster *session.Cluster, bundle *extras.Bundle, opts *Options) (extras.CRDStats, error) {
	var stats extras.CRDStats
	if len(bundle.CRDs) == 0 {
		return stats, nil
	}
	restCfg, restErr := cluster.RESTConfig()
	if restErr != nil {
		return stats, fmt.Errorf("rest config for CRDs: %w%s", restErr, cmdopts.ClusterHint(restErr))
	}
	stats, crdErr := extras.ApplyCRDs(c, restCfg, bundle.CRDs, extras.Policy(opts.CRDs), sess.Timeout())
	if crdErr != nil {
		return stats, fmt.Errorf("apply CRDs: %w%s", crdErr, cmdopts.ClusterHint(crdErr))
	}
	return stats, nil
}

// applyDeploy re-renders and verifies determinism before the real Helm
// install/upgrade. CRDs from the bundle are applied first.
func applyDeploy(c *nabat.Context, sess *session.Session, cluster *session.Cluster, helmClient session.HelmClient, platform *spec.PlatformConfig, manifest *spec.Spec, opts *Options, resolved *spec.ResolvedSpec, plan *deployPlan, k8sClient kubernetes.Interface, k8sErr error, bundle *extras.Bundle, postRenderer postrenderer.PostRenderer) error {
	verify, verifyCleanup, err := helmClient.RenderManifests(c, manifest, opts.Environment, resolved, postRenderer)
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

	crdStats, crdErr := applyBundleCRDs(c, sess, cluster, bundle, opts)
	if crdErr != nil {
		return crdErr
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
	// call; the required-API check already ran there, before confirmation.
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
		helmErr := helmClient.InstallApp(c, manifest, opts.Environment, false, resolved, postRenderer)
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
	c.Success("Deployed"+summary+crdApplySuffix(crdStats), "project", manifest.Project, "environment", opts.Environment)
	return nil
}

// crdApplySuffix appends a short CRD count when this deploy created or
// replaced any CRDs (already-present-only is silent on the Helm path).
func crdApplySuffix(stats extras.CRDStats) string {
	if stats.Created == 0 && stats.Replaced == 0 {
		return ""
	}
	parts := make([]string, 0, 2)
	if stats.Created > 0 {
		parts = append(parts, fmt.Sprintf("%d created", stats.Created))
	}
	if stats.Replaced > 0 {
		parts = append(parts, fmt.Sprintf("%d replaced", stats.Replaced))
	}
	n := stats.Created + stats.Replaced
	return fmt.Sprintf("; applied %d %s (%s)", n, crdNoun(n), strings.Join(parts, ", "))
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

// filterCoveredAPIs drops requirements that will be satisfied by CRDs this
// deploy installs from .deployah/crds/ (any one matching group/version is
// enough, matching [k8s.CheckAPIRequirements]).
func filterCoveredAPIs(reqs []k8s.APIRequirement, covered map[string]struct{}) []k8s.APIRequirement {
	if len(covered) == 0 || len(reqs) == 0 {
		return reqs
	}
	out := make([]k8s.APIRequirement, 0, len(reqs))
	for _, req := range reqs {
		pending := false
		for _, gv := range req.GroupVersions {
			if _, ok := covered[gv]; ok {
				pending = true
				break
			}
		}
		if !pending {
			out = append(out, req)
		}
	}
	return out
}

// requiredAPIs derives the Kubernetes API group/version requirements for
// components deployed in the target environment, including cert-manager
// (TLS mode) via the resolved spec.
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
		// Same matcher as spec.Resolve and chart generation, so wildcard
		// deploys agree on the active component set.
		if len(component.Environments) > 0 {
			if _, ok := spec.MatchEnvKey(environment, component.Environments); !ok {
				continue
			}
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
