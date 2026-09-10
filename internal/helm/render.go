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
	"helm.sh/helm/v4/pkg/postrenderer"

	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/spec"

	chartcommon "helm.sh/helm/v4/pkg/chart/common"
	chart "helm.sh/helm/v4/pkg/chart/v2"
)

// offlineMonitorAPIVersion lets ServiceMonitor/PodMonitor templates
// render when there is no discovery client.
const offlineMonitorAPIVersion = "monitoring.coreos.com/v1"

// RenderManifests renders the chart from [spec.ResolvedSpec] client-side,
// matching [Client.InstallApp]. Upgrade dry-runs still check cluster
// reachability. A nil or unresolved spec is an error. Callers must run
// the returned cleanup func.
func (c *Client) RenderManifests(ctx context.Context, resolved *spec.ResolvedSpec, postRenderer postrenderer.PostRenderer) (result *render.RenderResult, cleanup func(), err error) {
	releaseName, labels, err := releaseIdentity(resolved)
	if err != nil {
		return nil, nil, err
	}

	ch, chartPath, cleanup, err := c.prepareAndLoadChart(ctx, resolved)
	if err != nil {
		return nil, nil, err
	}

	values := map[string]any{}

	history := action.NewHistory(c.config)
	history.Max = 1
	if _, histErr := history.Run(releaseName); histErr != nil {
		// Not found -> fresh install. Any other history error is treated
		// the same way InstallApp does: fall through to an install attempt,
		// which will surface a clearer error if something else is wrong.
		result, err = c.renderInstall(ctx, releaseName, ch, values, labels, postRenderer)
	} else {
		result, err = c.renderUpgrade(ctx, releaseName, ch, values, labels, postRenderer)
	}
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	result.ChartPath = chartPath
	return result, cleanup, nil
}

// RenderOffline renders the chart from [spec.ResolvedSpec] as a fresh
// install without Kubernetes API access. A nil or unresolved spec is an
// error. Callers must run the returned cleanup func.
func (c *Client) RenderOffline(ctx context.Context, resolved *spec.ResolvedSpec, postRenderer postrenderer.PostRenderer) (result *render.RenderResult, cleanup func(), err error) {
	releaseName, labels, err := releaseIdentity(resolved)
	if err != nil {
		return nil, nil, err
	}

	ch, chartPath, cleanup, err := c.prepareAndLoadChart(ctx, resolved)
	if err != nil {
		return nil, nil, err
	}

	values := map[string]any{}

	result, err = c.renderInstall(ctx, releaseName, ch, values, labels, postRenderer)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	result.ChartPath = chartPath
	return result, cleanup, nil
}

// prepareAndLoadChart generates or caches the Helm chart from resolved
// and loads it. Cleanup removes the temp dir unless WithDebug(true).
func (c *Client) prepareAndLoadChart(ctx context.Context, resolved *spec.ResolvedSpec) (ch *chart.Chart, chartPath string, cleanup func(), err error) {
	chartPath, err = PrepareChart(ctx, resolved, c.chartCache)
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

// restoreConfigForDryRun returns a func that restores cfg after a client-side
// dry run. Call as `defer restoreConfigForDryRun(cfg)()`. Skipping restore
// makes later real installs a silent no-op.
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

// restoreCapabilitiesForDryRun restores cfg including Capabilities.
// Use only from renderInstall; upgrade dry-run must keep discovered
// Capabilities.
func restoreCapabilitiesForDryRun(cfg *action.Configuration) func() {
	restoreRest := restoreConfigForDryRun(cfg)
	capabilities := cfg.Capabilities
	return func() {
		restoreRest()
		cfg.Capabilities = capabilities
	}
}

func (c *Client) renderInstall(ctx context.Context, releaseName string, ch *chart.Chart, values map[string]any, labels map[string]string, postRenderer postrenderer.PostRenderer) (*render.RenderResult, error) {
	// See restoreCapabilitiesForDryRun: a client-side dry-run install swaps
	// cfg.KubeClient, cfg.Releases, and cfg.Capabilities. Restore them once
	// this render is done so only this call is affected.
	defer restoreCapabilitiesForDryRun(c.config)()

	install := action.NewInstall(c.config)
	install.ReleaseName = releaseName
	install.Namespace = c.Namespace()
	install.CreateNamespace = true
	install.DryRunStrategy = action.DryRunClient
	install.DisableOpenAPIValidation = true
	install.Labels = labels
	install.PostRenderer = postRenderer
	// DryRunClient resets Capabilities to DefaultCapabilities (built-in
	// APIs only). Append the Prometheus Operator GV so chart templates
	// gated on .Capabilities.APIVersions.Has still render offline.
	install.APIVersions = chartcommon.VersionSet{offlineMonitorAPIVersion}

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

func (c *Client) renderUpgrade(ctx context.Context, releaseName string, ch *chart.Chart, values map[string]any, labels map[string]string, postRenderer postrenderer.PostRenderer) (*render.RenderResult, error) {
	// Upgrade's dry-run path doesn't swap KubeClient/Releases as of Helm
	// v4.2.1; restoring defensively guards against a future version adding
	// the short-circuit Install already has. Capabilities is deliberately
	// not restored here (see restoreCapabilitiesForDryRun): Upgrade's fetch
	// is a legitimate cache other calls should reuse.
	defer restoreConfigForDryRun(c.config)()

	upgrade := action.NewUpgrade(c.config)
	upgrade.Namespace = c.Namespace()
	upgrade.DryRunStrategy = action.DryRunClient
	upgrade.DisableOpenAPIValidation = true
	upgrade.Labels = labels
	upgrade.PostRenderer = postRenderer

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
