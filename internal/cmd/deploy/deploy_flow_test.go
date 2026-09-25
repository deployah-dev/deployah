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

package deploy

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/release/common"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/session"
	"deployah.dev/deployah/internal/spec"
	"deployah.dev/deployah/internal/target"
	"deployah.dev/deployah/internal/testing/nabatctx"

	planengine "deployah.dev/deployah/internal/plan"
	v1 "helm.sh/helm/v4/pkg/release/v1"
	corev1 "k8s.io/api/core/v1"
)

// newClusterWithStub builds a Session and Cluster whose Kubernetes client is
// k8sClient (or errors if k8sClient is nil) and whose Helm client is stub.
func newClusterWithStub(t *testing.T, stub *stubHelmClient, k8sClient kubernetes.Interface) (*session.Session, *session.Cluster) {
	t.Helper()

	sess := session.New(
		session.WithHelmFactory(func(*target.Target, session.HelmConfig) (session.HelmClient, error) {
			return stub, nil
		}),
		session.WithKubernetesFactory(func(*target.Target) (kubernetes.Interface, error) {
			if k8sClient == nil {
				return nil, assertNever{}
			}
			return k8sClient, nil
		}),
	)
	cluster, err := sess.Target(t.Context(), "production")
	require.NoError(t, err)
	return sess, cluster
}

// assertNever is a placeholder error used by newClusterWithStub when a test
// does not need a Kubernetes client; any attempt to use it fails loudly
// through the returned error rather than a nil-pointer panic.
type assertNever struct{}

func (assertNever) Error() string { return "kubernetes client not configured for this test" }

func testRenderResult(manifest string) *render.RenderResult {
	return &render.RenderResult{
		ReleaseName: "web-production",
		Namespace:   "default",
		Manifest:    manifest,
		Revision:    1,
	}
}

const deployFlowManifestV1 = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 2
`

const deployFlowManifestV2 = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 3
`

// TestConfirmApply covers --yes and the non-interactive refusal path.
func TestConfirmApply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		opts        *Options
		wantProceed bool
		wantErrIs   error
		wantHint    string
	}{
		{
			name:        "yes skips prompt",
			opts:        &Options{Yes: true},
			wantProceed: true,
		},
		{
			name:      "non-interactive without yes refuses",
			opts:      &Options{Yes: false},
			wantErrIs: nabat.ErrConfirmationRequired,
			wantHint:  "--yes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := nabatctx.New(t, "test")
			proceed, err := confirmApply(h.Context, tt.opts, "Apply these changes?")
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				assert.False(t, proceed)
				var ce *nabat.ConfirmationError
				require.ErrorAs(t, err, &ce)
				assert.Equal(t, tt.wantHint, ce.BypassHint)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantProceed, proceed)
		})
	}
}

// TestApplyDeploy_RenderMismatch_AbortsBeforeApply verifies that when the
// chart renders differently the second time (a non-deterministic template),
// applyDeploy aborts with a clear error and never calls InstallApp.
func TestApplyDeploy_RenderMismatch_AbortsBeforeApply(t *testing.T) {
	t.Parallel()

	stub := &stubHelmClient{
		renderResults: []*render.RenderResult{
			testRenderResult(deployFlowManifestV1), // the apply-time re-render
		},
		installErr: nil, // would only matter if InstallApp were (wrongly) called
	}
	sess, cluster := newClusterWithStub(t, stub, nil)

	planned := &deployPlan{
		diff:    &planengine.Plan{},
		result:  testRenderResult(deployFlowManifestV2), // differs from the re-render above
		cleanup: func() {},
	}

	h := nabatctx.New(t, "test")
	opts := &Options{Environment: "production"}
	manifest := &spec.Spec{Project: "web"}

	err := applyDeploy(h.Context, sess, cluster, stub, nil, manifest, opts, nil, planned, nil, nil, &extras.Bundle{}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed between plan and apply")
	assert.Equal(t, 1, stub.renderCallCount, "must re-render exactly once before comparing")
}

// TestSkipDeploy_NoChanges_ShowsReadinessSummary verifies that skipping a
// no-op deploy still reports current pod readiness for the release, without
// calling Helm at all.
func TestSkipDeploy_NoChanges_ShowsReadinessSummary(t *testing.T) {
	t.Parallel()
	k8sClient := fake.NewSimpleClientset(
		&corev1.Pod{
			Name: "web-1", Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/instance":  "web-production",
				"app.kubernetes.io/component": "web",
			},
			Status: corev1.PodStatus{
				Phase:      corev1.PodRunning,
				Conditions: []corev1.PodCondition{},
				ContainerStatuses: []corev1.ContainerStatus{
					{Ready: true},
				},
			},
		},
	)
	h := nabatctx.New(t, "test")
	plan := &deployPlan{
		diff: &planengine.Plan{
			Header: planengine.Header{Release: "web-production", Revision: 7},
		},
		result:  testRenderResult(deployFlowManifestV1),
		cleanup: func() {},
	}

	err := skipDeploy(h.Context, k8sClient, nil, plan)
	require.NoError(t, err)
	assert.Contains(t, h.Stderr.String(), "No changes. Release web-production unchanged (revision 7).")
	assert.Contains(t, h.Stdout.String(), "Readiness:")
	assert.Contains(t, h.Stdout.String(), "web: 1/1")
}

// TestSkipHelmApply locks the idle gate: upgrades with no rendered
// changes skip Helm unless --reapply is set. Fresh installs never skip,
// even when the ordinary Manifest is empty. --skip-crds is not a trigger.
func TestSkipHelmApply(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		isUpgrade  bool
		hasChanges bool
		reapply    bool
		want       bool
	}{
		{name: "fresh install empty manifest", isUpgrade: false, hasChanges: false, want: false},
		{name: "fresh install with changes", isUpgrade: false, hasChanges: true, want: false},
		{name: "idle upgrade", isUpgrade: true, hasChanges: false, want: true},
		{name: "idle upgrade reapply", isUpgrade: true, hasChanges: false, reapply: true, want: false},
		{name: "upgrade with changes", isUpgrade: true, hasChanges: true, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, skipHelmApply(tt.isUpgrade, tt.hasChanges, tt.reapply))
		})
	}
}

// TestDeployPlan_ChartCRDsMatchSkipCRDs keeps the displayed plan aligned
// with InstallApp skipCRDs: process on a fresh install, skip when requested,
// and never treat chart CRDs as actionable on upgrade.
func TestDeployPlan_ChartCRDsMatchSkipCRDs(t *testing.T) {
	t.Parallel()
	yamlBody := "kind: CustomResourceDefinition\nmetadata:\n  name: widgets.example.com\n"
	docs := []extras.CRDDoc{{
		Path: "widget.yaml",
		Kind: "CustomResourceDefinition",
		Name: "widgets.example.com",
		YAML: []byte(yamlBody),
	}}
	tests := []struct {
		name        string
		upgrade     bool
		skipCRDs    bool
		wantProcess bool
		wantNote    string
		notContains []string
		wantIdle    bool
	}{
		{
			name:        "fresh default processes",
			wantProcess: true,
			wantNote:    "Helm install will process this chart CRD",
			notContains: []string{"install-time CRD processing disabled"},
		},
		{
			name:        "fresh skip",
			skipCRDs:    true,
			wantNote:    "install-time CRD processing disabled",
			notContains: []string{"+ CustomResourceDefinition/", "Helm install will process this chart CRD"},
		},
		{
			name:        "upgrade ignores skip flag for lifecycle",
			upgrade:     true,
			skipCRDs:    true,
			wantNote:    "Helm upgrade will not process this chart CRD",
			notContains: []string{"+ CustomResourceDefinition/", "install-time CRD processing disabled"},
			wantIdle:    true,
		},
		{
			name:        "upgrade without skip is not actionable",
			upgrade:     true,
			wantNote:    "Helm upgrade will not process this chart CRD",
			notContains: []string{"+ CustomResourceDefinition/"},
			wantIdle:    true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := &planengine.Plan{}
			planengine.StampChartCRDs(p, docs, tc.upgrade, tc.skipCRDs)
			require.Len(t, p.ChartCRDs, 1)
			assert.Equal(t, tc.wantProcess, p.ChartCRDs[0].WillProcess)
			assert.Equal(t, tc.wantIdle, skipHelmApply(tc.upgrade, p.HasChanges(), false))
			var buf strings.Builder
			require.NoError(t, planengine.RenderText(&buf, p, planengine.TextOptions{}))
			got := buf.String()
			assert.Contains(t, got, "CustomResourceDefinition/widgets.example.com")
			assert.Contains(t, got, tc.wantNote)
			for _, s := range tc.notContains {
				assert.NotContains(t, got, s)
			}
		})
	}
}

// TestApplyDeploy_PassesCRDsToInstall copies chart CRD files into Helm and
// maps Options.SkipCRDs onto Install.SkipCRDs.
func TestApplyDeploy_PassesCRDsToInstall(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		skipCRDs   bool
		wantStderr []string
	}{
		{name: "skip false", wantStderr: []string{"Deployed"}},
		{name: "skip true", skipCRDs: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			manifest := deployFlowManifestV1
			stub := &stubHelmClient{
				renderResults: []*render.RenderResult{testRenderResult(manifest)},
			}
			sess, cluster := newClusterWithStub(t, stub, nil)
			planned := &deployPlan{
				diff:    &planengine.Plan{Header: planengine.Header{Release: "web-production", Revision: 1}},
				result:  testRenderResult(manifest),
				cleanup: func() {},
			}
			h := nabatctx.New(t, "test")
			opts := &Options{Environment: "production", SkipCRDs: tc.skipCRDs}
			bundle := &extras.Bundle{CRDs: []extras.RawFile{{Path: "widget.yaml"}}}

			err := applyDeploy(h.Context, sess, cluster, stub, nil, &spec.Spec{Project: "web"}, opts, nil, planned, nil, assertNever{}, bundle, nil, nil)
			require.NoError(t, err)
			assert.Equal(t, 1, stub.installCallCount)
			assert.Equal(t, tc.skipCRDs, stub.lastSkipCRDs)
			require.Len(t, stub.lastCRDs, 1)
			for _, want := range tc.wantStderr {
				assert.Contains(t, h.Stderr.String(), want)
			}
		})
	}
}

// TestApplyDeploy_PropagatesInstallError returns Helm failures to the caller.
func TestApplyDeploy_PropagatesInstallError(t *testing.T) {
	t.Parallel()

	manifest := deployFlowManifestV1
	stub := &stubHelmClient{
		renderResults: []*render.RenderResult{testRenderResult(manifest)},
		installErr:    errors.New("helm boom"),
	}
	sess, cluster := newClusterWithStub(t, stub, nil)
	planned := &deployPlan{
		diff:    &planengine.Plan{Header: planengine.Header{Release: "web-production", Revision: 1}},
		result:  testRenderResult(manifest),
		cleanup: func() {},
	}
	h := nabatctx.New(t, "test")
	opts := &Options{Environment: "production"}

	err := applyDeploy(h.Context, sess, cluster, stub, nil, &spec.Spec{Project: "web"}, opts, nil, planned, nil, assertNever{}, &extras.Bundle{}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deploy failed")
	assert.Contains(t, err.Error(), "helm boom")
	assert.Equal(t, 1, stub.installCallCount)
}

// TestDeployFlags_SkipCRDsAcceptedCRDsRemoved checks the deploy CLI surface.
func TestDeployFlags_SkipCRDsAcceptedCRDsRemoved(t *testing.T) {
	t.Parallel()

	h := nabatctx.New(t, "deployah")
	Register(h.App)
	err := nabattest.Run(t, h.App, []string{"deploy", "--help"})
	require.NoError(t, err)
	help := h.Stdout.String() + h.Stderr.String()
	assert.Contains(t, help, "--skip-crds")
	assert.NotContains(t, help, "--crds")
	assert.NotContains(t, help, "create-replace")

	err = nabattest.Run(t, h.App, []string{"deploy", "prod", "--crds", "create"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "cluster")

	var opts Options
	assert.False(t, opts.SkipCRDs)
}

func TestComputePlan_HelmAction(t *testing.T) {
	t.Parallel()
	same := deployFlowManifestV1
	tests := []struct {
		name    string
		release *v1.Release
		result  *render.RenderResult
		want    semantic.HelmAction
	}{
		{
			name:   "fresh install",
			result: testRenderResult(same),
			want:   semantic.HelmInstall,
		},
		{
			name:    "unchanged upgrade",
			release: deployedRelease(same),
			result:  upgradeRenderResult(same, same),
			want:    semantic.HelmNone,
		},
		{
			name:    "changed upgrade",
			release: deployedRelease(deployFlowManifestV1),
			result:  upgradeRenderResult(deployFlowManifestV1, deployFlowManifestV2),
			want:    semantic.HelmUpgrade,
		},
		{
			name:    "previous wins over last successful release",
			release: deployedRelease(deployFlowManifestV1),
			result:  upgradeRenderResult(deployFlowManifestV2, deployFlowManifestV2),
			want:    semantic.HelmNone,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := &stubHelmClient{release: tt.release, renderResults: []*render.RenderResult{tt.result}}
			_, cluster := newClusterWithStub(t, stub, nil)
			h := nabatctx.New(t, "test")
			plan, err := computePlan(h.Context, stub, cluster, deployResolved(), nil, nil)
			require.NoError(t, err)
			t.Cleanup(plan.cleanup)
			assert.Equal(t, tt.want, plan.helmAction)
			assert.Same(t, tt.result, plan.result)
		})
	}
}

// TestComputePlan_LegacyDiffIgnoresArgsReorder documents the transitional
// divergence: dyff ignores a pure argument reorder, while the semantic
// action is an upgrade. The execution gate still uses the legacy diff.
func TestComputePlan_LegacyDiffIgnoresArgsReorder(t *testing.T) {
	t.Parallel()
	previous := deploymentArgs("--foo", "--bar")
	desired := deploymentArgs("--bar", "--foo")
	stub := &stubHelmClient{
		release:       deployedRelease(previous),
		renderResults: []*render.RenderResult{upgradeRenderResult(previous, desired)},
	}
	_, cluster := newClusterWithStub(t, stub, nil)
	h := nabatctx.New(t, "test")
	plan, err := computePlan(h.Context, stub, cluster, deployResolved(), nil, nil)
	require.NoError(t, err)
	t.Cleanup(plan.cleanup)
	assert.False(t, plan.diff.HasChanges())
	assert.Equal(t, semantic.HelmUpgrade, plan.helmAction)
}

func TestComputePlan_UpgradeWithoutPrevious(t *testing.T) {
	t.Parallel()
	result := testRenderResult(deployFlowManifestV1)
	result.IsUpgrade = true
	stub := &stubHelmClient{
		release:       deployedRelease(deployFlowManifestV1),
		renderResults: []*render.RenderResult{result},
	}
	_, cluster := newClusterWithStub(t, stub, nil)
	h := nabatctx.New(t, "test")
	plan, err := computePlan(h.Context, stub, cluster, deployResolved(), nil, nil)
	require.Error(t, err)
	assert.Nil(t, plan)
	assert.ErrorContains(t, err, "determine helm release intent")
	assert.Equal(t, 1, stub.cleanupCalls)
}

func deployResolved() *spec.ResolvedSpec {
	return &spec.ResolvedSpec{
		Spec: &spec.Spec{Project: "web"},
		Env:  spec.EnvIdentity{Original: "production"},
	}
}

func deployedRelease(manifest string) *v1.Release {
	return &v1.Release{
		Version:  2,
		Manifest: manifest,
		Info:     &v1.Info{Status: common.StatusDeployed},
	}
}

func upgradeRenderResult(previous, desired string) *render.RenderResult {
	result := testRenderResult(desired)
	result.IsUpgrade = true
	result.Revision = 3
	result.Previous = &render.ReleaseIntent{Manifest: previous}
	return result
}

func deploymentArgs(first, second string) string {
	return "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n  namespace: default\nspec:\n  template:\n    spec:\n      containers:\n        - name: api\n          args:\n            - " + first + "\n            - " + second + "\n"
}
