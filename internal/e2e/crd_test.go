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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/e2e-framework/klient/wait"

	"deployah.dev/deployah/internal/spec"

	inttest "deployah.dev/deployah/internal/testing"
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
// Failures in this test should point here, not at the suite caller.
//
//nolint:thelper
func runCRDLifecycle(t *testing.T, env *e2eEnv) {
	src := filepath.Join(env.scenariosDir, "crd-lifecycle")
	require.DirExists(t, src)

	dir, err := inttest.NewScenarioWorkspace(t, src)
	require.NoError(t, err)

	restCfg := kubeRESTConfig(t, env.kcPath, kindContext)
	ext := newApiextensionsClient(t, restCfg)
	dyn := newDynamicClient(t, restCfg)

	ns := fixtureNamespace("crd-lifecycle")
	var crdUID types.UID
	claim, err := env.claimNamespace(t.Context(), ns)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), namespaceWaitTimeout)
		defer cancel()
		var crdErr error
		if crdUID != "" {
			crdErr = deleteOwnedCRD(ctx, ext, crdLifecycleName, crdUID)
		}
		nsErr := env.cleanupClaim(t, dir, claim, []releaseTarget{{
			Project: "crd-lifecycle", Env: "dev",
		}})
		if joined := errors.Join(crdErr, nsErr); joined != nil {
			t.Errorf("cleanup: %v", joined)
		}
	})
	require.NoError(t, requireCRDAbsent(t.Context(), ext, crdLifecycleName))

	widget := hideExtrasManifest(t, dir, "widget.yaml")
	runIn(t, dir, "deploy", "dev", "--context", kindContext,
		"--namespace", ns)
	crd := waitCRDEstablished(t, ext, crdLifecycleName, &crdUID)
	require.NotEmpty(t, crdUID)
	assertCRDUserMetadata(t, crd)

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, ".deployah", "manifests", "widget.yaml"),
		[]byte(widget), 0o600))
	runIn(t, dir, "deploy", "dev", "--context", kindContext,
		"--namespace", ns)
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
	runIn(t, dir, "deploy", "dev", "--context", kindContext,
		"--namespace", ns)
	crd = getCRD(t, ext, crdLifecycleName)
	assert.Equal(t, "crd-lifecycle", crd.Labels["e2e-marker"],
		"Helm upgrade must not rewrite chart CRDs")
	assertCRDUserMetadata(t, crd)

	runIn(t, dir, "delete", "crd-lifecycle", "dev",
		"--yes", "--wait", "--allow-missing-platform",
		"--context", kindContext, "--namespace", ns)
	_, err = ext.ApiextensionsV1().CustomResourceDefinitions().Get(
		t.Context(), crdLifecycleName, metav1.GetOptions{})
	require.NoError(t, err, "CRD must survive deployah delete")
}

const crdAddedName = "addedwidgets.example.com"

// TestCRDNewlyAddedOnUpgrade checks that a CRD file added after the
// first install is not installed by an ordinary Helm Upgrade.
// Failures in this test should point here, not at the suite caller.
//
//nolint:thelper
func runCRDNewlyAddedOnUpgrade(t *testing.T, env *e2eEnv) {
	src := filepath.Join(env.scenariosDir, "crd-lifecycle")
	require.DirExists(t, src)

	dir, err := inttest.NewScenarioWorkspace(t, src)
	require.NoError(t, err)

	restCfg := kubeRESTConfig(t, env.kcPath, kindContext)
	ext := newApiextensionsClient(t, restCfg)

	ns := fixtureNamespace("crd-added")
	var crdUID types.UID
	claim, err := env.claimNamespace(t.Context(), ns)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), namespaceWaitTimeout)
		defer cancel()
		var crdErr error
		if crdUID != "" {
			crdErr = deleteOwnedCRD(ctx, ext, crdLifecycleName, crdUID)
		}
		nsErr := env.cleanupClaim(t, dir, claim, []releaseTarget{{
			Project: "crd-lifecycle", Env: "dev",
		}})
		if joined := errors.Join(crdErr, nsErr); joined != nil {
			t.Errorf("cleanup: %v", joined)
		}
	})
	require.NoError(t, requireCRDAbsent(t.Context(), ext, crdLifecycleName))
	require.NoError(t, requireCRDAbsent(t.Context(), ext, crdAddedName))

	hideExtrasManifest(t, dir, "widget.yaml")
	runIn(t, dir, "deploy", "dev", "--context", kindContext,
		"--namespace", ns)
	waitCRDEstablished(t, ext, crdLifecycleName, &crdUID)
	require.NotEmpty(t, crdUID)
	assertReleaseExists(t, dir, ns, "crd-lifecycle", "dev")

	_, sourceFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	added, err := os.ReadFile(filepath.Join(filepath.Dir(sourceFile), "testdata", "added-widget-crd.yaml")) // #nosec G304 -- package testdata
	require.NoError(t, err)
	require.NoError(t, os.WriteFile( // #nosec G703 -- path under the copied workspace
		filepath.Join(dir, ".deployah", "crds", "addedwidget.yaml"),
		added, 0o600))
	runIn(t, dir, "deploy", "dev", "--context", kindContext,
		"--namespace", ns)

	_, err = ext.ApiextensionsV1().CustomResourceDefinitions().Get(
		t.Context(), crdAddedName, metav1.GetOptions{})
	require.True(t, apierrors.IsNotFound(err),
		"Helm upgrade must not install a CRD added after first install, got: %v", err)
}

func requireCRDAbsent(ctx context.Context, ext apiextensionsclient.Interface, name string) error {
	_, err := ext.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("crd %s: ownership uncertain: %w", name, err)
	}
	return fmt.Errorf("crd %s already exists, not owned by this run", name)
}

func deleteOwnedCRD(ctx context.Context, ext apiextensionsclient.Interface, name string, uid types.UID) error {
	if uid == "" {
		return fmt.Errorf("crd %s: ownership uncertain, not deleting", name)
	}
	err := ext.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, name, metav1.DeleteOptions{
		Preconditions: &metav1.Preconditions{UID: &uid},
	})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
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

// hideExtrasManifest removes a custom manifest so the first install can
// process chart CRDs. Deployah does not read .deployah/crds/ to learn a
// custom resource type, so an unknown kind cannot load until that API
// exists on the cluster.
func hideExtrasManifest(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, spec.DeployahConfigDir, spec.ManifestsDir, name)
	raw := readFixtureFile(t, path)
	require.NoError(t, os.Remove(path))
	return raw
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

func waitCRDEstablished(tb testing.TB, ext apiextensionsclient.Interface, name string, uid *types.UID) *apiextensionsv1.CustomResourceDefinition {
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
		if uid != nil && *uid == "" && crd.UID != "" {
			*uid = crd.UID
		}
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
