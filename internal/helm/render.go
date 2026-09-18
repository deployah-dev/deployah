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

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/spec"

	chartcommon "helm.sh/helm/v4/pkg/chart/common"
	chart "helm.sh/helm/v4/pkg/chart/v2"
)

// offlineMonitorAPIVersion lets ServiceMonitor/PodMonitor templates
// render when there is no discovery client.
const offlineMonitorAPIVersion = "monitoring.coreos.com/v1"

// RenderManifests renders the chart from [spec.ResolvedSpec] client-side.
// The cluster must be reachable. Use [Client.RenderOffline] when there is
// no Kubernetes API access. Callers must run the returned cleanup func.
func (c *Client) RenderManifests(ctx context.Context, resolved *spec.ResolvedSpec, postRenderer postrenderer.PostRenderer, crds []extras.Object) (*render.RenderResult, func(), error) {
	result, _, cleanup, err := c.RenderManifestsWithPrep(ctx, resolved, postRenderer, crds)
	return result, cleanup, err
}

// RenderManifestsWithPrep renders the chart from [spec.ResolvedSpec]
// client-side and returns the [ReleasePrep] used to choose install or
// upgrade. crds are written into the per-invocation chart copy. Cleanup is
// nil on error; on success the caller must run it once.
func (c *Client) RenderManifestsWithPrep(ctx context.Context, resolved *spec.ResolvedSpec, postRenderer postrenderer.PostRenderer, crds []extras.Object) (*render.RenderResult, ReleasePrep, func(), error) {
	releaseName, labels, err := releaseIdentity(resolved)
	if err != nil {
		return nil, ReleasePrep{}, nil, err
	}

	prep, err := c.lookupReleasePrep(releaseName)
	if err != nil {
		return nil, ReleasePrep{}, nil, err
	}

	ch, chartPath, cleanup, err := c.prepareAndLoadChart(ctx, resolved, crds)
	if err != nil {
		return nil, ReleasePrep{}, nil, err
	}

	values := map[string]any{}
	var result *render.RenderResult
	switch prep.Operation {
	case OperationInstall:
		result, err = c.renderInstall(ctx, releaseName, ch, values, labels, postRenderer)
	case OperationUpgrade:
		result, err = c.renderUpgrade(ctx, releaseName, ch, values, labels, postRenderer)
	default:
		cleanup()
		return nil, ReleasePrep{}, nil, fmt.Errorf("invalid helm operation %d", prep.Operation)
	}
	if err != nil {
		cleanup()
		return nil, ReleasePrep{}, nil, err
	}
	result.ChartPath = chartPath
	return result, prep, cleanup, nil
}

// RenderOffline renders the chart from [spec.ResolvedSpec] as a fresh
// install without Kubernetes API access. A nil or unresolved spec is an
// error. Callers must run the returned cleanup func.
func (c *Client) RenderOffline(ctx context.Context, resolved *spec.ResolvedSpec, postRenderer postrenderer.PostRenderer, crds []extras.Object) (result *render.RenderResult, cleanup func(), err error) {
	releaseName, labels, err := releaseIdentity(resolved)
	if err != nil {
		return nil, nil, err
	}

	ch, chartPath, cleanup, err := c.prepareAndLoadChart(ctx, resolved, crds)
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
// and loads it. crds are written into the returned copy. Cleanup removes
// the temp dir unless WithDebug(true).
func (c *Client) prepareAndLoadChart(ctx context.Context, resolved *spec.ResolvedSpec, crds []extras.Object) (ch *chart.Chart, chartPath string, cleanup func(), err error) {
	chartPath, err = PrepareChart(ctx, resolved, c.chartCache, crds)
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

// restoreConfigForDryRun restores cfg after a client-side dry run.
// Call as `defer restoreConfigForDryRun(cfg)()`. Skip it and later
// real installs are a silent no-op.
func restoreConfigForDryRun(cfg *action.Configuration) func() {
	// Helm install dry-run replaces KubeClient and Releases with fakes.
	kubeClient := cfg.KubeClient
	releases := cfg.Releases
	// Upgrade dry-run writes MaxHistory in place; restoring the pointer
	// is not enough.
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
	// Dry-run install mutates the shared config. Restore when this returns.
	defer restoreCapabilitiesForDryRun(c.config)()

	install := c.newInstallAction(releaseName, labels, postRenderer, false)
	install.DryRunStrategy = action.DryRunClient
	install.DisableOpenAPIValidation = true
	// Client dry-run sees only built-in APIs. Add the monitor GV so
	// ServiceMonitor templates still render.
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
	// Restore KubeClient/Releases in case Helm later swaps them like
	// install dry-run. Keep Capabilities so later calls reuse discovery.
	defer restoreConfigForDryRun(c.config)()

	upgrade := c.newUpgradeAction(labels, postRenderer)
	upgrade.DryRunStrategy = action.DryRunClient
	upgrade.DisableOpenAPIValidation = true

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
