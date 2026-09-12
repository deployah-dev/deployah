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

import "fmt"

// Action is the semantic operation for one [ResourceChange]. The zero
// value is invalid.
type Action int

const (
	// Create is a resource that would exist after apply and does not exist
	// live.
	Create Action = iota + 1
	// Update is an in-place write to a live resource.
	Update
	// Delete is a prune of a live resource.
	Delete
	// Recreate is a delete of the live object plus a write of a
	// replacement. Stage C stores the action; it does not generate it.
	Recreate
)

func (a Action) String() string {
	switch a {
	case Create:
		return "create"
	case Update:
		return "update"
	case Delete:
		return "delete"
	case Recreate:
		return "recreate"
	default:
		return fmt.Sprintf("Action(%d)", int(a))
	}
}

func (a Action) valid() bool {
	switch a {
	case Create, Update, Delete, Recreate:
		return true
	default:
		return false
	}
}

func actionRank(a Action) int {
	switch a {
	case Create:
		return 0
	case Update:
		return 1
	case Recreate:
		return 2
	case Delete:
		return 3
	default:
		return 4
	}
}

// Completeness says whether every predicted After is exact. The zero
// value is invalid.
type Completeness int

const (
	// CompletenessComplete means every resource change has an exact
	// prediction.
	CompletenessComplete Completeness = iota + 1
	// CompletenessPartial means at least one prediction is not exact.
	CompletenessPartial
)

func (c Completeness) String() string {
	switch c {
	case CompletenessComplete:
		return "complete"
	case CompletenessPartial:
		return "partial"
	default:
		return fmt.Sprintf("Completeness(%d)", int(c))
	}
}

// Header identifies the release a plan describes. Empty fields are valid
// in Stage C; later stages fill them for CLI display.
type Header struct {
	Project      string
	Environment  string
	Release      string
	Namespace    string
	Context      string
	Revision     int
	FreshInstall bool
}

// ResourceRef is Kubernetes identity for one change. APIVersion is a
// GroupVersion string (core is "v1"). GenerateName is set only when Name
// is empty.
type ResourceRef struct {
	APIVersion   string
	Kind         string
	Namespace    string
	Name         string
	GenerateName string
}

func (r ResourceRef) identityKey() string {
	return r.APIVersion + "\x00" + r.Kind + "\x00" + r.Namespace + "\x00" + r.Name + "\x00" + r.GenerateName
}

func (r ResourceRef) String() string {
	if r.Name == "" && r.GenerateName != "" {
		return r.Kind + "/" + r.Namespace + "/generateName=" + r.GenerateName
	}
	return r.Kind + "/" + r.Namespace + "/" + r.Name
}

// OriginKind classifies how a resource entered the plan. The zero value
// is invalid.
type OriginKind int

const (
	// OriginHelm is a Helm-predicted resource.
	OriginHelm OriginKind = iota + 1
)

func (k OriginKind) String() string {
	switch k {
	case OriginHelm:
		return "helm"
	default:
		return fmt.Sprintf("OriginKind(%d)", int(k))
	}
}

func (k OriginKind) valid() bool {
	return k == OriginHelm
}

// ResourceOrigin names the producer of a [ResourceChange]. Stage C only
// constructs Helm origins. Later stages can add fields without changing
// [ResourceChange].
type ResourceOrigin struct {
	Kind OriginKind
	Helm *HelmOrigin
}

// HelmOrigin is the Helm release that produced a resource change.
type HelmOrigin struct {
	Release   string
	Namespace string
}

// WriteMethod is how a replacement object is written. The zero value is
// invalid.
type WriteMethod int

const (
	// WriteServerSide is Kubernetes server-side apply.
	WriteServerSide WriteMethod = iota + 1
)

func (m WriteMethod) String() string {
	switch m {
	case WriteServerSide:
		return "server_side_apply"
	default:
		return fmt.Sprintf("WriteMethod(%d)", int(m))
	}
}

func (m WriteMethod) valid() bool {
	return m == WriteServerSide
}

// DeletePropagation is Kubernetes deletion propagation. The zero value
// is invalid.
type DeletePropagation int

const (
	// PropagationBackground is background deletion.
	PropagationBackground DeletePropagation = iota + 1
)

func (p DeletePropagation) String() string {
	switch p {
	case PropagationBackground:
		return "background"
	default:
		return fmt.Sprintf("DeletePropagation(%d)", int(p))
	}
}

func (p DeletePropagation) valid() bool {
	return p == PropagationBackground
}

// WriteSemantics is the write half of [ApplySemantics].
type WriteSemantics struct {
	Method         WriteMethod
	FieldManager   string
	ForceConflicts bool
}

// DeleteSemantics is the delete half of [ApplySemantics].
type DeleteSemantics struct {
	Propagation DeletePropagation
}

// ApplySemantics is the mutation semantics that produced a prediction.
// Create and Update set Write only. Delete sets Delete only. Recreate
// sets both.
type ApplySemantics struct {
	Write  *WriteSemantics
	Delete *DeleteSemantics
}

// ResourceSnapshot is one full unredacted Kubernetes object. Object is
// unstructured content, not a client type. It is not a JSON rendering
// contract.
type ResourceSnapshot struct {
	Object map[string]any
}

// Execution is a reserved slot for later task and hook runs. Stage C
// defines no fields and does not accept non-empty execution lists.
type Execution struct{}
