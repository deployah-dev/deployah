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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/session"
	"deployah.dev/deployah/internal/spec"
	"deployah.dev/deployah/internal/target"

	planengine "deployah.dev/deployah/internal/plan"
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
			c := nabatContext(t) // nabattest.NewIO reports non-TTY by default
			proceed, err := confirmApply(c, tt.opts, "Apply these changes?")
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

	c := nabatContext(t)
	opts := &Options{Environment: "production"}
	manifest := &spec.Spec{Project: "web"}

	err := applyDeploy(c, sess, cluster, stub, nil, manifest, opts, nil, planned, nil, nil, &extras.Bundle{}, nil, nil)
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

// TestApplyDeploy_PassesCRDsToInstall forwards loaded CRDs to Helm
// install and maps Options.SkipCRDs onto Install.SkipCRDs.
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
			c, _, _, stderr := nabatContextWithIO(t)
			opts := &Options{Environment: "production", SkipCRDs: tc.skipCRDs}
			bundle := &extras.Bundle{CRDs: []extras.RawFile{{Path: "widget.yaml"}}}

			err := applyDeploy(c, sess, cluster, stub, nil, &spec.Spec{Project: "web"}, opts, nil, planned, nil, assertNever{}, bundle, nil, nil)
			require.NoError(t, err)
			assert.Equal(t, 1, stub.installCallCount)
			assert.Equal(t, tc.skipCRDs, stub.lastSkipCRDs)
			require.Len(t, stub.lastCRDs, 1)
			for _, want := range tc.wantStderr {
				assert.Contains(t, stderr.String(), want)
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
	c := nabatContext(t)
	opts := &Options{Environment: "production"}

	err := applyDeploy(c, sess, cluster, stub, nil, &spec.Spec{Project: "web"}, opts, nil, planned, nil, assertNever{}, &extras.Bundle{}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deploy failed")
	assert.Contains(t, err.Error(), "helm boom")
	assert.Equal(t, 1, stub.installCallCount)
}

// TestDeployFlags_SkipCRDsAcceptedCRDsRemoved checks the deploy CLI surface.
func TestDeployFlags_SkipCRDsAcceptedCRDsRemoved(t *testing.T) {
	t.Parallel()

	io, _, out, errOut := nabattest.NewIO()
	app := nabat.MustNew("deployah", nabat.WithIO(io))
	Register(app)
	err := nabattest.Run(t, app, []string{"deploy", "--help"})
	require.NoError(t, err)
	help := out.String() + errOut.String()
	assert.Contains(t, help, "--skip-crds")
	assert.NotContains(t, help, "--crds")
	assert.NotContains(t, help, "create-replace")

	err = nabattest.Run(t, app, []string{"deploy", "prod", "--crds", "create"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "cluster")

	var opts Options
	assert.False(t, opts.SkipCRDs)
}
