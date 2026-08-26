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
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/e2e-framework/klient/wait"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const crdLifecycleName = "clusterwidgets.example.com"

// TestCRDLifecycle checks CRD apply outside the Helm release.
// It waits for Established, re-applies with no Helm changes, compares
// create to create-replace, and checks the CRD survives deployah delete.
// File mutation cannot be expressed in e2e.yaml.
func (s *E2ESuite) TestCRDLifecycle() {
	t := s.T()
	src := filepath.Join(s.scenariosDir, "crd-lifecycle")
	require.DirExists(t, src)

	dir := t.TempDir()
	copyTree(t, src, dir)

	ns := fixtureNamespace("crd-lifecycle")
	s.createNamespace(t, ns)

	ext := newApiextensionsClient(t, s.kcPath, kindContext)
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
		"--namespace", ns, "--crds", "create")
	crd := waitCRDEstablished(t, ext, crdLifecycleName)
	assert.Equal(t, "crd-lifecycle", crd.Labels["e2e-marker"])

	_, stderr, err := runInErr(t, dir, "deploy", "dev", "--context", kindContext,
		"--yes", "--namespace", ns, "--crds", "create")
	require.NoError(t, err)
	assert.Contains(t, stderr, "already present")
	waitCRDEstablished(t, ext, crdLifecycleName)

	patched := strings.Replace(
		readFixtureFile(t, filepath.Join(dir, ".deployah", "crds", "clusterwidget.yaml")),
		`e2e-marker: "crd-lifecycle"`,
		`e2e-marker: "create-skipped"`,
		1,
	)
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, ".deployah", "crds", "clusterwidget.yaml"),
		[]byte(patched), 0o600))
	runIn(t, dir, "deploy", "dev", "--context", kindContext, "--yes",
		"--namespace", ns, "--crds", "create")
	crd = getCRD(t, ext, crdLifecycleName)
	assert.Equal(t, "crd-lifecycle", crd.Labels["e2e-marker"],
		"--crds create must not replace an existing CRD")

	runIn(t, dir, "deploy", "dev", "--context", kindContext, "--yes",
		"--namespace", ns, "--crds", "create-replace")
	require.NoError(t, wait.For(func(ctx context.Context) (bool, error) {
		live, getErr := ext.ApiextensionsV1().CustomResourceDefinitions().Get(
			ctx, crdLifecycleName, metav1.GetOptions{})
		if getErr != nil {
			if isRetryableAPIError(getErr) {
				return false, nil
			}
			return false, getErr
		}
		return live.Labels["e2e-marker"] == "create-skipped", nil
	}, wait.WithTimeout(2*time.Minute), wait.WithInterval(time.Second),
		wait.WithContext(t.Context()), wait.WithImmediate()))

	runIn(t, dir, "delete", "crd-lifecycle", "dev",
		"--yes", "--wait", "--allow-missing-platform",
		"--context", kindContext, "--namespace", ns)
	_, err = ext.ApiextensionsV1().CustomResourceDefinitions().Get(
		t.Context(), crdLifecycleName, metav1.GetOptions{})
	require.NoError(t, err, "CRD must survive deployah delete")
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

func newApiextensionsClient(tb testing.TB, kubeconfigPath, contextName string) apiextensionsclient.Interface {
	tb.Helper()
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = kubeconfigPath
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	restCfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules, overrides).ClientConfig()
	require.NoError(tb, err)
	cs, err := apiextensionsclient.NewForConfig(restCfg)
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
