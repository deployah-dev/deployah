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
			proceed, err := confirmApply(c, tt.opts)
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

	err := applyDeploy(c, sess, cluster, stub, nil, manifest, opts, nil, planned, nil, nil)
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
