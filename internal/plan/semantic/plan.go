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

package semantic

import (
	"fmt"
	"slices"
)

// Plan is the Live -> Predicted semantic result. Construct it with [New].
// Snapshots and field values are unredacted. This type is not a JSON
// rendering contract.
type Plan struct {
	Header       Header
	Changes      []ResourceChange
	Executions   []Execution
	Diagnostics  []Diagnostic
	Summary      Summary
	Completeness Completeness
}

// New validates changes and diagnostics, sorts them, derives [Summary]
// and [Completeness], and returns a plan whose Executions slice is
// non-nil and empty.
func New(header Header, changes []ResourceChange, diagnostics []Diagnostic) (Plan, error) {
	copiedChanges := slices.Clone(changes)
	copiedDiags := slices.Clone(diagnostics)

	for i := range copiedDiags {
		if err := validateDiagnostic(copiedDiags[i]); err != nil {
			return Plan{}, err
		}
	}
	for i := range copiedChanges {
		if err := validateChange(copiedChanges[i], copiedDiags); err != nil {
			return Plan{}, fmt.Errorf("resource %s: %w", copiedChanges[i].Resource, err)
		}
		copiedChanges[i] = normalizeChange(copiedChanges[i])
	}

	sortChanges(copiedChanges)
	sortDiagnostics(copiedDiags)

	return Plan{
		Header:       header,
		Changes:      copiedChanges,
		Executions:   []Execution{},
		Diagnostics:  copiedDiags,
		Summary:      Summarize(copiedChanges),
		Completeness: deriveCompleteness(copiedChanges, copiedDiags),
	}, nil
}

func normalizeChange(c ResourceChange) ResourceChange {
	c.Before = copySnapshot(c.Before)
	c.After = copySnapshot(c.After)
	switch {
	case (c.Action == Update || c.Action == Recreate) && c.After != nil:
		c.Fields = DiffFields(snapshotObject(c.Before), snapshotObject(c.After))
	default:
		c.Fields = nil
	}
	return c
}

func deriveCompleteness(changes []ResourceChange, diags []Diagnostic) Completeness {
	for _, d := range diags {
		if d.Category == CategoryPredictionLimitation {
			return CompletenessPartial
		}
	}
	for _, c := range changes {
		if c.Action == Update && c.After == nil {
			return CompletenessPartial
		}
	}
	return CompletenessComplete
}

func validateChange(c ResourceChange, diags []Diagnostic) error {
	if !c.Action.valid() {
		return fmt.Errorf("invalid action %s", c.Action)
	}
	if err := validateOrigin(c.Origin); err != nil {
		return err
	}
	if err := validateApply(c.Action, c.Apply); err != nil {
		return err
	}
	return validateSnapshots(c, diags)
}

func validateOrigin(o ResourceOrigin) error {
	if !o.Kind.valid() {
		return fmt.Errorf("invalid origin %s", o.Kind)
	}
	if o.Kind == OriginHelm && o.Helm == nil {
		return fmt.Errorf("helm origin requires helm details")
	}
	return nil
}

func validateApply(action Action, apply ApplySemantics) error {
	switch action {
	case Create, Update:
		if apply.Write == nil {
			return fmt.Errorf("%s requires write semantics", action)
		}
		if apply.Delete != nil {
			return fmt.Errorf("%s must not have delete semantics", action)
		}
		return validateWrite(*apply.Write)
	case Delete:
		if apply.Write != nil {
			return fmt.Errorf("delete must not have write semantics")
		}
		if apply.Delete == nil {
			return fmt.Errorf("delete requires delete semantics")
		}
		return validateDelete(*apply.Delete)
	case Recreate:
		if apply.Write == nil {
			return fmt.Errorf("recreate requires write semantics")
		}
		if apply.Delete == nil {
			return fmt.Errorf("recreate requires delete semantics")
		}
		if err := validateWrite(*apply.Write); err != nil {
			return err
		}
		return validateDelete(*apply.Delete)
	default:
		return fmt.Errorf("invalid action %s", action)
	}
}

func validateWrite(w WriteSemantics) error {
	if !w.Method.valid() {
		return fmt.Errorf("invalid write method %s", w.Method)
	}
	return nil
}

func validateDelete(d DeleteSemantics) error {
	if !d.Propagation.valid() {
		return fmt.Errorf("invalid delete propagation %s", d.Propagation)
	}
	return nil
}

func validateSnapshots(c ResourceChange, diags []Diagnostic) error {
	switch c.Action {
	case Create:
		if c.Before != nil {
			return fmt.Errorf("create must not have a before snapshot")
		}
		if c.After == nil {
			return fmt.Errorf("create requires an after snapshot")
		}
	case Update:
		if c.Before == nil {
			return fmt.Errorf("update requires a before snapshot")
		}
		if c.After == nil && !hasLimitation(c.Resource, diags) {
			return fmt.Errorf("update without after requires a prediction-limitation diagnostic")
		}
	case Delete:
		if c.Before == nil {
			return fmt.Errorf("delete requires a before snapshot")
		}
		if c.After != nil {
			return fmt.Errorf("delete must not have an after snapshot")
		}
	case Recreate:
		if c.Before == nil {
			return fmt.Errorf("recreate requires a before snapshot")
		}
		if c.After == nil {
			return fmt.Errorf("recreate requires an after snapshot")
		}
	}
	return nil
}

func hasLimitation(ref ResourceRef, diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Category != CategoryPredictionLimitation || d.Resource == nil {
			continue
		}
		if *d.Resource == ref {
			return true
		}
	}
	return false
}

func validateDiagnostic(d Diagnostic) error {
	if !d.Severity.valid() {
		return fmt.Errorf("invalid diagnostic severity %s", d.Severity)
	}
	if !d.Category.valid() {
		return fmt.Errorf("invalid diagnostic category %s", d.Category)
	}
	if d.Message == "" {
		return fmt.Errorf("diagnostic message is required")
	}
	return nil
}
