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
)

func (a Action) String() string {
	switch a {
	case Create:
		return "create"
	case Update:
		return "update"
	case Delete:
		return "delete"
	default:
		return fmt.Sprintf("Action(%d)", int(a))
	}
}

func (a Action) valid() bool {
	switch a {
	case Create, Update, Delete:
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
	case Delete:
		return 2
	default:
		return 3
	}
}

// HelmAction is whether Helm will install, upgrade, or do neither. The
// zero value is invalid.
type HelmAction int

const (
	// HelmNone means Helm will not execute. OriginHelm changes and
	// changed or WillRun preDeploy/postDeploy tasks are invalid.
	HelmNone HelmAction = iota + 1
	// HelmInstall is a fresh Helm install. It requires
	// [Header.FreshInstall].
	HelmInstall
	// HelmUpgrade is a Helm upgrade. Zero resource consequences remain
	// valid when release intent changed and Live already matches Desired.
	HelmUpgrade
)

func (a HelmAction) String() string {
	switch a {
	case HelmNone:
		return "none"
	case HelmInstall:
		return "install"
	case HelmUpgrade:
		return "upgrade"
	default:
		return fmt.Sprintf("HelmAction(%d)", int(a))
	}
}

func (a HelmAction) valid() bool {
	switch a {
	case HelmNone, HelmInstall, HelmUpgrade:
		return true
	default:
		return false
	}
}

// Header identifies the release a plan describes. Empty fields are valid;
// callers fill them for display.
type Header struct {
	Project     string
	Environment string
	Release     string
	Namespace   string
	Context     string
	// Revision is the Helm revision this plan would create. It is 1
	// for a fresh install and ReleasePrep.NextRevision for an upgrade.
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
	// OriginHelm is a resource rendered by the Helm release.
	OriginHelm OriginKind = iota + 1
	// OriginNamespace is the target Namespace created as part of a
	// Helm install. It is invalid with [HelmNone] or [HelmUpgrade].
	OriginNamespace
)

func (k OriginKind) String() string {
	switch k {
	case OriginHelm:
		return "helm"
	case OriginNamespace:
		return "namespace"
	default:
		return fmt.Sprintf("OriginKind(%d)", int(k))
	}
}

func (k OriginKind) valid() bool {
	switch k {
	case OriginHelm, OriginNamespace:
		return true
	default:
		return false
	}
}

// ResourceOrigin names the producer of a [ResourceChange]. Helm details
// are required for [OriginHelm] and forbidden for [OriginNamespace].
type ResourceOrigin struct {
	Kind OriginKind
	Helm *HelmOrigin
}

// HelmOrigin is the Helm release that produced a resource change.
type HelmOrigin struct {
	Release   string
	Namespace string
}

// WriteMethod is the write method on [WriteSemantics]. The zero value is
// invalid.
type WriteMethod int

const (
	// WriteCreate is Kubernetes create. FieldManager must be empty and
	// ForceConflicts must be false.
	WriteCreate WriteMethod = iota + 1
	// WriteServerSide is Kubernetes server-side apply. FieldManager
	// must be non-empty. ForceConflicts may be true or false.
	WriteServerSide
)

func (m WriteMethod) String() string {
	switch m {
	case WriteCreate:
		return "create"
	case WriteServerSide:
		return "server_side_apply"
	default:
		return fmt.Sprintf("WriteMethod(%d)", int(m))
	}
}

func (m WriteMethod) valid() bool {
	switch m {
	case WriteCreate, WriteServerSide:
		return true
	default:
		return false
	}
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

// ApplySemantics is the mutation semantics of a resource consequence.
// Create and Update set Write only. Delete sets Delete only.
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

// TaskPhase is when a [TaskPlan] belongs in a deploy. The zero value is
// invalid.
type TaskPhase int

const (
	// TaskPreDeploy is a preDeploy hook task.
	TaskPreDeploy TaskPhase = iota + 1
	// TaskPostDeploy is a postDeploy hook task.
	TaskPostDeploy
	// TaskSchedule is a scheduled task whose backing objects are normal
	// Helm Manifest resources.
	TaskSchedule
)

func (p TaskPhase) String() string {
	switch p {
	case TaskPreDeploy:
		return "preDeploy"
	case TaskPostDeploy:
		return "postDeploy"
	case TaskSchedule:
		return "schedule"
	default:
		return fmt.Sprintf("TaskPhase(%d)", int(p))
	}
}

func (p TaskPhase) valid() bool {
	switch p {
	case TaskPreDeploy, TaskPostDeploy, TaskSchedule:
		return true
	default:
		return false
	}
}

func (p TaskPhase) rank() int {
	switch p {
	case TaskPreDeploy:
		return 0
	case TaskPostDeploy:
		return 1
	case TaskSchedule:
		return 2
	default:
		return 3
	}
}

// TaskAction is the definition-level action for a [TaskPlan]. The zero
// value is invalid. It is independent of [TaskPlan.WillRun].
type TaskAction int

const (
	// TaskUnchanged means the task still exists and its definition did
	// not change.
	TaskUnchanged TaskAction = iota + 1
	// TaskCreate means the task is new in the desired spec.
	TaskCreate
	// TaskUpdate means the task still exists and at least one rendered
	// definition or referenced resource changed.
	TaskUpdate
	// TaskDelete means the task was removed. WillRun is always false.
	TaskDelete
)

func (a TaskAction) String() string {
	switch a {
	case TaskUnchanged:
		return "unchanged"
	case TaskCreate:
		return "create"
	case TaskUpdate:
		return "update"
	case TaskDelete:
		return "delete"
	default:
		return fmt.Sprintf("TaskAction(%d)", int(a))
	}
}

func (a TaskAction) valid() bool {
	switch a {
	case TaskUnchanged, TaskCreate, TaskUpdate, TaskDelete:
		return true
	default:
		return false
	}
}

// TaskPlan is one Deployah task in a [Plan]. preDeploy and postDeploy
// carry [HookDefinition] diffs. schedule references [ResourceChange]
// values in Plan.Changes. A current-only manual task is omitted. A
// current manual task whose previous release was preDeploy, postDeploy,
// or schedule is represented as a deletion of that previous footprint.
// Changing on between schedule and preDeploy or postDeploy is a
// [TaskUpdate] of the current phase. The previous CronJob stays in
// Plan.Changes; previous hook documents are not emitted as Kubernetes
// deletes.
type TaskPlan struct {
	Name        string
	Phase       TaskPhase
	Action      TaskAction
	WillRun     bool
	Definitions []HookDefinition
	Resources   []ResourceRef
	// HookWeight is the resolved Helm hook-weight used to sort preDeploy
	// and postDeploy tasks. It is not a JSON field.
	HookWeight int
}

// HookDefinition is a rendered Helm hook document difference for a
// preDeploy or postDeploy task. It is not a live Kubernetes apply or
// prune. It has no [ApplySemantics] and no [ResourceOrigin].
type HookDefinition struct {
	Resource ResourceRef
	Action   Action
	Before   *ResourceSnapshot
	After    *ResourceSnapshot
	Fields   []FieldChange
	// HookWeight is the Helm hook-weight of this document. [New] sorts
	// definitions by HookWeight then identity. It is not a JSON field.
	HookWeight int
}

func (d HookDefinition) definitionActionValid() bool {
	switch d.Action {
	case Create, Update, Delete:
		return true
	default:
		return false
	}
}

// ChartCRDLifecycle is Helm's install-only handling of one chart CRD
// document. The zero value is invalid.
type ChartCRDLifecycle int

const (
	// ChartCRDProcess means a fresh install will ask Helm to process the
	// chart CRD. It does not claim Kubernetes will create versus apply.
	ChartCRDProcess ChartCRDLifecycle = iota + 1
	// ChartCRDSkip means the CRD is in the chart but install-time
	// processing is disabled.
	ChartCRDSkip
	// ChartCRDUpgrade means the CRD is in the chart and Helm Upgrade will
	// not process it.
	ChartCRDUpgrade
)

func (l ChartCRDLifecycle) String() string {
	switch l {
	case ChartCRDProcess:
		return "process"
	case ChartCRDSkip:
		return "skip"
	case ChartCRDUpgrade:
		return "upgrade"
	default:
		return fmt.Sprintf("ChartCRDLifecycle(%d)", int(l))
	}
}

func (l ChartCRDLifecycle) valid() bool {
	switch l {
	case ChartCRDProcess, ChartCRDSkip, ChartCRDUpgrade:
		return true
	default:
		return false
	}
}

// ChartCRD is one chart CRD document for plan presentation. It is Helm
// chart-CRD lifecycle, not a resource consequence.
type ChartCRD struct {
	// Source is the display path under .deployah/crds/.
	Source string
	// Index is the 0-based position among non-empty YAML documents in that
	// file.
	Index int
	// Kind is always CustomResourceDefinition.
	Kind string
	// Name is metadata.name from the source document.
	Name string
	// Lifecycle is Helm's handling of this document in this invocation.
	Lifecycle ChartCRDLifecycle
	// WillProcess is true only when Lifecycle is [ChartCRDProcess].
	WillProcess bool
}
