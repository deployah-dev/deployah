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
	"unicode"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
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

	inttest "deployah.dev/deployah/internal/testing"
)

const (
	kindContext = "kind-deployah"
	clusterName = "deployah"
	maxDNSLabel = 63
)

var flagScaffold = flag.Bool("e2e.scaffold", false, "generate e2e.yaml skeleton from live cluster state")

// E2ESuite drives Deployah against a live Kind cluster created by
// `deployah cluster up`.
type E2ESuite struct {
	suite.Suite
	kcPath       string
	client       klient.Client
	mapper       *restmapper.DeferredDiscoveryRESTMapper
	mapperMu     sync.Mutex
	scenariosDir string
	created      bool // TearDownSuite runs cluster down when true
}

type e2eCase struct {
	Name     string
	Dir      string
	Project  string
	Fixture  inttest.E2EFixture
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
	suite.Run(t, new(E2ESuite))
}

// SetupSuite creates the Kind cluster, preloads allowlisted images, and
// builds a discovery RESTMapper.
func (s *E2ESuite) SetupSuite() {
	t := s.T()
	requireEngine(t)

	scenariosDir, err := filepath.Abs(inttest.TestScenariosDir)
	s.Require().NoError(err)
	s.scenariosDir = scenariosDir

	// Run from a temp dir; cluster up writes deployah.platform.yaml into cwd.
	// t.Chdir restores cwd when the suite ends.
	t.Chdir(t.TempDir())

	requireNoCollision(t)
	// Mark before up so TearDownSuite cleans up if create succeeds but a
	// later step in cluster up fails. down --force is a no-op when missing.
	s.created = true
	run(t, "cluster", "up")

	raw := run(t, "cluster", "status", "--output", "json")
	var status clusterStatusView
	s.Require().NoError(json.Unmarshal([]byte(raw), &status))
	s.Require().Equal(clusterName, status.Name)
	s.Require().Equal("running", status.Status)
	s.Require().Equal(kindContext, status.Context)
	s.Require().True(status.CloudProviderRunning)

	s.kcPath = status.Kubeconfig
	s.Require().FileExists(s.kcPath)

	s.client = newKlient(t, s.kcPath, kindContext)
	s.preloadImages(t)
	s.mapper = newRESTMapper(t, s.client)
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

// TestE2EFixtures runs every scenarios/*/e2e.yaml. Parallel fixtures run
// first (bounded by go test -parallel); sequential fixtures run after,
// alphabetically by directory name.
func (s *E2ESuite) TestE2EFixtures() {
	t := s.T()
	cases := s.loadE2ECases(t)
	require.NotEmpty(t, cases, "no e2e.yaml fixtures under %s", s.scenariosDir)

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

	t.Run("parallel", func(t *testing.T) {
		for _, c := range parallel {
			t.Run(c.Name, func(t *testing.T) {
				t.Parallel()
				s.runE2ECase(t, c)
			})
		}
	})
	t.Run("sequential", func(t *testing.T) {
		for _, c := range sequential {
			t.Run(c.Name, func(t *testing.T) {
				s.runE2ECase(t, c)
			})
		}
	})
}

func (s *E2ESuite) loadE2ECases(t *testing.T) []e2eCase {
	t.Helper()
	scenarios, err := inttest.DiscoverScenarios(s.scenariosDir)
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

		dir := filepath.Join(s.scenariosDir, sc.ScenarioDir)
		fx, loadErr := inttest.LoadE2EFixture(sc.E2EFixturePath, dir)
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

func (s *E2ESuite) runE2ECase(t *testing.T, c e2eCase) {
	t.Helper()
	if c.LoadErr != nil {
		t.Errorf("load %s: %v", c.Name, c.LoadErr)
		return
	}
	// The workspace is local. Preflight must finish before any namespace
	// create or other Kubernetes write.
	dir, err := inttest.NewScenarioWorkspace(t, c.Dir)
	require.NoError(t, err)
	fixturePath := filepath.Join(dir, inttest.E2EFixtureFile)
	fx, err := inttest.LoadE2EFixture(fixturePath, dir)
	if err != nil {
		t.Errorf("load %s: %v", fixturePath, err)
		return
	}
	prepared, err := inttest.PrepareE2EFixture(fixturePath, fx)
	if err != nil {
		t.Errorf("prepare %s: %v", fixturePath, err)
		return
	}

	ns := fixtureNamespace(c.Name)
	claim, err := s.claimNamespace(t.Context(), ns)
	if err != nil {
		t.Errorf("namespace %s: %v", ns, err)
		return
	}
	t.Cleanup(func() {
		cleanupErr := s.cleanupClaim(t, dir, claim, []releaseTarget{{
			Project: c.Project,
			Env:     fx.Env,
		}})
		if cleanupErr != nil {
			t.Errorf("cleanup: %v", cleanupErr)
		}
	})
	result := e2e.Run(t.Context(), e2e.RunInput{
		Dir:       dir,
		Namespace: ns,
		Project:   c.Project,
		Env:       fx.Env,
		Fixture:   prepared,
		Exec:      execCommand(t),
		Observe:   s.observe,
	})
	for _, step := range result.Steps {
		if step.Status == e2e.StatusPass {
			continue
		}
		t.Errorf("%s", step)
	}
	if *flagScaffold {
		s.scaffoldSimple(t, dir, ns, fx)
	}
}

func (s *E2ESuite) preloadImages(t *testing.T) {
	t.Helper()
	m, err := localkube.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		if closeErr := m.Close(); closeErr != nil {
			t.Logf("close localkube manager: %v", closeErr)
		}
	})
	for _, img := range inttest.AllowedE2EImages {
		t.Logf("preloading image %s", img)
		require.NoErrorf(t, m.LoadImage(t.Context(), clusterName, img), "load %s", img)
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
func (s *E2ESuite) newResources(ns string) (*resources.Resources, error) {
	res, err := resources.New(s.client.RESTConfig())
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
	stdout, _ := runCapture(tb, args...)
	return stdout
}

// runCapture runs deployah in the process cwd and returns stdout and stderr.
func runCapture(tb testing.TB, args ...string) (stdout, stderr string) {
	tb.Helper()
	appIO, _, out, errOut := nabattest.NewIO()
	app := cmd.NewApp(nabat.WithIO(appIO))
	err := nabattest.Run(tb, app, args)
	require.NoErrorf(tb, err, "deployah %s\nstderr:\n%s",
		strings.Join(args, " "), errOut.String())
	return out.String(), errOut.String()
}

func runIn(tb testing.TB, dir string, args ...string) (stdout, stderr string) {
	tb.Helper()
	stdout, stderr, err := runInErr(tb, dir, args...)
	require.NoErrorf(tb, err, "deployah %s\nstderr:\n%s",
		strings.Join(args, " "), stderr)
	return stdout, stderr
}

func runInErr(tb testing.TB, dir string, args ...string) (stdout, stderr string, err error) {
	tb.Helper()
	return runInErrContext(tb, tb.Context(), dir, args...)
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

func runErr(tb testing.TB, args ...string) error {
	tb.Helper()
	_, stderr, err := runInErr(tb, "", args...)
	if err == nil {
		return nil
	}
	if trimmed := strings.TrimSpace(stderr); trimmed != "" {
		return fmt.Errorf("%w\nstderr:\n%s", err, trimmed)
	}
	return err
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

func requireNoCollision(tb testing.TB) {
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
		return
	}
	require.NoError(tb, getErr)

	if os.Getenv("DEPLOYAH_E2E_FORCE") != "1" {
		tb.Fatal("cluster 'deployah' already exists; " +
			"set DEPLOYAH_E2E_FORCE=1 to destroy and recreate it")
	}
	tb.Log("DEPLOYAH_E2E_FORCE=1: destroying the existing cluster")
	require.NoError(tb, runErr(tb, "cluster", "down", "--force"))
}
