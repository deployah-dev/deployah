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
	"encoding/json"
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

// AllowedE2EImages is the Kind preload list.
// Specs that use anything else are rejected.
var AllowedE2EImages = []string{
	"nginx:latest",
	"nginx:1.26",
	"busybox:1.36",
	"redis:7-alpine",
	"redis:7",
	"postgres:16",
}

// E2EFixture is the decoded form of scenarios/*/e2e.yaml.
type E2EFixture struct {
	Env      string `json:"env"`
	Parallel *bool  `json:"parallel,omitempty"`
	Timeout  string `json:"timeout,omitempty"`
	Steps    []Step `json:"steps"`
}

// Step is one operation plus optional timeout and stopOnFailure.
// Exactly one operation is set.
type Step struct {
	Deploy        *DeployOp           `json:"deploy,omitempty"`
	Plan          *PlanOp             `json:"plan,omitempty"`
	Run           *RunOp              `json:"run,omitempty"`
	Logs          *LogsOp             `json:"logs,omitempty"`
	Delete        *DeleteOp           `json:"delete,omitempty"`
	Assert        []ResourceAssertion `json:"assert,omitempty"`
	Timeout       string              `json:"timeout,omitempty"`
	StopOnFailure *bool               `json:"stopOnFailure,omitempty"`
}

// OutputExpectation checks CLI output for deploy, plan, run, and delete.
// A missing exit code means 0.
type OutputExpectation struct {
	ExitCode *int         `json:"exitCode,omitempty"`
	Stdout   *StreamMatch `json:"stdout,omitempty"`
	Stderr   *StreamMatch `json:"stderr,omitempty"`
}

// StreamMatch lists Go regular expressions. Every pattern must match.
type StreamMatch struct {
	Contains []string `json:"contains"`
}

// DeployOp maps to `deployah deploy`.
type DeployOp struct {
	Spec   string             `json:"spec,omitempty"`
	Args   []string           `json:"args,omitempty"`
	Output *OutputExpectation `json:"output,omitempty"`
}

// PlanOp maps to `deployah plan` with JSON output.
type PlanOp struct {
	DetailedExitCode bool               `json:"detailedExitCode,omitempty"`
	Output           *OutputExpectation `json:"output,omitempty"`
	Expr             []string           `json:"expr,omitempty"`
}

// RunOp maps to `deployah run`.
type RunOp struct {
	Task   string             `json:"task"`
	Output *OutputExpectation `json:"output,omitempty"`
}

// LogsOp maps to `deployah logs`.
type LogsOp struct {
	Component string   `json:"component"`
	Contains  []string `json:"contains"`
}

// DeleteOp maps to `deployah delete`.
type DeleteOp struct {
	Output *OutputExpectation `json:"output,omitempty"`
}

// ResourceAssertion selects Kubernetes objects and checks them.
type ResourceAssertion struct {
	Select        ResourceSelect `json:"select"`
	Expect        map[string]any `json:"expect,omitempty"`
	Expr          []string       `json:"expr,omitempty"`
	StopOnFailure *bool          `json:"stopOnFailure,omitempty"`
}

// ResourceSelect names the objects to read. Fields are combined with AND.
type ResourceSelect struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Name       string            `json:"name,omitempty"`
	Namespace  string            `json:"namespace,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}

// ConfigError is one preflight failure with a YAML field path.
type ConfigError struct {
	File      string
	Step      int
	Assertion int
	Path      string
	Err       error
}

// Error reports the scenario file, step, assertion, field path, and cause.
func (e *ConfigError) Error() string {
	var b strings.Builder
	if e.File != "" {
		b.WriteString(e.File)
		b.WriteString(": ")
	}
	if e.Step >= 0 {
		fmt.Fprintf(&b, "steps[%d]", e.Step)
		if e.Assertion >= 0 {
			fmt.Fprintf(&b, " assert[%d]", e.Assertion)
		}
		if e.Path != "" {
			b.WriteString(".")
			b.WriteString(e.Path)
		}
		b.WriteString(": ")
	} else if e.Path != "" {
		b.WriteString(e.Path)
		b.WriteString(": ")
	}
	if e.Err != nil {
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

// Unwrap returns the underlying error.
func (e *ConfigError) Unwrap() error { return e.Err }

// RunParallel reports whether this fixture may run beside other fixtures.
// A missing value means yes.
func (f E2EFixture) RunParallel() bool {
	return f.Parallel == nil || *f.Parallel
}

// StopOnFailureEnabled reports the step failure policy. Nil means true.
func (s Step) StopOnFailureEnabled() bool {
	return s.StopOnFailure == nil || *s.StopOnFailure
}

// StopOnFailureEnabled reports the assertion failure policy. Nil means false.
func (a ResourceAssertion) StopOnFailureEnabled() bool {
	return a.StopOnFailure != nil && *a.StopOnFailure
}

// OpName returns the operation name, or "" if none or more than one is set.
func (s Step) OpName() string {
	n := 0
	name := ""
	if s.Deploy != nil {
		n++
		name = "deploy"
	}
	if s.Plan != nil {
		n++
		name = "plan"
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
	if len(s.Assert) > 0 {
		n++
		name = "assert"
	}
	if n != 1 {
		return ""
	}
	return name
}

// Output returns the operation's output expectation, or nil.
func (s Step) Output() *OutputExpectation {
	switch {
	case s.Deploy != nil:
		return s.Deploy.Output
	case s.Plan != nil:
		return s.Plan.Output
	case s.Run != nil:
		return s.Run.Output
	case s.Delete != nil:
		return s.Delete.Output
	default:
		return nil
	}
}

// LoadE2EFixture reads path, checks the schema, and checks images.
func LoadE2EFixture(path, scenarioDir string) (E2EFixture, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- scenario e2e.yaml under test
	if err != nil {
		return E2EFixture{}, fmt.Errorf("read e2e.yaml: %w", err)
	}
	jsonBytes, err := yaml.YAMLToJSON(raw)
	if err != nil {
		return E2EFixture{}, &ConfigError{File: path, Step: -1, Assertion: -1, Path: "", Err: fmt.Errorf("parse e2e.yaml: %w", err)}
	}
	var doc any
	if err = json.Unmarshal(jsonBytes, &doc); err != nil {
		return E2EFixture{}, &ConfigError{File: path, Step: -1, Assertion: -1, Err: fmt.Errorf("parse e2e.yaml: %w", err)}
	}
	if err = ValidateE2EDocument(doc); err != nil {
		return E2EFixture{}, &ConfigError{File: path, Step: -1, Assertion: -1, Err: err}
	}
	var fx E2EFixture
	if err = k8sjson.Unmarshal(jsonBytes, &fx); err != nil {
		return E2EFixture{}, &ConfigError{File: path, Step: -1, Assertion: -1, Err: fmt.Errorf("decode e2e.yaml: %w", err)}
	}
	if err = validateScenarioImages(scenarioDir, fx); err != nil {
		return E2EFixture{}, &ConfigError{File: path, Step: -1, Assertion: -1, Err: err}
	}
	return fx, nil
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
