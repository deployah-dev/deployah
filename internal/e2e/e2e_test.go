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
	"encoding/json"
	"errors"
	"fmt"
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
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"

	"deployah.dev/deployah/internal/cmd"
	"deployah.dev/deployah/internal/localkube"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// E2ESuite drives Deployah against a live Kind cluster created by
// `deployah cluster up`.
type E2ESuite struct {
	suite.Suite
	kcPath    string
	client    klient.Client
	scenarios []scenario
	created   bool // true once the suite attempts cluster up (teardown if partial)
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
	Env         string               `yaml:"env"`
	Namespace   string               `yaml:"namespace"`
	Deployments []expectedDeployment `yaml:"deployments"`
	Services    []expectedService    `yaml:"services"`
	Pods        expectedPods         `yaml:"pods"`
}

type expectedDeployment struct {
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
		for key, val := range svc.Selector {
			assert.Equalf(t, val, live.Spec.Selector[key],
				"service %s selector %s", svc.Name, key)
		}
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
	io, _, out, errOut := nabattest.NewIO()
	app := cmd.NewApp(nabat.WithIO(io))
	err := nabattest.Run(t, app, args)
	require.NoErrorf(t, err, "deployah %s\nstderr:\n%s",
		strings.Join(args, " "), errOut.String())
	return out.String()
}

func runErr(t testing.TB, args ...string) error {
	t.Helper()
	io, _, _, errOut := nabattest.NewIO()
	app := cmd.NewApp(nabat.WithIO(io))
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
