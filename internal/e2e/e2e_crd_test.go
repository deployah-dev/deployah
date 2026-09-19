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

//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/e2e-framework/klient/wait"

	"deployah.dev/deployah/internal/spec"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	crdLifecycleName   = "lifecyclewidgets.example.com"
	crdLifecycleWidget = "lifecycle-widget"
)

var crdLifecycleGVR = schema.GroupVersionResource{
	Group:    "example.com",
	Version:  "v1",
	Resource: "lifecyclewidgets",
}

// TestCRDLifecycle checks Helm-native chart CRD install, upgrade, and
// uninstall. File mutation cannot be expressed in e2e.yaml.
func (s *E2ESuite) TestCRDLifecycle() {
	t := s.T()
	src := filepath.Join(s.scenariosDir, "crd-lifecycle")
	require.DirExists(t, src)

	dir := t.TempDir()
	copyTree(t, src, dir)

	ns := fixtureNamespace("crd-lifecycle")
	s.createNamespace(t, ns)

	restCfg := kubeRESTConfig(t, s.kcPath, kindContext)
	ext := newApiextensionsClient(t, restCfg)
	dyn := newDynamicClient(t, restCfg)
	t.Cleanup(func() {
		// t.Context() is canceled before Cleanup; teardown needs its own.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if delCRDErr := ext.ApiextensionsV1().CustomResourceDefinitions().Delete(
			cleanupCtx, crdLifecycleName, metav1.DeleteOptions{}); delCRDErr != nil {
			t.Logf("cleanup CRD delete failed (non-fatal): %v", delCRDErr)
		}
		if _, _, delErr := runInErrContext(t, cleanupCtx, dir, "delete", "crd-lifecycle", "dev",
			"--yes", "--wait", "--allow-missing-platform",
			"--context", kindContext, "--namespace", ns); delErr != nil {
			t.Logf("cleanup delete failed (non-fatal): %v", delErr)
		}
		s.deleteNamespace(t, ns)
	})

	runIn(t, dir, "deploy", "dev", "--context", kindContext, "--yes",
		"--namespace", ns)
	crd := waitCRDEstablished(t, ext, crdLifecycleName)
	assertCRDUserMetadata(t, crd)
	waitClusterResource(t, dyn, crdLifecycleGVR, crdLifecycleWidget)

	patched := strings.Replace(
		readFixtureFile(t, filepath.Join(dir, ".deployah", "crds", "clusterwidget.yaml")),
		`e2e-marker: "crd-lifecycle"`,
		`e2e-marker: "upgrade-skipped"`,
		1,
	)
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, ".deployah", "crds", "clusterwidget.yaml"),
		[]byte(patched), 0o600))
	runIn(t, dir, "deploy", "dev", "--context", kindContext, "--yes",
		"--namespace", ns, "--reapply")
	crd = getCRD(t, ext, crdLifecycleName)
	assert.Equal(t, "crd-lifecycle", crd.Labels["e2e-marker"],
		"Helm upgrade must not rewrite chart CRDs")
	assertCRDUserMetadata(t, crd)

	runIn(t, dir, "delete", "crd-lifecycle", "dev",
		"--yes", "--wait", "--allow-missing-platform",
		"--context", kindContext, "--namespace", ns)
	_, err := ext.ApiextensionsV1().CustomResourceDefinitions().Get(
		t.Context(), crdLifecycleName, metav1.GetOptions{})
	require.NoError(t, err, "CRD must survive deployah delete")
}

const (
	crdAddedName = "addedwidgets.example.com"
	crdAddedYAML = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: addedwidgets.example.com
spec:
  group: example.com
  scope: Namespaced
  names:
    kind: AddedWidget
    plural: addedwidgets
    singular: addedwidget
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
`
)

// TestCRDNewlyAddedOnUpgrade checks that a CRD file added after the
// first install is not installed by an ordinary Helm Upgrade.
func (s *E2ESuite) TestCRDNewlyAddedOnUpgrade() {
	t := s.T()
	src := filepath.Join(s.scenariosDir, "crd-lifecycle")
	require.DirExists(t, src)

	dir := t.TempDir()
	copyTree(t, src, dir)

	ns := fixtureNamespace("crd-added")
	s.createNamespace(t, ns)

	restCfg := kubeRESTConfig(t, s.kcPath, kindContext)
	ext := newApiextensionsClient(t, restCfg)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		for _, name := range []string{crdLifecycleName, crdAddedName} {
			if delCRDErr := ext.ApiextensionsV1().CustomResourceDefinitions().Delete(
				cleanupCtx, name, metav1.DeleteOptions{}); delCRDErr != nil {
				t.Logf("cleanup CRD delete failed (non-fatal): %v", delCRDErr)
			}
		}
		if _, _, delErr := runInErrContext(t, cleanupCtx, dir, "delete", "crd-lifecycle", "dev",
			"--yes", "--wait", "--allow-missing-platform",
			"--context", kindContext, "--namespace", ns); delErr != nil {
			t.Logf("cleanup delete failed (non-fatal): %v", delErr)
		}
		s.deleteNamespace(t, ns)
	})

	runIn(t, dir, "deploy", "dev", "--context", kindContext, "--yes",
		"--namespace", ns)
	waitCRDEstablished(t, ext, crdLifecycleName)
	assertReleaseExists(t, dir, ns, "crd-lifecycle", "dev")

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, ".deployah", "crds", "addedwidget.yaml"),
		[]byte(crdAddedYAML), 0o600))
	runIn(t, dir, "deploy", "dev", "--context", kindContext, "--yes",
		"--namespace", ns, "--reapply")

	_, err := ext.ApiextensionsV1().CustomResourceDefinitions().Get(
		t.Context(), crdAddedName, metav1.GetOptions{})
	require.True(t, apierrors.IsNotFound(err),
		"Helm upgrade must not install a CRD added after first install, got: %v", err)
}

func assertReleaseExists(t *testing.T, dir, ns, project, env string) {
	t.Helper()
	stdout, _ := runIn(t, dir, "status", project,
		"--environment", env,
		"--output", "json",
		"--context", kindContext,
		"--namespace", ns)
	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &rows), stdout)
	require.NotEmpty(t, rows, stdout)
	require.NotEmpty(t, rows[0]["release"], stdout)
}

func assertCRDUserMetadata(t *testing.T, crd *apiextensionsv1.CustomResourceDefinition) {
	t.Helper()
	assert.Equal(t, "crd-lifecycle", crd.Labels["e2e-marker"])
	assert.Empty(t, crd.Labels[spec.LabelProject])
	assert.Empty(t, crd.Annotations[spec.AnnotationSource])
	assert.Empty(t, crd.Annotations[spec.AnnotationProject])
}

func copyTree(tb testing.TB, src, dst string) {
	tb.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		in, openErr := os.Open(path) // #nosec G304 G122 -- path under scenarios/
		if openErr != nil {
			return openErr
		}
		defer in.Close()                                                                 //nolint:errcheck // read-only copy helper
		out, createErr := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) // #nosec G304 -- temp fixture copy
		if createErr != nil {
			return createErr
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	require.NoError(tb, err)
}

func readFixtureFile(tb testing.TB, path string) string {
	tb.Helper()
	raw, err := os.ReadFile(path) // #nosec G304 -- path under test-controlled temp dir
	require.NoError(tb, err)
	return string(raw)
}

func kubeRESTConfig(tb testing.TB, kubeconfigPath, contextName string) *rest.Config {
	tb.Helper()
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = kubeconfigPath
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	restCfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules, overrides).ClientConfig()
	require.NoError(tb, err)
	return restCfg
}

func newApiextensionsClient(tb testing.TB, restCfg *rest.Config) apiextensionsclient.Interface {
	tb.Helper()
	cs, err := apiextensionsclient.NewForConfig(restCfg)
	require.NoError(tb, err)
	return cs
}

func newDynamicClient(tb testing.TB, restCfg *rest.Config) dynamic.Interface {
	tb.Helper()
	cs, err := dynamic.NewForConfig(restCfg)
	require.NoError(tb, err)
	return cs
}

func getCRD(tb testing.TB, ext apiextensionsclient.Interface, name string) *apiextensionsv1.CustomResourceDefinition {
	tb.Helper()
	crd, err := ext.ApiextensionsV1().CustomResourceDefinitions().Get(
		tb.Context(), name, metav1.GetOptions{})
	require.NoError(tb, err)
	return crd
}

func waitCRDEstablished(tb testing.TB, ext apiextensionsclient.Interface, name string) *apiextensionsv1.CustomResourceDefinition {
	tb.Helper()
	var latest *apiextensionsv1.CustomResourceDefinition
	require.NoError(tb, wait.For(func(ctx context.Context) (bool, error) {
		crd, err := ext.ApiextensionsV1().CustomResourceDefinitions().Get(
			ctx, name, metav1.GetOptions{})
		if err != nil {
			if isRetryableAPIError(err) {
				return false, nil
			}
			return false, err
		}
		latest = crd
		for _, cond := range crd.Status.Conditions {
			if cond.Type == apiextensionsv1.Established &&
				cond.Status == apiextensionsv1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	}, wait.WithTimeout(2*time.Minute), wait.WithInterval(time.Second),
		wait.WithContext(tb.Context()), wait.WithImmediate()))
	require.NotNil(tb, latest)
	return latest
}

func waitClusterResource(tb testing.TB, dyn dynamic.Interface, gvr schema.GroupVersionResource, name string) {
	tb.Helper()
	require.NoError(tb, wait.For(func(ctx context.Context) (bool, error) {
		_, err := dyn.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) || isRetryableAPIError(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}, wait.WithTimeout(2*time.Minute), wait.WithInterval(time.Second),
		wait.WithContext(tb.Context()), wait.WithImmediate()))
}
