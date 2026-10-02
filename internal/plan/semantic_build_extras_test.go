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

package plan_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/postrenderer"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

const (
	extrasPlanProject   = "plan-extras-fresh-install"
	extrasPlanEnv       = "dev"
	extrasPlanNamespace = "dev"
	extrasPlanPolicy    = "plan-extras-deny"
)

// TestBuildSemanticPlan_ExtraManifestIsFreshInstallCreate is the regression
// for a resource loaded from .deployah/manifests/. LoadFromSpec, the extras
// post-renderer, and the Helm client-only render run, then BuildSemanticPlan
// must report that object as a fresh-install create beside the chart resources.
func TestBuildSemanticPlan_ExtraManifestIsFreshInstallCreate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	specPath := filepath.Join(dir, "deployah.yaml")
	writeExtrasPlanFile(t, specPath, extrasPlanSpec)
	writeExtrasPlanFile(t, filepath.Join(dir, ".deployah", "manifests", "networkpolicy.yaml"), extrasPlanNetworkPolicy)

	manifest, subst, err := spec.Load(t.Context(), specPath, extrasPlanEnv, nil)
	require.NoError(t, err)
	envName, _, err := spec.ResolveEnvironment(manifest.Environments, nil, extrasPlanEnv)
	require.NoError(t, err)
	resolved, _, err := spec.Resolve(manifest, nil, spec.NormalizeEnv(envName), subst)
	require.NoError(t, err)

	bundle, err := extras.LoadFromSpec(specPath, manifest, nil, extrasPlanEnv, extrasPlanNamespace, nil)
	require.NoError(t, err)
	post := bundle.PostRendererFor()
	require.NotNil(t, post)

	client := &extrasInstallClient{
		tb:        t,
		cache:     helm.NewChartCache(time.Hour),
		namespace: extrasPlanNamespace,
	}
	p, result, cleanup, err := plan.BuildSemanticPlan(
		t.Context(),
		client,
		newMapper(),
		nil,
		plan.SemanticBuildInput{
			ClusterContext: "kind-dev",
			Resolved:       resolved,
			PostRenderer:   post,
		},
	)
	t.Cleanup(cleanup)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.Manifest, "kind: NetworkPolicy")
	assert.Contains(t, result.Manifest, "name: "+extrasPlanPolicy)

	generated := helm.GenerateReleaseName(extrasPlanProject, extrasPlanEnv) + "-web"
	assert.True(t, p.Header.FreshInstall)
	assert.Equal(t, semantic.HelmInstall, p.HelmAction)
	assert.Equal(t, extrasPlanProject, p.Header.Project)
	assert.Equal(t, extrasPlanEnv, p.Header.Environment)
	assert.Equal(t, extrasPlanNamespace, p.Header.Namespace)
	assert.Equal(t, helm.GenerateReleaseName(extrasPlanProject, extrasPlanEnv), p.Header.Release)

	got := make([]string, 0, len(p.Changes))
	for _, change := range p.Changes {
		assert.Equal(t, semantic.Create, change.Action)
		assert.Equal(t, extrasPlanNamespace, change.Resource.Namespace)
		assert.Nil(t, change.Before)
		require.NotNil(t, change.After)
		assert.Empty(t, change.Fields)
		got = append(got, change.Resource.APIVersion+" "+change.Resource.Kind+" "+change.Resource.Name)
	}
	assert.ElementsMatch(t, []string{
		"apps/v1 Deployment " + generated,
		"networking.k8s.io/v1 NetworkPolicy " + extrasPlanPolicy,
		"v1 Service " + generated,
	}, got)
}

// extrasInstallClient renders a fresh install with Helm's client-only dry run
// and applies the PostRenderer argument to that render.
type extrasInstallClient struct {
	tb        testing.TB
	cache     *helm.ChartCache
	namespace string
}

func (c *extrasInstallClient) RenderManifestsWithPrep(
	ctx context.Context,
	resolved *spec.ResolvedSpec,
	postRenderer postrenderer.PostRenderer,
	crds []extras.RawFile,
) (*render.RenderResult, helm.ReleasePrep, func(), error) {
	noop := func() {}
	chartPath, err := helm.PrepareChart(ctx, resolved, c.cache, crds)
	if err != nil {
		return nil, helm.ReleasePrep{}, noop, fmt.Errorf("prepare chart: %w", err)
	}
	cleanup := func() {
		if removeErr := os.RemoveAll(chartPath); removeErr != nil {
			c.tb.Errorf("remove chart dir %s: %v", chartPath, removeErr)
		}
	}

	chart, err := loader.Load(chartPath)
	if err != nil {
		return nil, helm.ReleasePrep{}, cleanup, fmt.Errorf("load chart: %w", err)
	}
	releaseName := helm.GenerateReleaseName(resolved.Spec.Project, resolved.Env.Original)
	install := action.NewInstall(action.NewConfiguration())
	install.ReleaseName = releaseName
	install.Namespace = c.namespace
	install.DryRunStrategy = action.DryRunClient
	install.DisableOpenAPIValidation = true
	install.PostRenderer = postRenderer

	rel, err := install.RunWithContext(ctx, chart, map[string]any{})
	if err != nil {
		return nil, helm.ReleasePrep{}, cleanup, fmt.Errorf("helm install dry run: %w", err)
	}
	v1rel, ok := rel.(*v1.Release)
	if !ok {
		return nil, helm.ReleasePrep{}, cleanup, fmt.Errorf("unexpected helm release type %T", rel)
	}
	result := &render.RenderResult{
		ReleaseName: releaseName,
		Namespace:   install.Namespace,
		Manifest:    v1rel.Manifest,
		Hooks:       v1rel.Hooks,
		IsUpgrade:   false,
		Revision:    1,
		ChartPath:   chartPath,
	}
	prep := helm.ReleasePrep{
		Operation:    helm.OperationInstall,
		NextRevision: 1,
	}
	return result, prep, cleanup, nil
}

func writeExtrasPlanFile(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}

const extrasPlanSpec = `apiVersion: v1-alpha.5
project: plan-extras-fresh-install
components:
  web:
    image: nginx:latest
    port: 8080
    environments: [dev]
    resourcePreset: small
environments:
  dev: {}
`

const extrasPlanNetworkPolicy = `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: plan-extras-deny
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: web
  policyTypes:
    - Ingress
`
