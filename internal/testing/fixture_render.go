// Copyright 2026 The Deployah Authors
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

package testing

import (
	"fmt"
	"os"
	"testing"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/postrenderer"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// fixtureChartCache reuses prepared charts for the test process.
// [helm.PrepareChart] returns a per-render copy; callers delete only that
// copy. The cache keeps its backing directories.
var fixtureChartCache = helm.NewChartCache(time.Hour)

// renderFixtureChart renders resolved as a fresh Helm install in
// [fixtureNamespace] without Kubernetes access. The chart comes from
// [helm.PrepareChart]. Helm renders it with a client-only install dry run.
// Cleanup removes the per-render chart copy when tb finishes.
func renderFixtureChart(tb testing.TB, resolved *spec.ResolvedSpec, postRenderer postrenderer.PostRenderer, crds []extras.RawFile) (*render.RenderResult, error) {
	tb.Helper()

	chartPath, err := helm.PrepareChart(tb.Context(), resolved, fixtureChartCache, crds)
	if err != nil {
		return nil, fmt.Errorf("prepare chart: %w", err)
	}
	tb.Cleanup(func() {
		if removeErr := os.RemoveAll(chartPath); removeErr != nil {
			tb.Errorf("remove chart dir %s: %v", chartPath, removeErr)
		}
	})

	ch, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("load chart: %w", err)
	}

	releaseName := helm.GenerateReleaseName(resolved.Spec.Project, resolved.Env.Original)
	install := action.NewInstall(action.NewConfiguration())
	install.ReleaseName = releaseName
	install.Namespace = fixtureNamespace
	install.DryRunStrategy = action.DryRunClient
	install.DisableOpenAPIValidation = true
	install.PostRenderer = postRenderer

	rel, err := install.RunWithContext(tb.Context(), ch, map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("helm install dry run: %w", err)
	}
	v1rel, ok := rel.(*v1.Release)
	if !ok {
		return nil, fmt.Errorf("unexpected helm release type %T", rel)
	}
	return &render.RenderResult{
		ReleaseName: releaseName,
		Namespace:   install.Namespace,
		Manifest:    v1rel.Manifest,
		Hooks:       v1rel.Hooks,
		IsUpgrade:   false,
		Revision:    1,
		ChartPath:   chartPath,
	}, nil
}
