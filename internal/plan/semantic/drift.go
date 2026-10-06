// Copyright 2026 The Deployah Authors
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
	"cmp"
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// DriftAction is the observed Previous-to-Live state of one resource.
// The zero value is invalid. It is not a release action: there is no
// create, update, or delete vocabulary here.
type DriftAction int

const (
	// DriftModified means Previous and Live both exist and the declared
	// semantic surface differs.
	DriftModified DriftAction = iota + 1
	// DriftMissing means Previous exists and Live does not.
	DriftMissing
	// DriftUnexpected means a release-owned Live object has no Previous
	// logical identity.
	DriftUnexpected
)

func (a DriftAction) String() string {
	switch a {
	case DriftModified:
		return "modified"
	case DriftMissing:
		return "missing"
	case DriftUnexpected:
		return "unexpected"
	default:
		return fmt.Sprintf("DriftAction(%d)", int(a))
	}
}

func (a DriftAction) valid() bool {
	switch a {
	case DriftModified, DriftMissing, DriftUnexpected:
		return true
	default:
		return false
	}
}

func driftActionRank(a DriftAction) int {
	switch a {
	case DriftModified:
		return 0
	case DriftMissing:
		return 1
	case DriftUnexpected:
		return 2
	default:
		return 3
	}
}

// DriftChange is one difference between Previous and Live.
// [New] checks and stores it. It is cluster state, separate
// from a release change.
//
// Name is required, and GenerateName must be empty. Modified and
// Missing use the Previous declaration, including its apiVersion.
// Unexpected uses the Live object. An apiVersion change alone is not
// drift.
//
// Previous is unset for Unexpected and is never rewritten. Live is
// unset for Missing. Modified Live keeps only the declared surface.
// Unexpected Live has server bookkeeping removed. Only Modified has
// Fields. [New] keeps them and does not recompute them.
type DriftChange struct {
	Resource ResourceRef
	Action   DriftAction
	Previous *ResourceSnapshot
	Live     *ResourceSnapshot
	Fields   []FieldChange
}

// HasDrift reports whether the plan lists any drift. Drift is observed
// cluster state and does not change [Plan.HasEffects] or [Plan.IsNoOp].
func (p Plan) HasDrift() bool {
	return len(p.Drift) > 0
}

// validateDriftSet checks drift. A fresh install accepts only an empty
// collection. The same group, kind, namespace, and name twice is an
// error. Group comes from apiVersion.
func validateDriftSet(header Header, drift []DriftChange) error {
	if header.FreshInstall && len(drift) > 0 {
		return fmt.Errorf("fresh install must not include drift")
	}
	seen := make(map[string]struct{}, len(drift))
	for i := range drift {
		if err := validateDrift(drift[i]); err != nil {
			return fmt.Errorf("drift %d: %w", i, err)
		}
		key := driftIdentityKey(drift[i].Resource)
		if _, dup := seen[key]; dup {
			return fmt.Errorf("drift %s: duplicate logical identity", drift[i].Resource)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateDrift(d DriftChange) error {
	if !d.Action.valid() {
		return fmt.Errorf("invalid action %s", d.Action)
	}
	if d.Resource.Name == "" {
		return fmt.Errorf("name is required")
	}
	if d.Resource.GenerateName != "" {
		return fmt.Errorf("generateName must be empty")
	}
	switch d.Action {
	case DriftModified:
		if d.Previous == nil {
			return fmt.Errorf("modified requires a previous snapshot")
		}
		if d.Live == nil {
			return fmt.Errorf("modified requires a live snapshot")
		}
		if len(d.Fields) == 0 {
			return fmt.Errorf("modified requires a field change")
		}
	case DriftMissing:
		if d.Previous == nil {
			return fmt.Errorf("missing requires a previous snapshot")
		}
		if d.Live != nil {
			return fmt.Errorf("missing must not have a live snapshot")
		}
		if len(d.Fields) != 0 {
			return fmt.Errorf("missing must not have field changes")
		}
	case DriftUnexpected:
		if d.Previous != nil {
			return fmt.Errorf("unexpected must not have a previous snapshot")
		}
		if d.Live == nil {
			return fmt.Errorf("unexpected requires a live snapshot")
		}
		if len(d.Fields) != 0 {
			return fmt.Errorf("unexpected must not have field changes")
		}
	}
	return nil
}

func sortDrift(drift []DriftChange) {
	slices.SortFunc(drift, compareDrift)
	for i := range drift {
		slices.SortFunc(drift[i].Fields, func(a, b FieldChange) int {
			return cmp.Compare(a.Path, b.Path)
		})
	}
}

func compareDrift(a, b DriftChange) int {
	return cmp.Or(
		cmp.Compare(driftGroup(a.Resource.APIVersion), driftGroup(b.Resource.APIVersion)),
		cmp.Compare(a.Resource.Kind, b.Resource.Kind),
		cmp.Compare(a.Resource.Namespace, b.Resource.Namespace),
		cmp.Compare(a.Resource.Name, b.Resource.Name),
		cmp.Compare(a.Resource.APIVersion, b.Resource.APIVersion),
		cmp.Compare(driftActionRank(a.Action), driftActionRank(b.Action)),
	)
}

// driftIdentityKey is logical identity: group, kind, namespace, and
// name. apiVersion is not included.
func driftIdentityKey(r ResourceRef) string {
	return driftGroup(r.APIVersion) + "\x00" + r.Kind + "\x00" + r.Namespace + "\x00" + r.Name
}

func driftGroup(apiVersion string) string {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return ""
	}
	if gv.Group == "core" {
		return ""
	}
	return gv.Group
}

func copyDrift(in []DriftChange) []DriftChange {
	if in == nil {
		return []DriftChange{}
	}
	out := slices.Clone(in)
	for i := range out {
		out[i].Previous = copySnapshot(out[i].Previous)
		out[i].Live = copySnapshot(out[i].Live)
		out[i].Fields = copyFields(out[i].Fields)
		if out[i].Fields == nil {
			out[i].Fields = []FieldChange{}
		}
	}
	return out
}
