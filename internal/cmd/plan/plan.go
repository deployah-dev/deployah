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

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/cmd/cmdopts"
	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/plan/view"
	"deployah.dev/deployah/internal/session"
	"deployah.dev/deployah/internal/spec"

	planengine "deployah.dev/deployah/internal/plan"
)

const (
	outputFormatHuman = "human"
	outputFormatJSON  = "json"
)

// outputFormats lists the choices for --output, in help-text order.
var outputFormats = []string{outputFormatHuman, outputFormatJSON}

// ErrChangesPresent means --detailed-exitcode found effects.
// The root command maps it to exit code 2 and prints no banner.
var ErrChangesPresent = errors.New("plan has pending changes")

// Options holds command-line flags for plan.
type Options struct {
	Environment      string `nabat:"environment"`
	ShowSecrets      bool   `nabat:"show-secrets"`
	OutputFormat     string `nabat:"output"`
	DetailedExitCode bool   `nabat:"detailed-exitcode"`
}

// clusterReadersFunc builds a REST mapper and a live reader from cfg.
// It does not list or get objects.
type clusterReadersFunc func(
	cfg *rest.Config,
) (planengine.RESTMapper, planengine.LiveReader, error)

// The Helm client from the session must be usable by the semantic builder.
var _ planengine.SemanticBuildClient = session.HelmClient(nil)

// Register adds the plan command to app.
func Register(app *nabat.App) {
	app.MustCommand("plan",
		nabat.WithDescription("Inspect changes for an environment"),
		nabat.WithLongDescription("Render the chart for an environment and compare it with the previous release baseline selected by Helm. For an existing release, also compare that baseline with live cluster state. Plan is read-only and never applies anything."),
		nabat.WithArg("environment", "", nabat.WithRequired(), nabat.WithUsage("Environment to plan for"), nabat.WithPrompt("Environment", "", nabat.WithHint("e.g. prod, staging"))),
		nabat.WithSelectFlag("output", outputFormatHuman, outputFormats, nabat.WithShort('o'), nabat.WithUsage("Output format: human or json")),
		nabat.WithFlag("show-secrets", false, nabat.WithUsage("Reveal Kubernetes Secret data and stringData values in the selected output format (human or json); values are redacted by default")),
		nabat.WithFlag("detailed-exitcode", false, nabat.WithUsage("Exit 2 when the plan has effects (resource changes, tasks that change or run, chart CRDs Helm will process), 0 when it has none, 1 on error; drift alone exits 0")),
		nabat.WithExample(`
# Inspect changes for production
deployah plan production

# Machine-readable output for CI
deployah plan production -o json

# Gate a CI job on exit code 2 (pending effects) vs. 0 (no effects)
deployah plan production --detailed-exitcode`),
		nabat.WithRun(runPlan),
	)
}

func runPlan(c *nabat.Context) error {
	opts := &Options{}
	if err := c.Bind(opts); err != nil {
		return fmt.Errorf("binding options: %w", err)
	}
	sess := session.FromContext(c)
	ws := sess.Workspace()

	platform, platformErr := ws.Platform()
	if platformErr != nil {
		return fmt.Errorf("load platform file: %w", platformErr)
	}

	manifest, substReport, err := spec.Load(c, ws.SpecPath(), opts.Environment, platform)
	if err != nil {
		return fmt.Errorf("load spec: %w", err)
	}

	if platform == nil && cmdopts.HasExposeComponents(manifest) {
		return fmt.Errorf(
			"one or more components use expose blocks but no platform file was found; "+
				"create %s or set DEPLOYAH_PLATFORM_FILE, or pass --platform-file",
			spec.DefaultPlatformPath,
		)
	}

	envIdentity := spec.NormalizeEnv(opts.Environment)
	resolvedSpec, report, err := spec.Resolve(manifest, platform, envIdentity, substReport)
	if err != nil {
		if report != nil && report.ErrorCode != "" {
			return fmt.Errorf("resolution failed (%s): %w", report.ErrorCode, err)
		}
		return fmt.Errorf("resolution failed: %w", err)
	}

	return executePlan(c, sess, platform, manifest, opts, resolvedSpec, newClusterReaders)
}

// executePlan builds and writes the plan for opts.
func executePlan(c *nabat.Context, sess *session.Session, platform *spec.PlatformConfig, manifest *spec.Spec, opts *Options, resolvedSpec *spec.ResolvedSpec, newReaders clusterReadersFunc) error {
	cluster, err := sess.Target(c, opts.Environment)
	if err != nil {
		return fmt.Errorf("target cluster: %w", err)
	}

	helmClient, err := cluster.Helm()
	if err != nil {
		return fmt.Errorf("helm client: %w%s", err, cmdopts.ClusterHint(err))
	}

	if reachErr := helmClient.IsReachable(); reachErr != nil {
		return fmt.Errorf("%w%s", reachErr, cmdopts.ClusterHint(reachErr))
	}

	cmdopts.WarnContextFallback(c, cluster, opts.Environment)

	// Materialize self-signed TLS once, before render. A new keypair on
	// every render would show up as a Secret change.
	k8sClient, k8sErr := cluster.Kubernetes()
	if k8sErr != nil {
		c.Logger().Debug("kubernetes client unavailable", "err", k8sErr)
	}
	if resolvedSpec != nil {
		if tlsErr := cmdopts.MaterializeSelfSignedTLS(c, k8sClient, k8sErr, cluster.Namespace(), resolvedSpec); tlsErr != nil {
			return fmt.Errorf("materialize self-signed TLS: %w", tlsErr)
		}
	}

	// Plan always needs a Kubernetes config.
	restCfg, restErr := cluster.RESTConfig()
	if restErr != nil {
		return fmt.Errorf("kubernetes config: %w", restErr)
	}
	bundle, err := extras.LoadFromSpec(sess.Workspace().SpecPath(), manifest, platform, opts.Environment, cluster.Namespace(), restCfg)
	if err != nil {
		return fmt.Errorf("load extras: %w", err)
	}

	mapper, live, err := newReaders(restCfg)
	if err != nil {
		return fmt.Errorf("cluster readers: %w%s", err, cmdopts.ClusterHint(err))
	}

	// Compare with the previous release baseline Helm selected. An existing
	// release also compares that baseline with live objects.
	p, _, cleanup, err := planengine.BuildSemanticPlan(c, helmClient, mapper, live, planengine.SemanticBuildInput{
		ClusterContext: cluster.Context(),
		Resolved:       resolvedSpec,
		PostRenderer:   bundle.PostRendererFor(),
		CRDs:           bundle.CRDs,
		CRDDocs:        bundle.CRDDocs,
		SkipCRDs:       false, // plan never asks Helm to skip chart CRDs
	})
	defer cleanup()
	if err != nil {
		return fmt.Errorf("%w%s", err, cmdopts.ClusterHint(err))
	}
	err = writePlan(c, p, opts)
	if err != nil {
		return err
	}
	// The plan is already written. Exit 2 is only a signal for CI.
	if opts.DetailedExitCode && p.HasEffects() {
		return ErrChangesPresent
	}
	return nil
}

func writePlan(c *nabat.Context, p semantic.Plan, opts *Options) error {
	// --show-secrets reveals values in human and JSON.
	// Otherwise they stay redacted.
	renderOpts := view.Options{ShowSecrets: opts.ShowSecrets}
	if opts.OutputFormat == outputFormatJSON {
		if err := view.WriteJSON(c.IO().Out, p, renderOpts); err != nil {
			return fmt.Errorf("write json plan: %w", err)
		}
		return nil
	}
	renderOpts.Styler = nabatStyler{c: c}
	if err := view.WriteHuman(c.IO().Out, p, renderOpts); err != nil {
		return fmt.Errorf("write human plan: %w", err)
	}
	return nil
}

func newClusterReaders(
	cfg *rest.Config,
) (planengine.RESTMapper, planengine.LiveReader, error) {
	// Deferred discovery and the dynamic client send no requests here.
	disco, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("discovery client: %w", err)
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(disco))
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("dynamic client: %w", err)
	}
	return mapper, planengine.NewDynamicLiveReader(dyn), nil
}
