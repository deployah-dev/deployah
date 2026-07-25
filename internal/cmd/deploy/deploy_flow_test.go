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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/k8s"
	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/session"
	"deployah.dev/deployah/internal/spec"

	planengine "deployah.dev/deployah/internal/plan"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// newClusterWithStub builds a [session.Cluster] whose Kubernetes client is
// k8sClient (or errors if k8sClient is nil) and whose Helm client is stub.
func newClusterWithStub(t *testing.T, stub *stubHelmClient, k8sClient kubernetes.Interface) *session.Cluster {
	t.Helper()
	sess := session.New(
		session.WithHelmFactory(func(*session.Session) (session.HelmClient, error) {
			return stub, nil
		}),
		session.WithKubernetesFactory(func(*session.Session) (kubernetes.Interface, error) {
			if k8sClient == nil {
				return nil, assertNever{}
			}
			return k8sClient, nil
		}),
	)
	cluster, err := sess.Target(t.Context(), "production")
	require.NoError(t, err)
	return cluster
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
		wantErr     bool
		errContains []string
	}{
		{
			name:        "yes skips prompt",
			opts:        &Options{Yes: true},
			wantProceed: true,
		},
		{
			name:        "non-interactive without yes refuses",
			opts:        &Options{Yes: false},
			wantErr:     true,
			errContains: []string{"refusing to deploy without confirmation", "--yes"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := nabatContext(t) // nabattest.NewIO reports non-TTY by default
			proceed, err := confirmApply(c, tt.opts, "Apply these changes?")
			if tt.wantErr {
				require.Error(t, err)
				assert.False(t, proceed)
				for _, s := range tt.errContains {
					assert.Contains(t, err.Error(), s)
				}
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
	cluster := newClusterWithStub(t, stub, nil)
	sess := cluster.Session

	planned := &deployPlan{
		diff:    &planengine.Plan{},
		result:  testRenderResult(deployFlowManifestV2), // differs from the re-render above
		cleanup: func() {},
	}

	c := nabatContext(t)
	opts := &Options{Environment: "production"}
	manifest := &spec.Spec{Project: "web"}

	err := applyDeploy(c, sess, cluster, stub, nil, manifest, opts, nil, planned, nil, nil, &extras.Bundle{}, nil)
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
			ObjectMeta: metav1.ObjectMeta{
				Name:      "web-1",
				Namespace: "default",
				Labels: map[string]string{
					"app.kubernetes.io/instance":  "web-production",
					"app.kubernetes.io/component": "web",
				},
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
	c, _, stdout, stderr := nabatContextWithIO(t)
	plan := &deployPlan{
		diff: &planengine.Plan{
			Header: planengine.Header{Release: "web-production", Revision: 7},
		},
		result:  testRenderResult(deployFlowManifestV1),
		cleanup: func() {},
	}

	err := skipDeploy(c, k8sClient, nil, plan)
	require.NoError(t, err)
	assert.Contains(t, stderr.String(), "No changes. Release web-production unchanged (revision 7).")
	assert.Contains(t, stdout.String(), "Readiness:")
	assert.Contains(t, stdout.String(), "web: 1/1")
}

// TestSkipWhenIdle locks the gate that used to skip CRD apply on a no-op
// Helm plan.
func TestSkipWhenIdle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		helmIdle bool
		crdCount int
		want     bool
	}{
		{name: "idle no CRDs", helmIdle: true, crdCount: 0, want: true},
		{name: "idle with CRDs", helmIdle: true, crdCount: 1, want: false},
		{name: "helm changes no CRDs", helmIdle: false, crdCount: 0, want: false},
		{name: "helm changes with CRDs", helmIdle: false, crdCount: 2, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, skipWhenIdle(tt.helmIdle, tt.crdCount))
		})
	}
}

// TestCRDIdleSuccessMessage covers wait-only vs created/replaced wording.
func TestCRDIdleSuccessMessage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		stats extras.CRDStats
		want  string
	}{
		{
			name:  "already present",
			stats: extras.CRDStats{Ready: 2},
			want:  "CRDs ready (2 already present). Release web-production unchanged (revision 3).",
		},
		{
			name:  "created and replaced",
			stats: extras.CRDStats{Created: 1, Replaced: 1, Ready: 2},
			want:  "Applied 2 CRDs (1 created, 1 replaced). Release web-production unchanged (revision 3).",
		},
		{
			name:  "one created",
			stats: extras.CRDStats{Created: 1, Ready: 1},
			want:  "Applied 1 CRD (1 created). Release web-production unchanged (revision 3).",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, crdIdleSuccessMessage(tt.stats, "web-production", 3))
		})
	}
}

// TestCRDApplySuffix covers the Helm-path success footnote for CRD writes.
func TestCRDApplySuffix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		stats extras.CRDStats
		want  string
	}{
		{name: "noop", stats: extras.CRDStats{Ready: 2}, want: ""},
		{name: "created", stats: extras.CRDStats{Created: 1, Ready: 1}, want: "; applied 1 CRD (1 created)"},
		{name: "both", stats: extras.CRDStats{Created: 1, Replaced: 2, Ready: 3}, want: "; applied 3 CRDs (1 created, 2 replaced)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, crdApplySuffix(tt.stats))
		})
	}
}

// TestFilterCoveredAPIs drops requirements satisfied by pending CRDs.
func TestFilterCoveredAPIs(t *testing.T) {
	t.Parallel()
	reqs := []k8s.APIRequirement{
		{GroupVersions: []string{"cert-manager.io/v1"}, Reason: "tls"},
		{GroupVersions: []string{"autoscaling/v2", "autoscaling/v2beta2"}, Reason: "hpa"},
	}
	covered := map[string]struct{}{"cert-manager.io/v1": {}}
	got := filterCoveredAPIs(reqs, covered)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"autoscaling/v2", "autoscaling/v2beta2"}, got[0].GroupVersions)
	assert.Equal(t, reqs, filterCoveredAPIs(reqs, nil))
}

// TestApplyCRDsOnly_ReportsSuccessAndReadiness covers the Helm-idle success
// message and readiness poll. applyBundleCRDs is a no-op for an empty CRD
// list (real CRD apply needs a live apiextensions API).
func TestApplyCRDsOnly_ReportsSuccessAndReadiness(t *testing.T) {
	t.Parallel()
	k8sClient := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "web-1",
				Namespace: "default",
				Labels: map[string]string{
					"app.kubernetes.io/instance":  "web-production",
					"app.kubernetes.io/component": "web",
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Ready: true},
				},
			},
		},
	)
	stub := &stubHelmClient{}
	cluster := newClusterWithStub(t, stub, k8sClient)
	sess := cluster.Session
	c, _, stdout, stderr := nabatContextWithIO(t)
	plan := &deployPlan{
		diff: &planengine.Plan{
			Header: planengine.Header{Release: "web-production", Revision: 3},
		},
		result:  testRenderResult(deployFlowManifestV1),
		cleanup: func() {},
	}
	opts := &Options{Environment: "production", CRDs: string(extras.PolicyCreate)}

	err := applyCRDsOnly(c, sess, cluster, k8sClient, nil, plan, &extras.Bundle{}, opts)
	require.NoError(t, err)
	assert.Contains(t, stderr.String(), "CRDs ready (0 already present). Release web-production unchanged (revision 3).")
	assert.Contains(t, stdout.String(), "Readiness:")
}
