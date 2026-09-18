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

package drift

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"deployah.dev/deployah/internal/plan/semantic"

	planengine "deployah.dev/deployah/internal/plan"
	sigsyaml "sigs.k8s.io/yaml"
)

// Result is the output of [ComputeDrift].
type Result struct {
	// Changes lists per-resource drift: fields that differ between the
	// Desired declared surface and Live, but were not already part of
	// the spec-edit diff in the plan passed to [ComputeDrift].
	Changes []planengine.Change
	// Incomplete lists resource labels ("Kind/name" or
	// "Kind/namespace/name") that drift could not be checked for, e.g.
	// because of missing RBAC. A non-empty Incomplete means the plan is
	// partial and must say so rather than silently omit those resources.
	Incomplete []string
}

// HasDrift reports whether r found any drift.
func (r *Result) HasDrift() bool {
	return r != nil && len(r.Changes) > 0
}

// LiveReader returns the live YAML for one rendered resource. live is ""
// with a nil error when the resource does not exist yet (not a failure).
type LiveReader interface {
	Live(ctx context.Context, resourceYAML string) (live string, err error)
}

// ComputeDrift GETs each resource in currentManifest, projects Live onto
// Desired's declared surface, diffs, and subtracts field paths already
// explained by specPlan.Changes. On a fresh install (specPlan.Header.FreshInstall)
// it short-circuits to an empty, complete Result: there is no live
// baseline to compare against. It does not take a Previous manifest.
func ComputeDrift(ctx context.Context, live LiveReader, specPlan *planengine.Plan, currentManifest string) (*Result, error) {
	if specPlan.Header.FreshInstall {
		return &Result{}, nil
	}

	resources, err := planengine.SplitResources(currentManifest)
	if err != nil {
		return nil, fmt.Errorf("split rendered manifest: %w", err)
	}

	explained := explainedPaths(specPlan)
	adding := addedLabels(specPlan)

	result := &Result{}
	for _, res := range resources {
		if adding[res.Label] {
			continue
		}
		liveYAML, liveErr := live.Live(ctx, res.YAML)
		if liveErr != nil {
			result.Incomplete = append(result.Incomplete, fmt.Sprintf("%s: %s", res.Label, liveErr))
			continue
		}
		if liveYAML == "" {
			continue
		}

		desiredObj, decErr := decodeUnstructured(res.YAML)
		if decErr != nil {
			result.Incomplete = append(result.Incomplete, fmt.Sprintf("%s: %s", res.Label, decErr))
			continue
		}
		liveObj, decErr := decodeUnstructured(liveYAML)
		if decErr != nil {
			result.Incomplete = append(result.Incomplete, fmt.Sprintf("%s: %s", res.Label, decErr))
			continue
		}
		projected, projErr := semantic.ProjectOntoDeclared(liveObj.Object, desiredObj.Object)
		if projErr != nil {
			result.Incomplete = append(result.Incomplete, fmt.Sprintf("%s: %s", res.Label, projErr))
			continue
		}
		desiredYAML, encErr := toYAML(desiredObj)
		if encErr != nil {
			result.Incomplete = append(result.Incomplete, fmt.Sprintf("%s: %s", res.Label, encErr))
			continue
		}
		projectedYAML, encErr := toYAML(&unstructured.Unstructured{Object: projected})
		if encErr != nil {
			result.Incomplete = append(result.Incomplete, fmt.Sprintf("%s: %s", res.Label, encErr))
			continue
		}

		total, diffErr := planengine.ComputeDiff(desiredYAML, projectedYAML)
		if diffErr != nil {
			result.Incomplete = append(result.Incomplete, fmt.Sprintf("%s: %s", res.Label, diffErr))
			continue
		}

		if change := driftOnlyChange(total, explained[res.Label]); change != nil {
			result.Changes = append(result.Changes, *change)
		}
	}
	return result, nil
}

func decodeUnstructured(raw string) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	if err := sigsyaml.Unmarshal([]byte(raw), &obj.Object); err != nil {
		return nil, fmt.Errorf("decode resource: %w", err)
	}
	return obj, nil
}

func addedLabels(specPlan *planengine.Plan) map[string]bool {
	out := make(map[string]bool)
	for _, c := range specPlan.Changes {
		if c.Action == planengine.ActionAdd {
			out[resourceLabel(c.Kind, c.Namespace, c.Name)] = true
		}
	}
	return out
}

func explainedPaths(specPlan *planengine.Plan) map[string]map[string]struct{} {
	out := make(map[string]map[string]struct{}, len(specPlan.Changes))
	for _, c := range specPlan.Changes {
		paths := make(map[string]struct{}, len(c.Fields))
		for _, f := range c.Fields {
			paths[f.Path] = struct{}{}
		}
		out[resourceLabel(c.Kind, c.Namespace, c.Name)] = paths
	}
	return out
}

func driftOnlyChange(total *planengine.Plan, explained map[string]struct{}) *planengine.Change {
	if len(total.Changes) == 0 {
		return nil
	}
	c := total.Changes[0]
	remaining := make([]planengine.FieldDiff, 0, len(c.Fields))
	for _, f := range c.Fields {
		if _, ok := explained[f.Path]; ok {
			continue
		}
		remaining = append(remaining, f)
	}
	if len(remaining) == 0 {
		return nil
	}
	c.Fields = remaining
	return &c
}

func resourceLabel(kind, namespace, name string) string {
	if namespace == "" {
		return fmt.Sprintf("%s/%s", kind, name)
	}
	return fmt.Sprintf("%s/%s/%s", kind, namespace, name)
}
