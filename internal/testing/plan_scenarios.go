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

package testing

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan"
	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// updateGolden regenerates a plan scenario's golden.txt/golden.json files
// instead of comparing against them, following the standard Go
// -update convention for golden files (see @go-testing.mdc).
var updateGolden = flag.Bool("update", false, "regenerate plan scenario golden files")

// PlanTestScenario describes one scenarios/plan-* directory: a before/after
// manifest pair (however each side is sourced) that [RunPlanScenarioTest]
// diffs through the real [plan.ComputeDiff] engine and checks against
// plan-config.yaml.
type PlanTestScenario struct {
	// Name is the scenario directory name (e.g. "plan-image-bump").
	Name string
	// Dir is the scenario's absolute directory path.
	Dir string
}

// PlanConfig is the structural expectation a scenario author writes by
// hand in plan-config.yaml: what [plan.ComputeDiff] must produce for this
// scenario's before/after pair, independent of how the text or JSON
// renderers format it.
type PlanConfig struct {
	// FreshInstall selects an empty previous manifest instead of resolving
	// one from before.yaml or previous.yaml, for the fresh-install case
	// where neither file is expected to exist.
	FreshInstall bool `yaml:"freshInstall"`
	// Changes lists every expected change, in the exact order
	// [plan.ComputeDiff] returns them (sorted by Kind, then Name).
	Changes []PlanConfigChange `yaml:"changes"`
	// Summary is the expected change tally.
	Summary PlanConfigSummary `yaml:"summary"`
	// HooksChanged asserts [plan.Plan.HooksChanged]. Always false in this
	// scenario suite today: the embedded chart defines no hooks, so this
	// only documents the plumbing for when one is added.
	HooksChanged bool `yaml:"hooksChanged"`
	// Masked lists field paths (as they appear in a Changes[].Fields[]
	// entry's Path) that [plan.ApplyMasking] must flag as masked. Every
	// masked path [RunPlanScenarioTest] finds in the computed plan must
	// appear here, and vice versa.
	Masked []string `yaml:"masked"`
	// Warning is documentary only: this offline harness has no live/faked
	// release store to compute a real warning from ([plan.LastSuccessfulRelease]
	// is covered separately in history_test.go), so the test runner just
	// sets Header.Warning to this value before checking it.
	Warning string `yaml:"warning"`
}

// PlanConfigChange is one expected entry in [PlanConfig.Changes].
type PlanConfigChange struct {
	Action string            `yaml:"action"`
	Kind   string            `yaml:"kind"`
	Name   string            `yaml:"name"`
	Fields []PlanConfigField `yaml:"fields"`
}

// PlanConfigField is one expected field-level difference within a
// [PlanConfigChange]. Old is empty for an added field; New is empty for a
// removed field, mirroring [plan.FieldDiff].
type PlanConfigField struct {
	Path string `yaml:"path"`
	Old  string `yaml:"old"`
	New  string `yaml:"new"`
}

// PlanConfigSummary is the expected [plan.Summary].
type PlanConfigSummary struct {
	Add     int `yaml:"add"`
	Change  int `yaml:"change"`
	Destroy int `yaml:"destroy"`
}

// DiscoverPlanScenarios finds every scenarios/plan-* directory with a
// plan-config.yaml. Unlike [DiscoverScenarios], deployah.yaml is optional:
// [RunPlanScenarioTest] falls back to a raw current.yaml for kinds the
// spec/chart pipeline can't produce (e.g. a Secret).
func DiscoverPlanScenarios(scenariosDir string) ([]PlanTestScenario, error) {
	entries, err := os.ReadDir(scenariosDir)
	if err != nil {
		return nil, fmt.Errorf("reading scenarios directory: %w", err)
	}

	var scenarios []PlanTestScenario
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "plan-") {
			continue
		}
		dir := filepath.Join(scenariosDir, entry.Name())
		if _, statErr := os.Stat(filepath.Join(dir, "plan-config.yaml")); errors.Is(statErr, fs.ErrNotExist) {
			continue
		}
		scenarios = append(scenarios, PlanTestScenario{Name: entry.Name(), Dir: dir})
	}

	return scenarios, nil
}

// RunPlanScenarioTest resolves scenario's previous/current manifest pair,
// computes the diff through the real plan engine, and checks the result
// against plan-config.yaml (and, when present, golden.txt/golden.json).
func RunPlanScenarioTest(t *testing.T, scenario PlanTestScenario) {
	t.Helper()
	t.Run(scenario.Name, func(t *testing.T) {
		cfg := loadPlanConfig(t, filepath.Join(scenario.Dir, "plan-config.yaml"))

		previous := resolvePreviousSide(t, scenario.Dir, cfg)
		current := resolveCurrentSide(t, scenario.Dir)

		p, err := plan.ComputeDiff(previous.Manifest, current.Manifest)
		require.NoError(t, err)
		p.HooksChanged = plan.HooksChanged(previous.Hooks, current.Hooks)
		p.Header = plan.Header{
			Project:      current.Project,
			Environment:  current.Environment,
			Release:      current.ReleaseName,
			Namespace:    current.Namespace,
			FreshInstall: previous.Manifest == "",
		}
		if cfg.Warning != "" {
			p.Header.Warning = cfg.Warning
		}
		plan.ApplyMasking(p)

		assertPlanMatchesConfig(t, p, cfg)
		checkTextGolden(t, scenario.Dir, p)
		checkJSONGolden(t, scenario.Dir, p)
	})
}

// manifestSide is one side (previous or current) of a plan scenario's diff
// input, plus [plan.Header] metadata populated when it came from rendering
// a spec file; those fields stay zero for a raw manifest file.
type manifestSide struct {
	Manifest    string
	Hooks       []*v1.Hook
	Project     string
	Environment string
	ReleaseName string
	Namespace   string
}

// resolvePreviousSide resolves a scenario's previous manifest: a raw
// previous.yaml (for cases a chart can't render, e.g. simulated noise
// fields) takes precedence, then a rendered before.yaml, then an empty
// manifest when plan-config.yaml declares freshInstall. Fails the test if
// none apply.
func resolvePreviousSide(t *testing.T, dir string, cfg PlanConfig) manifestSide {
	t.Helper()
	if side, ok := readRawManifestFile(t, filepath.Join(dir, "previous.yaml")); ok {
		return side
	}
	beforePath := filepath.Join(dir, "before.yaml")
	if _, err := os.Stat(beforePath); err == nil {
		return renderManifestFile(t, beforePath)
	}
	if !cfg.FreshInstall {
		t.Fatalf("scenario %s: no previous.yaml or before.yaml, and freshInstall is not set in plan-config.yaml", dir)
	}
	return manifestSide{}
}

// resolveCurrentSide resolves a scenario's current manifest: a raw
// current.yaml takes precedence (for resource kinds the spec/chart
// pipeline cannot produce, e.g. a bare Secret), otherwise deployah.yaml is
// rendered.
func resolveCurrentSide(t *testing.T, dir string) manifestSide {
	t.Helper()
	if side, ok := readRawManifestFile(t, filepath.Join(dir, "current.yaml")); ok {
		return side
	}
	return renderManifestFile(t, filepath.Join(dir, "deployah.yaml"))
}

// readRawManifestFile reads path as a raw, already-rendered Kubernetes
// manifest. It reports false (with a zero manifestSide) when path does not
// exist, and fails the test on any other read error.
func readRawManifestFile(t *testing.T, path string) (manifestSide, bool) {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- path built from scenario discovery under scenarios/
	if errors.Is(err, fs.ErrNotExist) {
		return manifestSide{}, false
	}
	require.NoError(t, err)
	return manifestSide{Manifest: string(data)}, true
}

// renderManifestFile loads specPath as a Deployah spec and renders it via
// [helm.Client.RenderOffline] (no cluster access, matching `deployah plan
// --offline`), the same rendering path the existing render scenario suite
// uses.
func renderManifestFile(t *testing.T, specPath string) manifestSide {
	t.Helper()
	ctx := context.Background()

	manifest, err := spec.Load(ctx, specPath, "", nil)
	require.NoError(t, err)
	envName, _, err := spec.ResolveEnvironment(manifest.Environments, nil, "")
	require.NoError(t, err)

	client, err := helm.NewClient()
	require.NoError(t, err)

	result, cleanup, err := client.RenderOffline(ctx, manifest, envName, nil)
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	require.NoError(t, err)

	return manifestSide{
		Manifest:    result.Manifest,
		Hooks:       result.Hooks,
		Project:     manifest.Project,
		Environment: envName,
		ReleaseName: result.ReleaseName,
		Namespace:   result.Namespace,
	}
}

// loadPlanConfig reads and parses a scenario's plan-config.yaml.
func loadPlanConfig(t *testing.T, path string) PlanConfig {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- path built from scenario discovery under scenarios/
	require.NoError(t, err)

	var cfg PlanConfig
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	return cfg
}

// assertPlanMatchesConfig checks the computed plan p against every
// structural expectation declared in cfg.
func assertPlanMatchesConfig(t *testing.T, p *plan.Plan, cfg PlanConfig) {
	t.Helper()

	require.Len(t, p.Changes, len(cfg.Changes), "change count mismatch")
	for i, want := range cfg.Changes {
		got := p.Changes[i]
		assert.Equal(t, want.Action, string(got.Action), "changes[%d].action", i)
		assert.Equal(t, want.Kind, got.Kind, "changes[%d].kind", i)
		assert.Equal(t, want.Name, got.Name, "changes[%d].name", i)

		require.Len(t, got.Fields, len(want.Fields), "changes[%d].fields count mismatch", i)
		for j, wantField := range want.Fields {
			gotField := got.Fields[j]
			assert.Equal(t, wantField.Path, gotField.Path, "changes[%d].fields[%d].path", i, j)
			assert.Equal(t, wantField.Old, gotField.Old, "changes[%d].fields[%d].old", i, j)
			assert.Equal(t, wantField.New, gotField.New, "changes[%d].fields[%d].new", i, j)
		}
	}

	assert.Equal(t, cfg.Summary.Add, p.Summary.Add, "summary.add")
	assert.Equal(t, cfg.Summary.Change, p.Summary.Change, "summary.change")
	assert.Equal(t, cfg.Summary.Destroy, p.Summary.Destroy, "summary.destroy")
	assert.Equal(t, cfg.HooksChanged, p.HooksChanged, "hooksChanged")

	if cfg.Warning != "" {
		assert.Contains(t, p.Header.Warning, cfg.Warning, "header.warning")
	}

	assert.ElementsMatch(t, cfg.Masked, maskedFieldPaths(p), "masked field paths")
}

// maskedFieldPaths collects the Path of every FieldDiff [plan.ApplyMasking]
// flagged as masked, across all of p's changes.
func maskedFieldPaths(p *plan.Plan) []string {
	var paths []string
	for _, c := range p.Changes {
		for _, f := range c.Fields {
			if f.Masked {
				paths = append(paths, f.Path)
			}
		}
	}
	return paths
}

// checkTextGolden compares [plan.RenderText]'s output for p against
// dir/golden.txt when that file exists; scenarios without a golden.txt
// only run the structural checks in [assertPlanMatchesConfig].
func checkTextGolden(t *testing.T, dir string, p *plan.Plan) {
	t.Helper()
	goldenPath := filepath.Join(dir, "golden.txt")
	if _, err := os.Stat(goldenPath); errors.Is(err, fs.ErrNotExist) {
		return
	}

	var buf strings.Builder
	require.NoError(t, plan.RenderText(&buf, p, plan.TextOptions{}))
	compareOrUpdateGolden(t, goldenPath, buf.String())
}

// checkJSONGolden is [checkTextGolden]'s JSON counterpart, comparing
// [plan.RenderJSON]'s output against dir/golden.json when it exists.
func checkJSONGolden(t *testing.T, dir string, p *plan.Plan) {
	t.Helper()
	goldenPath := filepath.Join(dir, "golden.json")
	if _, err := os.Stat(goldenPath); errors.Is(err, fs.ErrNotExist) {
		return
	}

	var buf strings.Builder
	require.NoError(t, plan.RenderJSON(&buf, p))
	compareOrUpdateGolden(t, goldenPath, buf.String())
}

// compareOrUpdateGolden compares actual against the contents of path, or
// overwrites path with actual when *updateGolden is set
// (`go test -update`).
func compareOrUpdateGolden(t *testing.T, path, actual string) {
	t.Helper()
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(actual), 0o600))
		return
	}

	want, err := os.ReadFile(path) // #nosec G304 -- path is a scenario golden file under test
	require.NoError(t, err)
	assert.Equal(t, string(want), actual, "golden file %s is stale; run with -update to regenerate", path)
}
