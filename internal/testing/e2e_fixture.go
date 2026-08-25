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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	k8sjson "k8s.io/apimachinery/pkg/util/json"
)

// E2EFixtureFile is the Kind fixture filename in a scenario directory.
const E2EFixtureFile = "e2e.yaml"

// DefaultE2EStepTimeout is the wait applied when e2e.yaml omits timeout.
const DefaultE2EStepTimeout = 3 * time.Minute

// AllowedE2EImages is the Kind preload list. [LoadE2EFixture] rejects
// scenario specs that reference any other image.
var AllowedE2EImages = []string{
	"nginx:latest",
	"nginx:1.26",
	"busybox:1.36",
	"redis:7-alpine",
	"redis:7",
	"postgres:16",
}

// E2EFixture is the decoded form of scenarios/*/e2e.yaml, requiring
// exactly one of Resources or Steps.
type E2EFixture struct {
	Env       string              `json:"env"`
	Parallel  *bool               `json:"parallel,omitempty"`
	Timeout   string              `json:"timeout,omitempty"`
	Resources []ResourceAssertion `json:"resources,omitempty"`
	Steps     []Step              `json:"steps,omitempty"`
}

// ResourceAssertion is one cluster assertion.
// Match is a partial Kubernetes object compared with [DiffSubset].
// MinCount defaults to 1; 0 requires the list to be empty.
// When Match sets metadata.name, MinCount may only be 0 or 1 because a
// named Get cannot count list items.
type ResourceAssertion struct {
	Match    map[string]any `json:"match"`
	MinCount *int           `json:"minCount,omitempty"`
}

// Step is one CLI operation plus optional output and resource assertions.
// Exactly one of Deploy, Run, Logs, or Delete must be set.
type Step struct {
	Deploy         *DeployOp           `json:"deploy,omitempty"`
	Run            *RunOp              `json:"run,omitempty"`
	Logs           *LogsOp             `json:"logs,omitempty"`
	Delete         *DeleteOp           `json:"delete,omitempty"`
	Timeout        string              `json:"timeout,omitempty"`
	StderrContains string              `json:"stderrContains,omitempty"`
	StdoutContains string              `json:"stdoutContains,omitempty"`
	Resources      []ResourceAssertion `json:"resources,omitempty"`
}

// DeployOp maps to `deployah deploy`.
type DeployOp struct {
	Spec string   `json:"spec,omitempty"`
	Args []string `json:"args,omitempty"`
}

// RunOp maps to `deployah run`.
type RunOp struct {
	Task string `json:"task"`
}

// LogsOp maps to `deployah logs`.
type LogsOp struct {
	Component string `json:"component"`
	Contains  string `json:"contains"`
}

// DeleteOp maps to `deployah delete`.
type DeleteOp struct{}

// RunParallel reports whether the fixture may run concurrently.
// A nil Parallel field means true.
func (f E2EFixture) RunParallel() bool {
	return f.Parallel == nil || *f.Parallel
}

// StepTimeout is the per-step wait: step override, then fixture default,
// then [DefaultE2EStepTimeout].
func (f E2EFixture) StepTimeout(step Step) (time.Duration, error) {
	raw := step.Timeout
	if raw == "" {
		raw = f.Timeout
	}
	if raw == "" {
		return DefaultE2EStepTimeout, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("timeout %q: %w", raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("timeout %q must be positive", raw)
	}
	return d, nil
}

// Count returns the minimum matching resources, or 1 when MinCount is unset.
func (r ResourceAssertion) Count() int {
	if r.MinCount == nil {
		return 1
	}
	return *r.MinCount
}

// OpName returns the operation name, or "" if none or more than one is set.
func (s Step) OpName() string {
	n := 0
	name := ""
	if s.Deploy != nil {
		n++
		name = "deploy"
	}
	if s.Run != nil {
		n++
		name = "run"
	}
	if s.Logs != nil {
		n++
		name = "logs"
	}
	if s.Delete != nil {
		n++
		name = "delete"
	}
	if n != 1 {
		return ""
	}
	return name
}

// LoadE2EFixture reads path, validates document shape, and checks that
// images in scenarioDir specs are on [AllowedE2EImages].
func LoadE2EFixture(path, scenarioDir string) (E2EFixture, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- scenario e2e.yaml under test
	if err != nil {
		return E2EFixture{}, fmt.Errorf("read e2e.yaml: %w", err)
	}
	jsonBytes, err := yaml.YAMLToJSON(raw)
	if err != nil {
		return E2EFixture{}, fmt.Errorf("parse e2e.yaml: %w", err)
	}
	var fx E2EFixture
	if decErr := k8sjson.Unmarshal(jsonBytes, &fx); decErr != nil {
		return E2EFixture{}, fmt.Errorf("decode e2e.yaml: %w", decErr)
	}
	if valErr := fx.validate(); valErr != nil {
		return E2EFixture{}, valErr
	}
	if imgErr := validateScenarioImages(scenarioDir, fx); imgErr != nil {
		return E2EFixture{}, imgErr
	}
	return fx, nil
}

func (f E2EFixture) validate() error {
	if f.Env == "" {
		return fmt.Errorf("e2e.yaml: env is required")
	}
	hasRes := len(f.Resources) > 0
	hasSteps := len(f.Steps) > 0
	if hasRes == hasSteps {
		return fmt.Errorf("e2e.yaml: exactly one of resources or steps is required")
	}
	if _, err := f.StepTimeout(Step{}); err != nil {
		return fmt.Errorf("e2e.yaml: %w", err)
	}
	for i, ra := range f.Resources {
		if err := ra.validate(); err != nil {
			return fmt.Errorf("e2e.yaml resources[%d]: %w", i, err)
		}
	}
	for i, step := range f.Steps {
		if step.OpName() == "" {
			return fmt.Errorf("e2e.yaml steps[%d]: exactly one of deploy, run, logs, delete is required", i)
		}
		if step.Run != nil && step.Run.Task == "" {
			return fmt.Errorf("e2e.yaml steps[%d]: run.task is required", i)
		}
		if step.Logs != nil && (step.Logs.Component == "" || step.Logs.Contains == "") {
			return fmt.Errorf("e2e.yaml steps[%d]: logs.component and logs.contains are required", i)
		}
		if _, err := f.StepTimeout(step); err != nil {
			return fmt.Errorf("e2e.yaml steps[%d]: %w", i, err)
		}
		for j, ra := range step.Resources {
			if err := ra.validate(); err != nil {
				return fmt.Errorf("e2e.yaml steps[%d].resources[%d]: %w", i, j, err)
			}
		}
	}
	return nil
}

func (r ResourceAssertion) validate() error {
	if len(r.Match) == 0 {
		return fmt.Errorf("match is required")
	}
	kind, hasKind := r.Match["kind"].(string)
	apiVersion, hasAPI := r.Match["apiVersion"].(string)
	if !hasKind || kind == "" || !hasAPI || apiVersion == "" {
		return fmt.Errorf("match requires apiVersion and kind")
	}
	if r.Count() < 0 {
		return fmt.Errorf("minCount must be >= 0")
	}
	if r.Count() > 1 {
		metaMap, hasMeta := r.Match["metadata"].(map[string]any)
		if hasMeta {
			name, hasName := metaMap["name"].(string)
			if hasName && name != "" {
				return fmt.Errorf("minCount > 1 cannot be used with metadata.name")
			}
		}
	}
	return nil
}

func validateScenarioImages(scenarioDir string, fx E2EFixture) error {
	specs := []string{filepath.Join(scenarioDir, "deployah.yaml")}
	for _, step := range fx.Steps {
		if step.Deploy != nil && step.Deploy.Spec != "" {
			specs = append(specs, filepath.Join(scenarioDir, step.Deploy.Spec))
		}
	}
	seen := map[string]struct{}{}
	for _, specPath := range specs {
		if _, err := os.Stat(specPath); err != nil {
			continue
		}
		raw, err := os.ReadFile(specPath) // #nosec G304 -- scenario spec under test
		if err != nil {
			return fmt.Errorf("read spec %s: %w", specPath, err)
		}
		var doc any
		if parseErr := yaml.Unmarshal(raw, &doc); parseErr != nil {
			return fmt.Errorf("parse spec %s: %w", specPath, parseErr)
		}
		for _, img := range collectImages(doc) {
			if _, ok := seen[img]; ok {
				continue
			}
			seen[img] = struct{}{}
			if !imageAllowed(img) {
				return fmt.Errorf("image %q is not on the e2e allowlist", img)
			}
		}
	}
	return nil
}

func collectImages(v any) []string {
	var out []string
	switch n := v.(type) {
	case map[string]any:
		for k, child := range n {
			if k == "image" {
				if s, ok := child.(string); ok && s != "" && !strings.Contains(s, "${") {
					out = append(out, s)
				}
				continue
			}
			out = append(out, collectImages(child)...)
		}
	case []any:
		for _, child := range n {
			out = append(out, collectImages(child)...)
		}
	}
	return out
}

func imageAllowed(img string) bool {
	norm := normalizeImageRef(img)
	for _, allowed := range AllowedE2EImages {
		if normalizeImageRef(allowed) == norm {
			return true
		}
	}
	return false
}

func normalizeImageRef(img string) string {
	img = strings.TrimPrefix(img, "docker.io/library/")
	img = strings.TrimPrefix(img, "docker.io/")
	return img
}
