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
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"
	"sigs.k8s.io/e2e-framework/klient"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"

	"deployah.dev/deployah/internal/cmd"
	"deployah.dev/deployah/internal/localkube"
	"deployah.dev/deployah/internal/testing/e2e"

	live "deployah.dev/deployah/internal/e2e"
	inttest "deployah.dev/deployah/internal/testing"
)

const (
	kindContext = "kind-deployah"
	clusterName = "deployah"
	maxDNSLabel = 63
)

var flagScaffold = flag.Bool("e2e.scaffold", false, "generate e2e.yaml skeleton from live cluster state")

// e2eEnv is the shared Kind cluster for one TestE2E run.
type e2eEnv struct {
	kcPath       string
	client       klient.Client
	mapper       *restmapper.DeferredDiscoveryRESTMapper
	mapperMu     sync.Mutex
	scenariosDir string
}

type e2eCase struct {
	Name     string
	Dir      string
	Project  string
	Fixture  e2e.E2EFixture
	Parallel bool
	LoadErr  error
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

// TestE2E runs the Kind-based end-to-end suite.
func TestE2E(t *testing.T) {
	env := startCluster(t)
	t.Run("TestCRDLifecycle", func(t *testing.T) { runCRDLifecycle(t, env) })
	t.Run("TestCRDNewlyAddedOnUpgrade", func(t *testing.T) { runCRDNewlyAddedOnUpgrade(t, env) })
	t.Run("TestDeployCreatesNamespace", func(t *testing.T) { runDeployCreatesNamespace(t, env) })
	t.Run("TestE2EFixtures", func(t *testing.T) { runE2EFixtures(t, env) })
	t.Run("TestPlanCLI", func(t *testing.T) { runPlanCLI(t, env) })
	t.Run("TestPublicImagePull", func(t *testing.T) { runPublicImagePull(t, env) })
	t.Run("TestSemanticPlanDoesNotMutateLive", func(t *testing.T) { runSemanticPlanDoesNotMutateLive(t, env) })
	t.Run("TestWildcardInstances", func(t *testing.T) { runWildcardInstances(t, env) })
}

// startCluster creates the Kind cluster owned by this test.
func startCluster(t *testing.T) *e2eEnv {
	t.Helper()
	// Cleanup is registered before create. A pre-existing cluster is left
	// alone unless DEPLOYAH_E2E_FORCE=1.
	requireEngine(t)
	// cluster up writes deployah.platform.yaml into the process directory.
	// Child scenarios pass their own directory and do not change it.
	t.Chdir(t.TempDir())

	scenariosDir, err := filepath.Abs(inttest.TestScenariosDir)
	require.NoError(t, err)
	found, getErr := lookupCluster(t)
	force := os.Getenv("DEPLOYAH_E2E_FORCE") == "1"
	intent := live.PlanCluster(found, getErr, force)
	if intent == live.ClusterReplace {
		t.Log("DEPLOYAH_E2E_FORCE=1: destroying the existing cluster")
	}
	switch intent {
	case live.ClusterRefuse:
		t.Fatal("cluster 'deployah' already exists; " +
			"set DEPLOYAH_E2E_FORCE=1 to destroy and recreate it")
	case live.ClusterUncertain:
		t.Fatalf("cluster lookup: %v", getErr)
	}
	var env e2eEnv
	env.scenariosDir = scenariosDir
	err = live.PrepareCluster(t, intent, func() error {
		// Cleanup runs after t.Context is canceled, so down needs its own deadline.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_, stderr, downErr := runInErrContext(t, ctx, "", "cluster", "down", "--force")
		if downErr == nil {
			return nil
		}
		if trimmed := strings.TrimSpace(stderr); trimmed != "" {
			return fmt.Errorf("%w\nstderr:\n%s", downErr, trimmed)
		}
		return downErr
	}, func() error {
		run(t, "cluster", "up")
		raw := run(t, "cluster", "status", "--output", "json")
		var status clusterStatusView
		if unmarshalErr := json.Unmarshal([]byte(raw), &status); unmarshalErr != nil {
			return unmarshalErr
		}
		if status.Name != clusterName || status.Status != "running" || status.Context != kindContext || !status.CloudProviderRunning {
			return fmt.Errorf("cluster status %+v", status)
		}
		if _, statErr := os.Stat(status.Kubeconfig); statErr != nil {
			return fmt.Errorf("kubeconfig %s: %w", status.Kubeconfig, statErr)
		}
		env.kcPath = status.Kubeconfig
		env.client = newKlient(t, env.kcPath, kindContext)
		env.mapper = newRESTMapper(t, env.client)
		return nil
	})
	require.NoError(t, err)
	return &env
}

// runE2EFixtures runs every scenarios/*/e2e.yaml.
// Failures in this test should point here, not at the suite caller.
//
//nolint:thelper
func runE2EFixtures(t *testing.T, env *e2eEnv) {
	// Parallel fixtures share the go test -parallel limit. Sequential
	// fixtures run after them, in directory name order.
	cases := env.loadE2ECases(t)
	require.NotEmpty(t, cases, "no e2e.yaml fixtures under %s", env.scenariosDir)

	var parallel, sequential []e2eCase
	for _, c := range cases {
		if c.Parallel {
			parallel = append(parallel, c)
		} else {
			sequential = append(sequential, c)
		}
	}
	slices.SortFunc(sequential, func(a, b e2eCase) int {
		return strings.Compare(a.Name, b.Name)
	})

	live.RunGroups(t, parallel, sequential, func(c e2eCase) string { return c.Name }, env.runE2ECase)
}

func (env *e2eEnv) loadE2ECases(t *testing.T) []e2eCase {
	t.Helper()
	scenarios, err := inttest.DiscoverScenarios(env.scenariosDir)
	require.NoError(t, err)

	seen := map[string]struct{}{}
	var cases []e2eCase
	for _, sc := range scenarios {
		if !sc.HasE2EFixture {
			continue
		}
		if _, ok := seen[sc.ScenarioDir]; ok {
			continue
		}
		seen[sc.ScenarioDir] = struct{}{}

		dir := filepath.Join(env.scenariosDir, sc.ScenarioDir)
		fx, loadErr := e2e.LoadE2EFixture(sc.E2EFixturePath, dir)
		project, projErr := projectName(dir)
		if projErr != nil && loadErr == nil {
			loadErr = projErr
		}
		parallel := false
		if loadErr == nil {
			parallel = fx.RunParallel()
		}
		cases = append(cases, e2eCase{
			Name:     sc.ScenarioDir,
			Dir:      dir,
			Project:  project,
			Fixture:  fx,
			Parallel: parallel,
			LoadErr:  loadErr,
		})
	}
	return cases
}

// Step lines stay here. t.Helper would point them at the group loop.
//
//nolint:thelper
func (env *e2eEnv) runE2ECase(t *testing.T, c e2eCase) {
	if c.LoadErr != nil {
		t.Errorf("load %s: %v", c.Name, c.LoadErr)
		return
	}
	// The workspace is local. Preflight must finish before any namespace
	// create or other Kubernetes write.
	dir, err := inttest.NewScenarioWorkspace(t, c.Dir)
	require.NoError(t, err)
	fixturePath := filepath.Join(dir, e2e.FixtureFile)
	fx, err := e2e.LoadE2EFixture(fixturePath, dir)
	if err != nil {
		t.Errorf("load %s: %v", fixturePath, err)
		return
	}
	prepared, err := e2e.PrepareE2EFixture(fixturePath, fx)
	if err != nil {
		t.Errorf("prepare %s: %v", fixturePath, err)
		return
	}

	ns := fixtureNamespace(c.Name)
	claim, err := env.claimNamespace(t.Context(), ns)
	if err != nil {
		t.Errorf("namespace %s: %v", ns, err)
		return
	}
	t.Cleanup(func() {
		started := time.Now()
		cleanupErr := env.cleanupClaim(t, dir, claim, []releaseTarget{{
			Project: c.Project,
			Env:     fx.Env,
		}})
		if cleanupErr != nil {
			t.Errorf("cleanup: %v", cleanupErr)
			return
		}
		if testing.Verbose() {
			t.Logf("PASS  cleanup (%s)", time.Since(started).Round(time.Millisecond))
		}
	})
	result := e2e.Run(t.Context(), e2e.RunInput{
		Dir:       dir,
		Namespace: ns,
		Project:   c.Project,
		Env:       fx.Env,
		Fixture:   prepared,
		Exec:      execCommand(t),
		Observe:   env.observe,
	})
	info, fails := e2e.StepReports(testing.Verbose(), c.Name, result.Steps)
	for _, line := range info {
		t.Log(line)
	}
	for _, line := range fails {
		t.Error(line)
	}
	if *flagScaffold {
		env.scaffoldSimple(t, dir, ns, fx)
	}
}

func newRESTMapper(tb testing.TB, c klient.Client) *restmapper.DeferredDiscoveryRESTMapper {
	tb.Helper()
	disco, err := discovery.NewDiscoveryClientForConfig(c.RESTConfig())
	require.NoError(tb, err)
	return restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(disco))
}

func newKlient(tb testing.TB, kubeconfigPath, contextName string) klient.Client {
	tb.Helper()
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = kubeconfigPath
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}

	restCfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules, overrides).ClientConfig()
	require.NoErrorf(tb, err, "rest config from %s (context %s)",
		kubeconfigPath, contextName)

	c, err := klient.New(restCfg)
	require.NoError(tb, err, "build klient")
	return c
}

// newResources returns a dedicated Resources client. klient.Client.Resources
// mutates a shared namespace field, which is not safe under t.Parallel.
func (env *e2eEnv) newResources(ns string) (*resources.Resources, error) {
	res, err := resources.New(env.client.RESTConfig())
	if err != nil {
		return nil, err
	}
	return res.WithNamespace(ns), nil
}

func projectName(dir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "deployah.yaml")) // #nosec G304 -- scenario spec
	if err != nil {
		return "", fmt.Errorf("read deployah.yaml: %w", err)
	}
	var spec struct {
		Project string `yaml:"project"`
	}
	if err = yaml.Unmarshal(raw, &spec); err != nil {
		return "", fmt.Errorf("parse deployah.yaml: %w", err)
	}
	if spec.Project == "" {
		return "", fmt.Errorf("%s/deployah.yaml has no project field", dir)
	}
	return spec.Project, nil
}

func fixtureNamespace(name string) string {
	var b strings.Builder
	b.WriteString("e2e-")
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteRune('-')
	}
	ns := strings.Trim(b.String(), "-")
	if len(ns) > maxDNSLabel {
		ns = strings.Trim(ns[:maxDNSLabel], "-")
	}
	return ns
}

func run(tb testing.TB, args ...string) string {
	tb.Helper()
	appIO, _, out, errOut := nabattest.NewIO()
	app := cmd.NewApp(nabat.WithIO(appIO))
	err := nabattest.Run(tb, app, args)
	require.NoErrorf(tb, err, "deployah %s\nstderr:\n%s",
		strings.Join(args, " "), errOut.String())
	return out.String()
}

func runIn(tb testing.TB, dir string, args ...string) (stdout, stderr string) {
	tb.Helper()
	stdout, stderr, err := runInErrContext(tb, tb.Context(), dir, args...)
	require.NoErrorf(tb, err, "deployah %s\nstderr:\n%s",
		strings.Join(args, " "), stderr)
	return stdout, stderr
}

func runInErrContext(tb testing.TB, ctx context.Context, dir string, args ...string) (stdout, stderr string, err error) {
	tb.Helper()
	appIO, _, out, errOut := nabattest.NewIO()
	app := cmd.NewApp(nabat.WithIO(appIO))
	opts := []nabattest.RunOption{nabattest.WithContext(ctx)}
	if dir != "" {
		opts = append(opts, nabattest.WithDir(dir))
	}
	err = nabattest.RunParallel(tb, app, args, opts...)
	return out.String(), errOut.String(), err
}

func requireEngine(tb testing.TB) {
	tb.Helper()
	if err := exec.Command("docker", "info").Run(); err != nil {
		if os.Getenv("CI") == "true" {
			tb.Fatalf("container engine required in CI: %v", err)
		}
		tb.Skipf("no container engine: %v", err)
	}
}

func lookupCluster(tb testing.TB) (bool, error) {
	tb.Helper()
	m, err := localkube.New()
	require.NoError(tb, err)
	tb.Cleanup(func() {
		if closeErr := m.Close(); closeErr != nil {
			tb.Logf("close localkube manager: %v", closeErr)
		}
	})
	_, getErr := m.Get(tb.Context(), clusterName)
	if errors.Is(getErr, localkube.ErrNotFound) {
		return false, nil
	}
	if getErr != nil {
		return false, getErr
	}
	return true, nil
}
