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

	planengine "deployah.dev/deployah/internal/plan"
)

// Result is the output of [ComputeDrift].
type Result struct {
	// Changes lists per-resource drift: fields that differ between the
	// predicted apply and the resource's live state, but were not already
	// part of the spec-edit diff in the plan passed to [ComputeDrift].
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

// ComputeDrift predicts each resource in currentManifest via predictor and
// compares it against live state, subtracting field paths already explained
// by specPlan.Changes so only changes the cluster picked up outside of
// Deployah remain. On a fresh install (specPlan.Header.FreshInstall) it
// short-circuits to an empty, complete Result: there is no live baseline to
// compare against.
func ComputeDrift(ctx context.Context, predictor Predictor, specPlan *planengine.Plan, currentManifest string) (*Result, error) {
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
			// specPlan already reports this as "+ add"; if it also exists
			// live (e.g. an orphan from a failed release), every field
			// would look like unexplained drift on a resource Deployah
			// considers not-yet-existing. Skip it -- the add is the only
			// signal that matters.
			continue
		}
		predicted, live, predictErr := predictor.Predict(ctx, res.YAML)
		if predictErr != nil {
			// predictErr's text is surfaced verbatim: an RBAC denial from
			// the API server already names the missing verb and resource,
			// so there is nothing more useful to add here.
			result.Incomplete = append(result.Incomplete, fmt.Sprintf("%s: %s", res.Label, predictErr))
			continue
		}
		if live == "" {
			// Nothing live to compare against yet (e.g. a resource this
			// same plan would add): no baseline, no drift.
			continue
		}

		total, diffErr := planengine.ComputeDiff(predicted, live)
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

// addedLabels indexes specPlan's changes by resource label, returning the
// set of labels whose Action is ActionAdd: resources the spec-edit diff
// already reports as new, for which [ComputeDrift] must not report
// per-field drift even if the resource already exists live.
func addedLabels(specPlan *planengine.Plan) map[string]bool {
	out := make(map[string]bool)
	for _, c := range specPlan.Changes {
		if c.Action == planengine.ActionAdd {
			out[resourceLabel(c.Kind, c.Namespace, c.Name)] = true
		}
	}
	return out
}

// explainedPaths indexes specPlan's changes by resource label so
// [ComputeDrift] can subtract, per resource, the field paths the spec edit
// already reports.
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

// driftOnlyChange filters total (the predicted-vs-live diff for one
// resource) down to fields absent from explained, returning nil when
// nothing remains. total always has at most one entry: predicted and live
// describe the same resource identity, so [planengine.ComputeDiff] can only
// ever report it as changed or unchanged, never added or destroyed.
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

// resourceLabel matches the "Kind/name" / "Kind/namespace/name" format
// [planengine.SplitResources] uses for [planengine.ResourceYAML.Label].
func resourceLabel(kind, namespace, name string) string {
	if namespace == "" {
		return fmt.Sprintf("%s/%s", kind, name)
	}
	return fmt.Sprintf("%s/%s/%s", kind, namespace, name)
}
