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

package helm

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/v2/loader"

	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/spec"

	chart "helm.sh/helm/v4/pkg/chart/v2"
)

// RenderManifests renders the chart via Helm's DryRunClient strategy, so
// hooks/templates see the same values, capabilities, and revision as a real
// apply. It mirrors InstallApp's install-vs-upgrade decision so the result
// compares 1:1 with what InstallApp would produce, but on an upgrade this
// means it also performs InstallApp's cluster-reachability check (skipped
// only for a fresh install).
//
// Callers must invoke the returned cleanup func; ChartPath is not removed
// automatically so callers like deploy can reuse it for the real apply.
func (c *Client) RenderManifests(ctx context.Context, manifest *spec.Spec, environment string, resolved *spec.ResolvedSpec) (result *render.RenderResult, cleanup func(), err error) {
	releaseName := GenerateReleaseName(manifest.Project, environment)

	ch, chartPath, cleanup, err := c.prepareAndLoadChart(ctx, manifest, environment, resolved)
	if err != nil {
		return nil, nil, err
	}

	values, labels := renderInputs(manifest, environment)

	history := action.NewHistory(c.config)
	history.Max = 1
	if _, histErr := history.Run(releaseName); histErr != nil {
		// Not found -> fresh install. Any other history error is treated
		// the same way InstallApp does: fall through to an install attempt,
		// which will surface a clearer error if something else is wrong.
		result, err = c.renderInstall(ctx, releaseName, ch, values, labels)
	} else {
		result, err = c.renderUpgrade(ctx, releaseName, ch, values, labels)
	}
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	result.ChartPath = chartPath
	return result, cleanup, nil
}

// RenderOffline renders the chart for manifest/environment as a fresh
// install, without any Kubernetes API access: no reachability check and no
// release-history lookup. It is the engine behind `deployah plan --offline`.
// Because it never looks at release history, the result always describes a
// fresh install (IsUpgrade false, Revision 1) even when a release already
// exists, so it can't be diffed against a prior release like
// [Client.RenderManifests] can; use that instead when cluster access is fine.
func (c *Client) RenderOffline(ctx context.Context, manifest *spec.Spec, environment string, resolved *spec.ResolvedSpec) (result *render.RenderResult, cleanup func(), err error) {
	releaseName := GenerateReleaseName(manifest.Project, environment)

	ch, chartPath, cleanup, err := c.prepareAndLoadChart(ctx, manifest, environment, resolved)
	if err != nil {
		return nil, nil, err
	}

	values, labels := renderInputs(manifest, environment)

	result, err = c.renderInstall(ctx, releaseName, ch, values, labels)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	result.ChartPath = chartPath
	return result, cleanup, nil
}

// prepareAndLoadChart generates (or fetches from cache) the Helm chart for
// manifest/environment and loads it, returning a cleanup func for the
// generated chart directory. It is the common first step shared by
// [Client.RenderManifests] and [Client.RenderOffline]. When the client was
// constructed with WithDebug(true), cleanup is a no-op and the temp dir is
// left behind for inspection.
func (c *Client) prepareAndLoadChart(ctx context.Context, manifest *spec.Spec, environment string, resolved *spec.ResolvedSpec) (ch *chart.Chart, chartPath string, cleanup func(), err error) {
	chartPath, err = PrepareChart(ctx, manifest, environment, resolved)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to prepare chart: %w", err)
	}

	cleanup = func() {}
	if !c.debug {
		cleanup = func() {
			if removeErr := os.RemoveAll(chartPath); removeErr != nil {
				slog.WarnContext(ctx, "failed to cleanup chart temp dir", "path", chartPath, "err", removeErr)
			}
		}
	}

	ch, err = loader.Load(chartPath)
	if err != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("failed to load chart: %w", err)
	}
	return ch, chartPath, cleanup, nil
}

// renderInputs builds the Helm values and labels shared by every render
// path (install, upgrade, offline). Values are empty: the chart's own
// values.yaml, written by [PrepareChart], already carries the mapped spec
// data. This mirrors InstallApp.
func renderInputs(manifest *spec.Spec, environment string) (values map[string]any, labels map[string]string) {
	values = map[string]any{}
	labels = map[string]string{
		"deployah.dev/project":     manifest.Project,
		"deployah.dev/environment": environment,
		"deployah.dev/managed-by":  "deployah",
		"deployah.dev/version":     manifest.APIVersion,
	}
	return values, labels
}

// restoreConfigForDryRun snapshots the Configuration fields a client-side dry
// run can mutate and returns a func that restores them. c.config is shared
// with the real, non-dry-run InstallApp on this Client, so an unrestored
// swap would silently turn every later real install/upgrade into a no-op
// that reports success but never touches the cluster. Call it as
// `defer restoreConfigForDryRun(cfg)()` around any action that might dry-run.
func restoreConfigForDryRun(cfg *action.Configuration) func() {
	// Install's dry-run path (action/install.go, !interactWithServer branch)
	// sets KubeClient to a kubefake.PrintingKubeClient that discards
	// everything, and Releases to a throwaway in-memory store.
	kubeClient := cfg.KubeClient
	releases := cfg.Releases
	// Upgrade's dry-run path writes MaxHistory in place on the shared
	// Releases storage object, so restoring the pointer alone would not
	// undo that field write; it must be snapshotted separately.
	maxHistory := 0
	if releases != nil {
		maxHistory = releases.MaxHistory
	}
	return func() {
		cfg.KubeClient = kubeClient
		cfg.Releases = releases
		if releases != nil {
			releases.MaxHistory = maxHistory
		}
	}
}

// restoreCapabilitiesForDryRun wraps restoreConfigForDryRun and additionally
// snapshots/restores cfg.Capabilities. Only Install's dry-run path swaps
// Capabilities to a fake value; Upgrade instead populates it on demand from
// the real cluster and caches it for reuse, so restoring it after an
// upgrade dry-run would discard a legitimate discovery result. Use this
// variant only for renderInstall.
func restoreCapabilitiesForDryRun(cfg *action.Configuration) func() {
	restoreRest := restoreConfigForDryRun(cfg)
	capabilities := cfg.Capabilities
	return func() {
		restoreRest()
		cfg.Capabilities = capabilities
	}
}

func (c *Client) renderInstall(ctx context.Context, releaseName string, ch *chart.Chart, values map[string]any, labels map[string]string) (*render.RenderResult, error) {
	// See restoreCapabilitiesForDryRun: a client-side dry-run install swaps
	// cfg.KubeClient, cfg.Releases, and cfg.Capabilities. Restore them once
	// this render is done so only this call is affected.
	defer restoreCapabilitiesForDryRun(c.config)()

	install := action.NewInstall(c.config)
	install.ReleaseName = releaseName
	install.Namespace = c.settings.Namespace()
	install.CreateNamespace = true
	install.DryRunStrategy = action.DryRunClient
	install.DisableOpenAPIValidation = true
	install.Labels = labels

	rel, runErr := install.RunWithContext(ctx, ch, values)
	if runErr != nil {
		return nil, c.wrapHelmError("render", releaseName, runErr)
	}
	v1rel, convErr := releaserToV1(rel)
	if convErr != nil {
		return nil, convErr
	}
	return &render.RenderResult{
		ReleaseName: releaseName,
		Namespace:   install.Namespace,
		Manifest:    v1rel.Manifest,
		Hooks:       v1rel.Hooks,
		IsUpgrade:   false,
		Revision:    1,
	}, nil
}

func (c *Client) renderUpgrade(ctx context.Context, releaseName string, ch *chart.Chart, values map[string]any, labels map[string]string) (*render.RenderResult, error) {
	// Upgrade's dry-run path doesn't swap KubeClient/Releases as of Helm
	// v4.2.1; restoring defensively guards against a future version adding
	// the short-circuit Install already has. Capabilities is deliberately
	// not restored here (see restoreCapabilitiesForDryRun): Upgrade's fetch
	// is a legitimate cache other calls should reuse.
	defer restoreConfigForDryRun(c.config)()

	upgrade := action.NewUpgrade(c.config)
	upgrade.Namespace = c.settings.Namespace()
	upgrade.DryRunStrategy = action.DryRunClient
	upgrade.DisableOpenAPIValidation = true
	upgrade.Labels = labels

	rel, runErr := upgrade.RunWithContext(ctx, releaseName, ch, values)
	if runErr != nil {
		return nil, c.wrapHelmError("render", releaseName, runErr)
	}
	v1rel, convErr := releaserToV1(rel)
	if convErr != nil {
		return nil, convErr
	}
	return &render.RenderResult{
		ReleaseName: releaseName,
		Namespace:   upgrade.Namespace,
		Manifest:    v1rel.Manifest,
		Hooks:       v1rel.Hooks,
		IsUpgrade:   true,
		Revision:    v1rel.Version,
	}, nil
}
