// Copyright 2025 The Deployah Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package plan

import (
	"errors"
	"fmt"

	"nabat.dev/nabat"
	"nabat.dev/theme"

	"deployah.dev/deployah/internal/cmd/common"
	"deployah.dev/deployah/internal/drift"
	"deployah.dev/deployah/internal/k8s"
	"deployah.dev/deployah/internal/session"
	"deployah.dev/deployah/internal/spec"

	planengine "deployah.dev/deployah/internal/plan"
)

const (
	outputFormatText = "text"
	outputFormatJSON = "json"
)

// outputFormats lists the choices for --output, in help-text order.
var outputFormats = []string{outputFormatText, outputFormatJSON}

// Options holds command-line flags for plan.
type Options struct {
	Environment      string `nabat:"environment"`
	Drift            bool   `nabat:"drift"`
	Offline          bool   `nabat:"offline"`
	ShowSecrets      bool   `nabat:"show-secrets"`
	Raw              bool   `nabat:"raw"`
	YAML             bool   `nabat:"yaml"`
	OutputFormat     string `nabat:"output"`
	DetailedExitCode bool   `nabat:"detailed-exitcode"`
}

// Register adds the plan command to app.
func Register(app *nabat.App) {
	app.MustCommand("plan",
		nabat.WithDescription("Preview the changes a deploy would make"),
		nabat.WithLongDescription("Render the chart for an environment and show what would change compared to the last successful release, without applying anything."),
		nabat.WithArg("environment", "", nabat.WithRequired(), nabat.WithUsage("Environment to plan for"), nabat.WithPrompt("Environment", "", nabat.WithHint("e.g. prod, staging"))),
		nabat.WithFlag("drift", false, nabat.WithUsage("Detect drift between the rendered manifests and the live cluster state (requires cluster access; not compatible with --offline)")),
		nabat.WithFlag("offline", false, nabat.WithUsage("Render and validate the chart without contacting the cluster")),
		nabat.WithFlag("show-secrets", false, nabat.WithUsage("Reveal masked secret values in text output (requires an interactive terminal; refused with --output json)")),
		nabat.WithFlag("raw", false, nabat.WithUsage("Show raw Kubernetes field paths instead of the compact Deployah vocabulary")),
		nabat.WithFlag("yaml", false, nabat.WithUsage("Show changed fields as YAML blocks instead of a single line")),
		nabat.WithSelectFlag("output", outputFormatText, outputFormats, nabat.WithUsage("Output format")),
		nabat.WithFlag("detailed-exitcode", false, nabat.WithUsage("Exit 2 when the plan has pending changes, 0 when it does not, 1 on error (for CI)")),
		nabat.WithValidation(validateOptions),
		nabat.WithExample(`
# Preview what a deploy would change
deployah plan production

# Render and validate the chart without touching the cluster
deployah plan production --offline

# Machine-readable output for CI
deployah plan production --output json

# Gate a CI job on exit code 2 (pending changes) vs. 0 (no changes)
deployah plan production --detailed-exitcode`),
		nabat.WithRun(func(c *nabat.Context) error {
			// Captured here (not read from *nabat.Context, which has no
			// public accessor for it) so RenderText can color its output;
			// see internal/plan/format_text.go's TextOptions.Theme doc.
			return runPlan(c, app.Theme())
		}),
	)
}

// validateOptions rejects flag combinations that cannot both take effect,
// before runPlan does any work.
func validateOptions(c *nabat.Context) error {
	opts := &Options{}
	if err := c.Bind(opts); err != nil {
		return fmt.Errorf("binding options: %w", err)
	}

	if opts.Raw && opts.YAML {
		return errors.New("--raw and --yaml cannot be used together")
	}
	if opts.ShowSecrets && opts.OutputFormat == outputFormatJSON {
		return errors.New("--show-secrets cannot be used with --output json: JSON output always masks secrets")
	}
	if opts.ShowSecrets && !c.IsInteractive() {
		return errors.New("--show-secrets requires an interactive terminal")
	}
	if opts.Offline && opts.OutputFormat == outputFormatJSON {
		return errors.New("--offline has no diff to report as JSON; drop --output json")
	}
	if opts.Drift && opts.Offline {
		return errors.New("--drift requires cluster access; it cannot be used with --offline")
	}
	return nil
}

func runPlan(c *nabat.Context, resolvedTheme theme.ResolvedTheme) error {
	opts := &Options{}
	if err := c.Bind(opts); err != nil {
		return fmt.Errorf("binding options: %w", err)
	}

	sess := session.FromContext(c)

	// Prescan the raw (pre-envsubst) manifest for ${VAR} tokens so the
	// resolver can distinguish static from dynamic subdomains, matching
	// deploy's resolution behavior.
	rawSpec, _, rawErr := spec.ParseManifest(sess.SpecPath())
	if rawErr != nil {
		return fmt.Errorf("parse manifest: %w", rawErr)
	}
	substReport := spec.PrescanSubstitutionReport(rawSpec)

	platform, platformErr := sess.Platform()
	if platformErr != nil {
		return fmt.Errorf("load platform file: %w", platformErr)
	}

	manifest, err := spec.Load(c, sess.SpecPath(), opts.Environment, platform)
	if err != nil {
		return fmt.Errorf("load spec: %w", err)
	}

	if platform == nil && common.HasExposeComponents(manifest) {
		return fmt.Errorf(
			"one or more components use expose blocks but no platform file was found; "+
				"create %s or set DEPLOYAH_PLATFORM_FILE, or pass --platform-file",
			spec.DefaultPlatformPath,
		)
	}

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

	if opts.Offline {
		return runOffline(c, manifest, opts, resolvedSpec)
	}
	return runOnline(c, sess, manifest, opts, resolvedSpec, resolvedTheme)
}

// runOffline renders the chart without contacting the cluster and prints a
// resource count instead of a diff: there is no reachable release history to
// compare against.
func runOffline(c *nabat.Context, manifest *spec.Spec, opts *Options, resolvedSpec *spec.ResolvedSpec) error {
	sess := session.FromContext(c)

	cluster, err := sess.Target(c, opts.Environment)
	if err != nil {
		return fmt.Errorf("target cluster: %w", err)
	}
	helmClient, err := cluster.Helm()
	if err != nil {
		return fmt.Errorf("helm client: %w", err)
	}

	// Offline mode never contacts the cluster, so any self-signed TLS cert
	// is generated fresh (nil client) rather than fetched/reused -- a
	// deliberate offline generation, not common.MaterializeSelfSignedTLS's
	// fail-closed path for an online command that couldn't build a client.
	if resolvedSpec != nil {
		if tlsErr := k8s.MaterializeSelfSignedTLS(c, nil, "", resolvedSpec); tlsErr != nil {
			return fmt.Errorf("materialize self-signed TLS: %w", tlsErr)
		}
	}

	result, cleanup, err := helmClient.RenderOffline(c, manifest, opts.Environment, resolvedSpec)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return fmt.Errorf("render manifests: %w", err)
	}

	count, err := planengine.CountResources(result.Manifest)
	if err != nil {
		return fmt.Errorf("count rendered resources: %w", err)
	}

	c.Println(fmt.Sprintf("Rendered %d resources for environment '%s' (no cluster comparison).", count, opts.Environment))
	c.Println("validation: OK")
	return nil
}

// runOnline renders the chart, diffs it against the last successful
// release, and displays the resulting plan.
func runOnline(c *nabat.Context, sess *session.Session, manifest *spec.Spec, opts *Options, resolvedSpec *spec.ResolvedSpec, resolvedTheme theme.ResolvedTheme) error {
	cluster, err := sess.Target(c, opts.Environment)
	if err != nil {
		return fmt.Errorf("target cluster: %w", err)
	}

	helmClient, err := cluster.Helm()
	if err != nil {
		return fmt.Errorf("helm client: %w%s", err, common.ClusterHint(err))
	}

	if reachErr := helmClient.IsReachable(); reachErr != nil {
		return fmt.Errorf("%w%s", reachErr, common.ClusterHint(reachErr))
	}

	common.WarnContextFallback(c, cluster, opts.Environment)

	// Materialize self-signed TLS certs once, before rendering, matching
	// deploy's determinism guarantee (a fresh keypair per render would make
	// every plan show a phantom Secret change).
	k8sClient, k8sErr := cluster.Kubernetes()
	if k8sErr != nil {
		c.Logger().Debug("kubernetes client unavailable", "err", k8sErr)
	}
	if resolvedSpec != nil {
		if tlsErr := common.MaterializeSelfSignedTLS(c, k8sClient, k8sErr, cluster.Namespace(), resolvedSpec); tlsErr != nil {
			return fmt.Errorf("materialize self-signed TLS: %w", tlsErr)
		}
	}

	p, result, cleanup, err := planengine.BuildPlan(c, helmClient, manifest, opts.Environment, cluster.Context(), resolvedSpec)
	defer cleanup()
	if err != nil {
		return fmt.Errorf("%w%s", err, common.ClusterHint(err))
	}

	if opts.Drift {
		if driftErr := checkDrift(c, cluster, p, result.Manifest); driftErr != nil {
			return fmt.Errorf("check drift: %w%s", driftErr, common.ClusterHint(driftErr))
		}
	}

	return outputPlan(c, p, opts, resolvedTheme)
}

// checkDrift runs `--drift` detection against the resolved cluster and
// records the outcome directly on p (DriftChecked, Drift, DriftIncomplete),
// so it takes effect no matter which renderer outputPlan picks. On a fresh
// install there's no live release to compare against, so it reports that
// via c.Info (stderr, not the stdout diff body) and leaves DriftChecked
// false.
func checkDrift(c *nabat.Context, cluster *session.Cluster, p *planengine.Plan, currentManifest string) error {
	if p.Header.FreshInstall {
		c.Info("--drift is a no-op on a fresh install; there is no live release to compare against.")
		return nil
	}

	cfg, err := cluster.RESTConfig()
	if err != nil {
		return fmt.Errorf("kubernetes config: %w", err)
	}
	predictor, err := drift.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("drift client: %w", err)
	}

	result, err := drift.ComputeDrift(c, predictor, p, currentManifest)
	if err != nil {
		return fmt.Errorf("compute drift: %w", err)
	}

	p.DriftChecked = true
	p.Drift = result.Changes
	p.DriftIncomplete = result.Incomplete
	return nil
}

func outputPlan(c *nabat.Context, p *planengine.Plan, opts *Options, resolvedTheme theme.ResolvedTheme) error {
	if opts.OutputFormat == outputFormatJSON {
		if err := planengine.RenderJSON(c.IO().Out, p); err != nil {
			return fmt.Errorf("render json: %w", err)
		}
	} else {
		textOpts := planengine.TextOptions{
			Mode:        textMode(opts),
			ShowSecrets: opts.ShowSecrets,
			Theme:       resolvedTheme,
		}
		if err := planengine.RenderText(c.IO().Out, p, textOpts); err != nil {
			return fmt.Errorf("render text: %w", err)
		}
	}

	if opts.DetailedExitCode && p.HasChanges() {
		return planengine.ErrChangesPresent
	}
	return nil
}

func textMode(opts *Options) planengine.Mode {
	switch {
	case opts.Raw:
		return planengine.ModeRaw
	case opts.YAML:
		return planengine.ModeYAML
	default:
		return planengine.ModeCompact
	}
}
