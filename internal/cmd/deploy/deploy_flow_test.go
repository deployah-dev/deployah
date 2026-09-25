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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/release/common"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/session"
	"deployah.dev/deployah/internal/spec"
	"deployah.dev/deployah/internal/target"
	"deployah.dev/deployah/internal/testing/nabatctx"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	v1 "helm.sh/helm/v4/pkg/release/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	clienttesting "k8s.io/client-go/testing"
)

// newClusterWithStub builds a Session and Cluster whose Kubernetes client is
// k8sClient (or errors if k8sClient is nil) and whose Helm client is stub.
func newClusterWithStub(t *testing.T, stub *stubHelmClient, k8sClient kubernetes.Interface) (*session.Session, *session.Cluster) {
	t.Helper()

	sess := session.New(
		session.WithNamespace("default"),
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

const deploySpec = `apiVersion: v1-alpha.5
project: web
components:
  web:
    image: nginx:latest
    port: 80
environments:
  production: {}
`

const deploySpecResize = `apiVersion: v1-alpha.5
project: web
components:
  web:
    image: nginx:latest
    port: 80
    persistence:
      size: 2Gi
      mountPath: /data
environments:
  production: {}
`

func writeDeploySpec(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "deployah.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func releaseWithComponent(component map[string]any) *v1.Release {
	return &v1.Release{
		Name:     "web-production",
		Version:  1,
		Manifest: deployFlowManifestV1,
		Info:     &v1.Info{Status: common.StatusDeployed},
		Chart: &chart.Chart{
			Values: map[string]any{
				"deployah": map[string]any{
					"resolved": map[string]any{
						"components": map[string]any{"web": component},
					},
				},
			},
		},
	}
}

func runDeployCommand(t *testing.T, specBody string, stub *stubHelmClient) (stdout, stderr string, err error) {
	t.Helper()
	specPath := writeDeploySpec(t, specBody)
	sess := session.New(
		session.WithSpecPath(specPath),
		session.WithHelmFactory(func(*target.Target, session.HelmConfig) (session.HelmClient, error) {
			return stub, nil
		}),
		session.WithKubernetesFactory(func(*target.Target) (kubernetes.Interface, error) {
			return nil, assertNever{}
		}),
	)
	h := nabatctx.New(t, "deployah")
	Register(h.App)
	require.NoError(t, h.App.OnPreRun(func(c *nabat.Context) error {
		c.SetContext(session.WithContext(c.Context(), sess))
		return nil
	}))
	err = nabattest.Run(t, h.App, []string{"deploy", "production"})
	return h.Stdout.String(), h.Stderr.String(), err
}

// TestRunDeploy_ExecutesHelm proves a fresh release and an unchanged
// existing release both invoke Helm once, with no render, plan, or prompt.
func TestRunDeploy_ExecutesHelm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		stub *stubHelmClient
	}{
		{
			name: "fresh release",
			stub: &stubHelmClient{releaseErr: helm.ErrReleaseNotFound},
		},
		{
			name: "unchanged existing release",
			stub: &stubHelmClient{
				release:      releaseWithComponent(map[string]any{"workloadKind": "Deployment"}),
				renderResult: testRenderResult(deployFlowManifestV1),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, err := runDeployCommand(t, deploySpec, tt.stub)
			require.NoError(t, err)
			assert.NotErrorIs(t, err, nabat.ErrConfirmationRequired)
			assert.Equal(t, 1, tt.stub.installCallCount)
			assert.Equal(t, 0, tt.stub.renderCallCount)
			assert.NotContains(t, stdout, "Plan:")
			assert.NotContains(t, stdout, "No changes")
			assert.NotContains(t, stdout, "Apply these changes?")
			assert.Contains(t, stderr, "Deployed")
		})
	}
}

// TestRunDeploy_GuardsBlockBeforeHelm proves workload and resize guards
// stop the command before Helm or a render.
func TestRunDeploy_GuardsBlockBeforeHelm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		specBody string
		release  *v1.Release
		wantErr  string
	}{
		{
			name:     "kind change",
			specBody: deploySpec,
			release:  releaseWithComponent(map[string]any{"workloadKind": "StatefulSet"}),
			wantErr:  "kind change",
		},
		{
			name:     "resize without flag",
			specBody: deploySpecResize,
			release:  releaseWithComponent(map[string]any{"workloadKind": "Deployment", "persistenceSize": "1Gi"}),
			wantErr:  "requires --resize-volumes",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := &stubHelmClient{release: tt.release}
			_, _, err := runDeployCommand(t, tt.specBody, stub)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
			assert.Equal(t, 0, stub.installCallCount)
			assert.Equal(t, 0, stub.renderCallCount)
		})
	}
}

// TestApplyDeploy_PassesCRDsToInstall copies chart CRD files into Helm and
// maps Options.SkipCRDs onto Install.SkipCRDs. A deploy with no resize
// does not render first.
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
			stub := &stubHelmClient{}
			sess, cluster := newClusterWithStub(t, stub, nil)
			h := nabatctx.New(t, "test")
			opts := &Options{Environment: "production", SkipCRDs: tc.skipCRDs}
			bundle := &extras.Bundle{CRDs: []extras.RawFile{{Path: "widget.yaml"}}}

			err := applyDeploy(h.Context, sess, cluster, stub, nil, &spec.Spec{Project: "web"}, opts, nil, "web-production", nil, assertNever{}, bundle, nil, nil)
			require.NoError(t, err)
			assert.Equal(t, 1, stub.installCallCount)
			assert.Equal(t, 0, stub.renderCallCount)
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

	stub := &stubHelmClient{installErr: errors.New("helm boom")}
	sess, cluster := newClusterWithStub(t, stub, nil)
	h := nabatctx.New(t, "test")
	opts := &Options{Environment: "production"}

	err := applyDeploy(h.Context, sess, cluster, stub, nil, &spec.Spec{Project: "web"}, opts, nil, "web-production", nil, assertNever{}, &extras.Bundle{}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deploy failed")
	assert.Contains(t, err.Error(), "helm boom")
	assert.Equal(t, 1, stub.installCallCount)
	assert.Equal(t, 0, stub.renderCallCount)
}

// TestDeployFlags checks the deploy CLI surface.
func TestDeployFlags(t *testing.T) {
	t.Parallel()

	h := nabatctx.New(t, "deployah")
	Register(h.App)
	err := nabattest.Run(t, h.App, []string{"deploy", "--help"})
	require.NoError(t, err)
	help := h.Stdout.String() + h.Stderr.String()
	assert.Contains(t, help, "--skip-crds")
	assert.NotContains(t, help, "--crds")
	assert.NotContains(t, help, "create-replace")
	assert.NotContains(t, help, "--yes")
	assert.NotContains(t, help, "-y,")
	assert.NotContains(t, help, "--reapply")
	assert.NotContains(t, help, "confirmation")
	assert.NotContains(t, help, "Shows what would change")

	err = nabattest.Run(t, h.App, []string{"deploy", "prod", "--crds", "create"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "cluster")

	for _, args := range [][]string{
		{"deploy", "prod", "--yes"},
		{"deploy", "prod", "--reapply"},
	} {
		rejected := nabatctx.New(t, "deployah")
		Register(rejected.App)
		runErr := nabattest.Run(t, rejected.App, args)
		require.Error(t, runErr)
		assert.NotContains(t, runErr.Error(), "cluster")
	}

	var opts Options
	assert.False(t, opts.SkipCRDs)
}

// TestApplyDeploy_ResizePreflight proves a render failure stops before any
// volume mutation, and a successful preflight resizes before Helm.
func TestApplyDeploy_ResizePreflight(t *testing.T) {
	t.Parallel()

	resizes := []persistenceResize{{
		Component: "db", PreviousSize: "10Gi", NewSize: "20Gi",
		StorageClass: "fast-ssd", Stateful: true,
	}}

	t.Run("render failure", func(t *testing.T) {
		t.Parallel()
		log := &deployCallLog{}
		stub := &stubHelmClient{renderErr: errors.New("bad chart"), log: log}
		client := resizeClient(log)
		sess, cluster := newClusterWithStub(t, stub, client)
		h := nabatctx.New(t, "test")

		err := applyDeploy(h.Context, sess, cluster, stub, nil, &spec.Spec{Project: "shop"}, &Options{Environment: "production"}, nil, "shop-production", client, nil, &extras.Bundle{}, nil, resizes)
		require.Error(t, err)
		assert.ErrorContains(t, err, "helm preflight before volume resize")
		assert.Equal(t, []string{"render", "cleanup"}, log.snapshot())
		assert.Equal(t, 1, stub.cleanupCalls)
		assert.Equal(t, 0, stub.installCallCount)
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		log := &deployCallLog{}
		stub := &stubHelmClient{renderResult: testRenderResult(deployFlowManifestV1), log: log}
		client := resizeClient(log)
		sess, cluster := newClusterWithStub(t, stub, client)
		h := nabatctx.New(t, "test")

		err := applyDeploy(h.Context, sess, cluster, stub, nil, &spec.Spec{Project: "shop"}, &Options{Environment: "production"}, nil, "shop-production", client, nil, &extras.Bundle{}, nil, resizes)
		require.NoError(t, err)
		assert.Equal(t, []string{"render", "cleanup", "pvc-update", "sts-delete", "install"}, log.snapshot())
		assert.Equal(t, 1, stub.renderCallCount)
		assert.Equal(t, 1, stub.cleanupCalls)
		assert.Equal(t, 1, stub.installCallCount)
	})
}

func resizeClient(log *deployCallLog) *fake.Clientset {
	sc := &storagev1.StorageClass{
		Name:                 "fast-ssd",
		AllowVolumeExpansion: new(true),
	}
	sts := &appsv1.StatefulSet{
		Name:      "shop-production-db",
		Namespace: "default",
		Labels: map[string]string{
			"app.kubernetes.io/instance": "shop-production",
			spec.LabelComponent:          "db",
		},
	}
	qty10 := resource.MustParse("10Gi")
	pvc := &corev1.PersistentVolumeClaim{
		Name:      "data-shop-production-db-0",
		Namespace: "default",
		Labels: map[string]string{
			"app.kubernetes.io/instance": "shop-production",
			spec.LabelComponent:          "db",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: qty10},
			},
			StorageClassName: new("fast-ssd"),
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Capacity: corev1.ResourceList{corev1.ResourceStorage: qty10},
			Conditions: []corev1.PersistentVolumeClaimCondition{{
				Type:   corev1.PersistentVolumeClaimFileSystemResizePending,
				Status: corev1.ConditionTrue,
			}},
		},
	}
	pod := &corev1.Pod{
		Name:      "db-0",
		Namespace: "default",
		Labels: map[string]string{
			"app.kubernetes.io/instance": "shop-production",
			spec.LabelComponent:          "db",
		},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{Ready: true}},
		},
	}
	client := fake.NewSimpleClientset(sc, sts, pvc, pod)
	client.PrependReactor("update", "persistentvolumeclaims", func(clienttesting.Action) (bool, runtime.Object, error) {
		log.add("pvc-update")
		return false, nil, nil
	})
	client.PrependReactor("delete", "statefulsets", func(clienttesting.Action) (bool, runtime.Object, error) {
		log.add("sts-delete")
		return false, nil, nil
	})
	return client
}
