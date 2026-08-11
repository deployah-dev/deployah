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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.yaml.in/yaml/v3"
	"k8s.io/client-go/tools/clientcmd"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"
	"sigs.k8s.io/e2e-framework/klient"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"

	"deployah.dev/deployah/internal/cmd"
	"deployah.dev/deployah/internal/localkube"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// E2ESuite drives Deployah against a live Kind cluster created by
// `deployah cluster up`.
type E2ESuite struct {
	suite.Suite
	kcPath      string
	client      klient.Client
	scenarios   []scenario
	testdataDir string // absolute; resolved before SetupSuite chdirs to a temp dir
	created     bool   // true once the suite attempts cluster up (teardown if partial)
}

type scenario struct {
	Name    string
	Dir     string // absolute
	Project string // from deployah.yaml, needed by the delete cleanup
}

// clusterStatusView mirrors the JSON tags on the unexported status view in
// internal/cmd/cluster/status.go.
type clusterStatusView struct {
	Name                 string `json:"name"`
	Status               string `json:"status"`
	Context              string `json:"context"`
	Kubeconfig           string `json:"kubeconfig"`
	CloudProviderRunning bool   `json:"cloudProviderRunning"`
}

type expectations struct {
	Env          string                `yaml:"env"`
	Namespace    string                `yaml:"namespace"`
	Deployments  []expectedDeployment  `yaml:"deployments"`
	StatefulSets []expectedStatefulSet `yaml:"statefulSets"`
	Services     []expectedService     `yaml:"services"`
	PVCs         []expectedPVC         `yaml:"pvcs"`
	Pods         expectedPods          `yaml:"pods"`
}

type expectedDeployment struct {
	Name     string            `yaml:"name"`
	Replicas int32             `yaml:"replicas"`
	Image    string            `yaml:"image"`
	PortName string            `yaml:"portName"`
	Labels   map[string]string `yaml:"labels"`
}

type expectedStatefulSet struct {
	Name     string            `yaml:"name"`
	Replicas int32             `yaml:"replicas"`
	Image    string            `yaml:"image"`
	PortName string            `yaml:"portName"`
	Labels   map[string]string `yaml:"labels"`
}

type expectedService struct {
	Name           string            `yaml:"name"`
	Port           int32             `yaml:"port"`
	TargetPortName string            `yaml:"targetPortName"`
	Selector       map[string]string `yaml:"selector"`
	// ClusterIP, when set to "None", asserts a headless Service.
	ClusterIP string `yaml:"clusterIP"`
}

type expectedPVC struct {
	NamePrefix string `yaml:"namePrefix"`
	MinCount   int    `yaml:"minCount"`
	Phase      string `yaml:"phase"`
	Storage    string `yaml:"storage"`
}

type expectedPods struct {
	LabelSelector string `yaml:"labelSelector"`
	MinCount      int    `yaml:"minCount"`
	Phase         string `yaml:"phase"`
}

// TestE2E runs the Kind-based end-to-end suite.
func TestE2E(t *testing.T) {
	suite.Run(t, new(E2ESuite))
}

// SetupSuite discovers fixtures, creates the Kind cluster, and checks status.
func (s *E2ESuite) SetupSuite() {
	t := s.T()
	requireEngine(t)

	// go test starts in the package directory. Resolve fixtures now, because
	// the chdir below moves the whole suite out of it.
	testdataDir, err := filepath.Abs("testdata")
	s.Require().NoError(err)
	s.testdataDir = testdataDir
	s.scenarios = discoverScenarios(t, testdataDir)

	// cluster up scaffolds deployah.platform.yaml into the cwd, so run from a
	// temp dir to keep the repo clean. t.Chdir restores cwd when the suite ends.
	t.Chdir(t.TempDir())

	requireNoCollision(t)
	// Mark before up so TearDownSuite cleans up if create succeeds but a
	// later step in cluster up fails. down --force is a no-op when missing.
	s.created = true
	run(t, "cluster", "up")

	raw := run(t, "cluster", "status", "--output", "json")
	var status clusterStatusView
	s.Require().NoError(json.Unmarshal([]byte(raw), &status))
	s.Require().Equal("deployah", status.Name)
	s.Require().Equal("running", status.Status)
	s.Require().Equal("kind-deployah", status.Context)
	s.Require().True(status.CloudProviderRunning)

	// status already carries the kubeconfig path, so no second CLI call.
	s.kcPath = status.Kubeconfig
	s.Require().FileExists(s.kcPath)

	s.client = newKlient(t, s.kcPath, "kind-deployah")
}

// TearDownSuite destroys the Kind cluster only when this suite created it.
func (s *E2ESuite) TearDownSuite() {
	if !s.created {
		return
	}
	if err := runErr(s.T(), "cluster", "down", "--force"); err != nil {
		s.T().Errorf("cluster down failed: %v", err)
	}
}

// TestStatefulScale deploys a stateful component at replicas 1, then upgrades
// to replicas 2 and asserts a second PVC is created.
func (s *E2ESuite) TestStatefulScale() {
	t := s.T()
	src := filepath.Join(s.testdataDir, "stateful-scale")
	require.DirExists(t, src)

	// Work in a temp copy so swapping deployah.yaml never dirties testdata/.
	dir := t.TempDir()
	for _, name := range []string{"deployah.yaml", "deployah-replicas-2.yaml"} {
		data, readErr := os.ReadFile(filepath.Join(src, name)) // #nosec G304 -- fixture under testdata
		require.NoError(t, readErr)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
	}

	t.Chdir(dir)
	t.Cleanup(func() {
		if delErr := runErr(t, "delete", "stateful-scale", "dev",
			"--yes", "--wait", "--allow-missing-platform",
			"--context", "kind-deployah"); delErr != nil {
			t.Logf("cleanup delete failed (non-fatal): %v", delErr)
		}
	})

	run(t, "deploy", "dev", "--context", "kind-deployah", "--yes")

	res := s.client.Resources("default")
	ctx := t.Context()
	stsName := "stateful-scale-dev-cache"

	require.NoError(t, wait.For(
		conditions.New(res).ResourceMatch(&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: stsName, Namespace: "default"},
		}, func(obj k8s.Object) bool {
			live, ok := obj.(*appsv1.StatefulSet)
			return ok && live.Status.ReadyReplicas >= 1
		}),
		wait.WithTimeout(5*time.Minute),
		wait.WithInterval(2*time.Second),
	))

	replicas2, readErr := os.ReadFile("deployah-replicas-2.yaml") // #nosec G304 -- temp fixture copy
	require.NoError(t, readErr)
	require.NoError(t, os.WriteFile("deployah.yaml", replicas2, 0o600))

	run(t, "deploy", "dev", "--context", "kind-deployah", "--yes")
	require.NoError(t, wait.For(
		conditions.New(res).ResourceMatch(&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: stsName, Namespace: "default"},
		}, func(obj k8s.Object) bool {
			live, ok := obj.(*appsv1.StatefulSet)
			return ok && live.Spec.Replicas != nil &&
				*live.Spec.Replicas == 2 && live.Status.ReadyReplicas >= 2
		}),
		wait.WithTimeout(5*time.Minute),
		wait.WithInterval(2*time.Second),
	))

	var pvcs corev1.PersistentVolumeClaimList
	require.NoError(t, res.List(ctx, &pvcs))
	matched := 0
	for _, pvc := range pvcs.Items {
		if strings.HasPrefix(pvc.Name, "data-stateful-scale-dev-cache-") {
			matched++
			assert.Equal(t, corev1.ClaimBound, pvc.Status.Phase, pvc.Name)
		}
	}
	assert.GreaterOrEqual(t, matched, 2, "expected per-pod PVCs after scale-up")
}

const crdLifecycleName = "clusterwidgets.example.com"

// TestCRDLifecycle covers CRD apply outside the Helm release: Established
// before install, idle-Helm re-apply, create vs create-replace, and survival
// across deployah delete.
func (s *E2ESuite) TestCRDLifecycle() {
	t := s.T()
	src := filepath.Join(s.testdataDir, "crd-lifecycle")
	require.DirExists(t, src)

	dir := t.TempDir()
	copyTree(t, src, dir)
	t.Chdir(dir)

	ext := newApiextensionsClient(t, s.kcPath, "kind-deployah")
	t.Cleanup(func() {
		// t.Context() is canceled just before Cleanup runs (Go 1.24+), so
		// teardown API calls need an independent context.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		// Best-effort: remove the fixture CRD so later suite runs stay clean.
		if delCRDErr := ext.ApiextensionsV1().CustomResourceDefinitions().Delete(
			cleanupCtx, crdLifecycleName, metav1.DeleteOptions{}); delCRDErr != nil {
			t.Logf("cleanup CRD delete failed (non-fatal): %v", delCRDErr)
		}
		if delErr := runErr(t, "delete", "crd-lifecycle", "dev",
			"--yes", "--wait", "--allow-missing-platform",
			"--context", "kind-deployah"); delErr != nil {
			t.Logf("cleanup delete failed (non-fatal): %v", delErr)
		}
	})

	// First deploy installs the CRD and the release.
	run(t, "deploy", "dev", "--context", "kind-deployah", "--yes", "--crds", "create")
	crd := waitCRDEstablished(t, ext, crdLifecycleName)
	assert.Equal(t, "crd-lifecycle", crd.Labels["e2e-marker"])

	// Idle Helm plan must still visit CRDs (already present). Success messages
	// go to stderr via nabat, so assert via a dedicated IO capture.
	_, stderr := runCapture(t, "deploy", "dev", "--context", "kind-deployah", "--yes", "--crds", "create")
	assert.Contains(t, stderr, "already present")
	waitCRDEstablished(t, ext, crdLifecycleName)

	// --crds create leaves an existing CRD alone when the file changes.
	patched := strings.Replace(
		readFixtureFile(t, filepath.Join(dir, ".deployah", "crds", "clusterwidget.yaml")),
		`e2e-marker: "crd-lifecycle"`,
		`e2e-marker: "create-skipped"`,
		1,
	)
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, ".deployah", "crds", "clusterwidget.yaml"),
		[]byte(patched), 0o600))
	run(t, "deploy", "dev", "--context", "kind-deployah", "--yes", "--crds", "create")
	crd = getCRD(t, ext, crdLifecycleName)
	assert.Equal(t, "crd-lifecycle", crd.Labels["e2e-marker"],
		"--crds create must not replace an existing CRD")

	// --crds create-replace server-side-applies over the existing CRD.
	run(t, "deploy", "dev", "--context", "kind-deployah", "--yes", "--crds", "create-replace")
	require.NoError(t, wait.For(func(ctx context.Context) (bool, error) {
		live, getErr := ext.ApiextensionsV1().CustomResourceDefinitions().Get(
			ctx, crdLifecycleName, metav1.GetOptions{})
		if getErr != nil {
			return false, getErr
		}
		return live.Labels["e2e-marker"] == "create-skipped", nil
	}, wait.WithTimeout(2*time.Minute), wait.WithInterval(time.Second)))

	// CRDs are never pruned on uninstall.
	run(t, "delete", "crd-lifecycle", "dev",
		"--yes", "--wait", "--allow-missing-platform",
		"--context", "kind-deployah")
	_, err := ext.ApiextensionsV1().CustomResourceDefinitions().Get(
		t.Context(), crdLifecycleName, metav1.GetOptions{})
	require.NoError(t, err, "CRD must survive deployah delete")
}

// TestDeployScenarios deploys each discovered fixture and asserts expect.yaml.
func (s *E2ESuite) TestDeployScenarios() {
	for _, sc := range s.scenarios {
		s.Run(sc.Name, func() {
			t := s.T()

			// Load before the chdir; sc.Dir is absolute so order is safe either
			// way, but reading first keeps the dependency obvious.
			exp := loadExpectations(t, sc.Dir)
			t.Chdir(sc.Dir) // deploy reads deployah.yaml from the cwd

			// Registered before deploy: a partial apply still gets torn down.
			t.Cleanup(func() {
				if err := runErr(t, "delete", sc.Project, exp.Env,
					"--yes", "--wait", "--allow-missing-platform",
					"--context", "kind-deployah"); err != nil {
					t.Logf("cleanup delete failed (non-fatal): %v", err)
				}
			})

			run(t, "deploy", exp.Env, "--context", "kind-deployah", "--yes")
			s.assertExpectations(t, exp)
		})
	}
}

func (s *E2ESuite) assertExpectations(t testing.TB, exp expectations) {
	t.Helper()
	res := s.client.Resources(exp.Namespace)
	ctx := t.Context()

	for _, dep := range exp.Deployments {
		target := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: dep.Name, Namespace: exp.Namespace},
		}

		// Spelled out rather than using the DeploymentAvailable shorthand, so
		// the condition under test is unambiguous in the source.
		err := wait.For(
			conditions.New(res).DeploymentConditionMatch(
				target, appsv1.DeploymentAvailable, corev1.ConditionTrue),
			wait.WithTimeout(5*time.Minute),
			wait.WithInterval(2*time.Second),
		)
		require.NoErrorf(t, err, "deployment %s/%s never became Available",
			exp.Namespace, dep.Name)

		var live appsv1.Deployment
		require.NoError(t, res.Get(ctx, dep.Name, exp.Namespace, &live))
		dumpActual(t, &live)

		for key, val := range dep.Labels { // subset match
			assert.Equalf(t, val, live.Labels[key],
				"deployment %s label %s", dep.Name, key)
		}

		containers := live.Spec.Template.Spec.Containers
		require.NotEmptyf(t, containers, "deployment %s has no containers", dep.Name)
		assert.Equalf(t, dep.Image, containers[0].Image,
			"deployment %s image", dep.Name)

		if dep.PortName != "" {
			require.NotEmptyf(t, containers[0].Ports,
				"deployment %s has no ports", dep.Name)
			assert.Equalf(t, dep.PortName, containers[0].Ports[0].Name,
				"deployment %s port name", dep.Name)
		}
		if dep.Replicas > 0 {
			require.NotNil(t, live.Spec.Replicas)
			assert.Equalf(t, dep.Replicas, *live.Spec.Replicas,
				"deployment %s replicas", dep.Name)
		}
	}

	for _, sts := range exp.StatefulSets {
		target := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: sts.Name, Namespace: exp.Namespace},
		}
		err := wait.For(
			conditions.New(res).ResourceMatch(target, func(obj k8s.Object) bool {
				live, ok := obj.(*appsv1.StatefulSet)
				if !ok || live.Spec.Replicas == nil {
					return false
				}
				return live.Status.ReadyReplicas >= *live.Spec.Replicas &&
					live.Status.ReadyReplicas > 0
			}),
			wait.WithTimeout(5*time.Minute),
			wait.WithInterval(2*time.Second),
		)
		require.NoErrorf(t, err, "statefulset %s/%s never became ready",
			exp.Namespace, sts.Name)

		var live appsv1.StatefulSet
		require.NoError(t, res.Get(ctx, sts.Name, exp.Namespace, &live))
		dumpActual(t, &live)

		for key, val := range sts.Labels {
			assert.Equalf(t, val, live.Labels[key],
				"statefulset %s label %s", sts.Name, key)
		}
		containers := live.Spec.Template.Spec.Containers
		require.NotEmptyf(t, containers, "statefulset %s has no containers", sts.Name)
		assert.Equalf(t, sts.Image, containers[0].Image,
			"statefulset %s image", sts.Name)
		if sts.PortName != "" {
			require.NotEmptyf(t, containers[0].Ports,
				"statefulset %s has no ports", sts.Name)
			assert.Equalf(t, sts.PortName, containers[0].Ports[0].Name,
				"statefulset %s port name", sts.Name)
		}
		if sts.Replicas > 0 {
			require.NotNil(t, live.Spec.Replicas)
			assert.Equalf(t, sts.Replicas, *live.Spec.Replicas,
				"statefulset %s replicas", sts.Name)
		}
	}

	for _, svc := range exp.Services {
		var live corev1.Service
		require.NoError(t, res.Get(ctx, svc.Name, exp.Namespace, &live))
		dumpActual(t, &live)

		require.NotEmptyf(t, live.Spec.Ports, "service %s has no ports", svc.Name)
		assert.Equalf(t, svc.Port, live.Spec.Ports[0].Port, "service %s port", svc.Name)

		// TargetPort is an intstr; a named port lives in StrVal, not IntVal.
		if svc.TargetPortName != "" {
			assert.Equalf(t, svc.TargetPortName, live.Spec.Ports[0].TargetPort.StrVal,
				"service %s targetPort name", svc.Name)
		}
		if svc.ClusterIP == "None" {
			assert.Equalf(t, corev1.ClusterIPNone, live.Spec.ClusterIP,
				"service %s should be headless", svc.Name)
		}
		for key, val := range svc.Selector {
			assert.Equalf(t, val, live.Spec.Selector[key],
				"service %s selector %s", svc.Name, key)
		}
	}

	for _, wantPVC := range exp.PVCs {
		var pvcs corev1.PersistentVolumeClaimList
		require.NoError(t, res.List(ctx, &pvcs))
		matched := 0
		for _, pvc := range pvcs.Items {
			if !strings.HasPrefix(pvc.Name, wantPVC.NamePrefix) {
				continue
			}
			matched++
			if wantPVC.Phase != "" {
				assert.Equalf(t, wantPVC.Phase, string(pvc.Status.Phase),
					"pvc %s phase", pvc.Name)
			}
			if wantPVC.Storage != "" {
				req := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
				assert.Equalf(t, wantPVC.Storage, req.String(),
					"pvc %s storage", pvc.Name)
			}
		}
		assert.GreaterOrEqualf(t, matched, wantPVC.MinCount,
			"pvcs with prefix %s", wantPVC.NamePrefix)
	}

	if exp.Pods.LabelSelector != "" {
		var pods corev1.PodList
		require.NoError(t, res.List(ctx, &pods,
			resources.WithLabelSelector(exp.Pods.LabelSelector)))
		assert.GreaterOrEqualf(t, len(pods.Items), exp.Pods.MinCount,
			"pods matching %s", exp.Pods.LabelSelector)
		for _, pod := range pods.Items {
			assert.Equalf(t, exp.Pods.Phase, string(pod.Status.Phase),
				"pod %s phase", pod.Name)
		}
	}
}

func newKlient(t testing.TB, kubeconfigPath, contextName string) klient.Client {
	t.Helper()
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = kubeconfigPath
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}

	restCfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules, overrides).ClientConfig()
	require.NoErrorf(t, err, "rest config from %s (context %s)",
		kubeconfigPath, contextName)

	c, err := klient.New(restCfg)
	require.NoError(t, err, "build klient")
	return c
}

func discoverScenarios(t testing.TB, testdataDir string) []scenario {
	t.Helper()
	entries, err := os.ReadDir(testdataDir)
	require.NoErrorf(t, err, "read %s", testdataDir)

	var found []scenario
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(testdataDir, entry.Name()) // absolute
		specPath := filepath.Join(dir, "deployah.yaml")
		if !regularFileExists(specPath) ||
			!regularFileExists(filepath.Join(dir, "expect.yaml")) {
			continue
		}

		raw, readErr := os.ReadFile(specPath) // #nosec G304 -- path under testdata/
		require.NoError(t, readErr)
		var spec struct {
			Project string `yaml:"project"`
		}
		require.NoError(t, yaml.Unmarshal(raw, &spec))
		require.NotEmptyf(t, spec.Project, "%s has no project field", specPath)

		found = append(found, scenario{
			Name: entry.Name(), Dir: dir, Project: spec.Project,
		})
	}
	require.NotEmptyf(t, found, "no scenarios found in %s", testdataDir)
	return found
}

func regularFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func loadExpectations(t testing.TB, dir string) expectations {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "expect.yaml")) // #nosec G304 -- path under testdata/
	require.NoError(t, err)

	var exp expectations
	require.NoError(t, yaml.Unmarshal(raw, &exp))
	require.NotEmptyf(t, exp.Env, "%s/expect.yaml has no env field", dir)
	if exp.Namespace == "" {
		exp.Namespace = "default"
	}
	return exp
}

func run(t testing.TB, args ...string) string {
	t.Helper()
	stdout, _ := runCapture(t, args...)
	return stdout
}

// runCapture runs deployah and returns stdout and stderr on success.
func runCapture(t testing.TB, args ...string) (stdout, stderr string) {
	t.Helper()
	appIO, _, out, errOut := nabattest.NewIO()
	app := cmd.NewApp(nabat.WithIO(appIO))
	err := nabattest.Run(t, app, args)
	require.NoErrorf(t, err, "deployah %s\nstderr:\n%s",
		strings.Join(args, " "), errOut.String())
	return out.String(), errOut.String()
}

func copyTree(t testing.TB, src, dst string) {
	t.Helper()
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
		in, openErr := os.Open(path) // #nosec G304 -- path under testdata/
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
	require.NoError(t, err)
}

func readFixtureFile(t testing.TB, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path) // #nosec G304 -- path under test-controlled temp dir
	require.NoError(t, err)
	return string(raw)
}

func newApiextensionsClient(t testing.TB, kubeconfigPath, contextName string) apiextensionsclient.Interface {
	t.Helper()
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = kubeconfigPath
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	restCfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules, overrides).ClientConfig()
	require.NoError(t, err)
	cs, err := apiextensionsclient.NewForConfig(restCfg)
	require.NoError(t, err)
	return cs
}

func getCRD(t testing.TB, ext apiextensionsclient.Interface, name string) *apiextensionsv1.CustomResourceDefinition {
	t.Helper()
	crd, err := ext.ApiextensionsV1().CustomResourceDefinitions().Get(
		t.Context(), name, metav1.GetOptions{})
	require.NoError(t, err)
	return crd
}

func waitCRDEstablished(t testing.TB, ext apiextensionsclient.Interface, name string) *apiextensionsv1.CustomResourceDefinition {
	t.Helper()
	var latest *apiextensionsv1.CustomResourceDefinition
	require.NoError(t, wait.For(func(ctx context.Context) (bool, error) {
		crd, err := ext.ApiextensionsV1().CustomResourceDefinitions().Get(
			ctx, name, metav1.GetOptions{})
		if err != nil {
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
	}, wait.WithTimeout(2*time.Minute), wait.WithInterval(time.Second)))
	require.NotNil(t, latest)
	return latest
}

func runErr(t testing.TB, args ...string) error {
	t.Helper()
	appIO, _, _, errOut := nabattest.NewIO()
	app := cmd.NewApp(nabat.WithIO(appIO))
	err := nabattest.Run(t, app, args)
	if err == nil {
		return nil
	}
	if stderr := strings.TrimSpace(errOut.String()); stderr != "" {
		return fmt.Errorf("%w\nstderr:\n%s", err, stderr)
	}
	return err
}

func requireEngine(t testing.TB) {
	t.Helper()
	if err := exec.Command("docker", "info").Run(); err != nil {
		if os.Getenv("CI") == "true" {
			t.Fatalf("container engine required in CI: %v", err)
		}
		t.Skipf("no container engine: %v", err)
	}
}

func requireNoCollision(t testing.TB) {
	t.Helper()
	m, err := localkube.New()
	require.NoError(t, err)
	defer m.Close() //nolint:errcheck // best-effort cleanup of provider resources

	_, getErr := m.Get(t.Context(), "deployah")
	if errors.Is(getErr, localkube.ErrNotFound) {
		return // no existing cluster, nothing to do
	}
	require.NoError(t, getErr)

	if os.Getenv("DEPLOYAH_E2E_FORCE") != "1" {
		t.Fatal("cluster 'deployah' already exists; " +
			"set DEPLOYAH_E2E_FORCE=1 to destroy and recreate it")
	}
	t.Log("DEPLOYAH_E2E_FORCE=1: destroying the existing cluster")
	require.NoError(t, runErr(t, "cluster", "down", "--force"))
}

// dumpActual logs a live object as YAML when DEPLOYAH_E2E_DUMP=1, so a new
// scenario's expect.yaml can be curated from what deployah actually renders.
func dumpActual(t testing.TB, obj any) {
	t.Helper()
	if os.Getenv("DEPLOYAH_E2E_DUMP") != "1" {
		return
	}
	out, err := yaml.Marshal(obj)
	if err != nil {
		t.Logf("dump failed: %v", err)
		return
	}
	t.Logf("ACTUAL:\n%s", out)
}
