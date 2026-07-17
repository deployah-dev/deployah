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

package plan

import "errors"

// ErrChangesPresent is returned by the `deployah plan` command when
// --detailed-exitcode is set and the computed plan has at least one change.
// internal/cmd/root.go checks for it with [errors.Is] to translate it into
// exit code 2 instead of the generic exit code 1.
var ErrChangesPresent = errors.New("plan has pending changes")

// Action classifies how a resource changes between the previous and current
// manifest.
type Action string

const (
	// ActionAdd means the resource exists in the current manifest but not in
	// the previous one.
	ActionAdd Action = "add"
	// ActionChange means the resource exists on both sides with at least one
	// field difference.
	ActionChange Action = "change"
	// ActionDestroy means the resource exists in the previous manifest but
	// not in the current one.
	ActionDestroy Action = "destroy"
)

// FieldChangeKind classifies one field-level difference within a resource.
type FieldChangeKind string

const (
	// FieldAdded means the field is present in the current resource only.
	FieldAdded FieldChangeKind = "added"
	// FieldChanged means the field's value differs between the two resources.
	FieldChanged FieldChangeKind = "changed"
	// FieldRemoved means the field is present in the previous resource only.
	FieldRemoved FieldChangeKind = "removed"
)

// FieldDiff is one field-level difference inside a [Change], using dyff's
// dot-style, name-keyed path notation. Old is meaningless when ChangeKind
// is [FieldAdded], New when it's [FieldRemoved]. When Masked is true, Old
// and New still hold the real values (for --show-secrets); the JSON
// renderer must always omit them regardless.
type FieldDiff struct {
	Path       string
	ChangeKind FieldChangeKind
	Old        string
	New        string
	Masked     bool
	// Segments is Path broken into its structured parts, so a renderer can
	// reconstruct the real nested manifest shape (ModeYAML) instead of
	// working from the flattened dot string. Derived from dyff's own
	// ytbx.PathElement list, which is the only source that distinguishes a
	// plain map key from a named list-entry identifier -- the flattened
	// Path string alone cannot tell "containers.web" apart from a map with
	// a literal key "web".
	Segments []PathSegment
}

// PathSegment is one element of a [FieldDiff]'s structured path.
type PathSegment struct {
	// Name is the map key, or -- when ListKey is set -- the value that
	// identifies one entry in a named-entry list (e.g. "web" in a
	// container list entry matched by its "name" field).
	Name string
	// ListKey is the identifying field name for a named-entry list item
	// (almost always "name" for Kubernetes; occasionally "key" or another
	// field dyff detected as unique). Empty for a plain map key or a
	// positional list index.
	ListKey string
	// Idx is the positional index into a list whose entries dyff could not
	// match by identity (e.g. a plain string list like `command`). -1 for
	// a map key or a named list-entry segment.
	Idx int
}

// Change describes one Kubernetes resource that differs between the
// previous and current manifest.
type Change struct {
	Action     Action
	Kind       string
	APIVersion string
	Name       string
	Namespace  string
	// Fields is empty for [ActionAdd] and [ActionDestroy]; the whole
	// resource is the change in those cases.
	Fields []FieldDiff
}

// Summary counts changes by action for the trailer line ("Plan: 1 to add, ...").
type Summary struct {
	Add     int
	Change  int
	Destroy int
}

// Total returns the total number of changed resources.
func (s Summary) Total() int {
	return s.Add + s.Change + s.Destroy
}

// Header carries the identifying and contextual information shown above the
// list of changes: which project, environment, release, and cluster this
// plan describes.
type Header struct {
	Project     string
	Environment string
	Release     string
	Namespace   string
	Context     string

	// Revision is the current (last successful) release revision. It is
	// meaningless when FreshInstall is true.
	Revision int
	// FreshInstall is true when no prior successful release exists, so
	// every resource in the current manifest renders as an addition.
	FreshInstall bool
	// Warning is a non-fatal note about the release history, e.g. that the
	// latest revision failed or is pending and the plan compares against an
	// older successful revision instead.
	Warning string
}

// Plan is the full result of comparing a previous manifest (the last
// successful release, or none on a fresh install) against a freshly
// rendered current manifest.
type Plan struct {
	Header  Header
	Changes []Change
	Summary Summary
	// HooksChanged is true when the release's Helm hooks differ between the
	// previous and current render but are not otherwise represented in
	// Changes (hooks are not regular cluster resources tracked by the diff).
	HooksChanged bool

	// DriftChecked is true when `--drift` ran, regardless of outcome, so
	// renderers can tell "checked, found nothing" from "not requested" even
	// though Drift is empty in both cases.
	DriftChecked bool
	// Drift lists fields that differ between a server-side apply
	// prediction and a resource's live state, but are not already
	// explained by Changes. See deployah.dev/deployah/internal/drift.
	Drift []Change
	// DriftIncomplete lists resource labels drift could not be checked for
	// (e.g. missing RBAC), so the plan can say it is incomplete instead of
	// silently omitting them.
	DriftIncomplete []string
}

// HasChanges reports whether applying this plan would change the cluster:
// any resource-level change, or a hook-only change.
func (p *Plan) HasChanges() bool {
	if p == nil {
		return false
	}
	return len(p.Changes) > 0 || p.HooksChanged
}
