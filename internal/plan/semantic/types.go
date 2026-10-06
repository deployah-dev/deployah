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
	// Create is a resource present in Desired and absent from Previous.
	Create Action = iota + 1
	// Update is a resource present in both Previous and Desired whose
	// declared content differs.
	Update
	// Delete means the resource leaves Desired release state. It does not
	// claim that Helm or Kubernetes will delete the live object.
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

// HelmAction is the Helm release-intent transition a semantic plan
// describes. It does not control whether deployah deploy invokes Helm.
// The zero value is invalid.
type HelmAction int

const (
	// HelmNone means the release intent did not change. The plan must not
	// include resource changes. preDeploy and postDeploy tasks must be
	// unchanged, and WillRun must be false. HelmNone does not stop
	// `deployah deploy` from running Helm.
	HelmNone HelmAction = iota + 1
	// HelmInstall means the release is new. [Header.FreshInstall] must be
	// true.
	HelmInstall
	// HelmUpgrade means an existing release changes. The plan can have no
	// resource changes when only hooks changed, or when the manifests
	// differ only in encoding.
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

// ResourceRef names one resource in a change. APIVersion comes from
// the object this change uses. Core resources use "v1". Namespace is the
// effective namespace used to pair Previous and Desired. It is not
// always the namespace written in the snapshot. GenerateName is set
// only when Name is empty.
//
// Sorting and task links compare every field, including APIVersion and
// GenerateName. That comparison is not logical identity. Logical
// identity ignores APIVersion and does not use GenerateName.
type ResourceRef struct {
	APIVersion   string
	Kind         string
	Namespace    string
	Name         string
	GenerateName string
}

// refKey is an exact equality key for this reference. It includes
// APIVersion and GenerateName, so it is not logical resource identity.
func (r ResourceRef) refKey() string {
	return r.APIVersion + "\x00" + r.Kind + "\x00" + r.Namespace + "\x00" + r.Name + "\x00" + r.GenerateName
}

func (r ResourceRef) String() string {
	if r.Name == "" && r.GenerateName != "" {
		return r.Kind + "/" + r.Namespace + "/generateName=" + r.GenerateName
	}
	return r.Kind + "/" + r.Namespace + "/" + r.Name
}

// ResourceSnapshot is one Kubernetes object, with secret values still
// present. Object is a plain map, not a typed client object. It is not
// the JSON document the plan view writes.
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

// TaskPlan is one Deployah task in a [Plan].
//
// A preDeploy or postDeploy task carries [HookDefinition] diffs. A
// schedule task points at [ResourceChange] entries in [Plan.Changes].
//
// A manual task that exists only in the current spec is left out. If
// the task is manual now, but was preDeploy, postDeploy, or schedule
// in the previous release, the plan shows that old hook or schedule
// as removed.
//
// Moving between schedule and preDeploy or postDeploy is a
// [TaskUpdate] of the phase the task has now. The old CronJob stays
// in [Plan.Changes]. Old hook documents are not listed as deletes.
type TaskPlan struct {
	Name   string
	Phase  TaskPhase
	Action TaskAction
	// WillRun reports whether this task's hook takes part in the Helm
	// transition. [New] sets it and ignores a caller value. Schedule
	// and delete tasks do not run. Other preDeploy and postDeploy
	// tasks run for [HelmInstall] and [HelmUpgrade], and do not run
	// for [HelmNone]. It does not predict whether a later
	// `deployah deploy` skips the hook.
	WillRun     bool
	Definitions []HookDefinition
	Resources   []ResourceRef
	// HookWeight is the resolved Helm hook-weight used to sort preDeploy
	// and postDeploy tasks. It is not a JSON field.
	HookWeight int
}

// HookDefinition is a rendered Helm hook document difference for a
// preDeploy or postDeploy task. It compares Previous and Desired hook
// declarations. It is not a live Kubernetes apply or prune.
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
	// A true value contributes to [Plan.HasEffects].
	WillProcess bool
}
