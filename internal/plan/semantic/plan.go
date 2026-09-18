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

// Plan is the Previous/Live/Desired semantic result. Construct it with
// [New]. Snapshots and field values are unredacted. This type is not a
// JSON rendering contract.
type Plan struct {
	Header     Header
	HelmAction HelmAction
	Changes    []ResourceChange
	Drift      []ResourceDrift
	Tasks      []TaskPlan
	Summary    Summary
}

// New validates helmAction, header, changes, drift, and tasks, sorts
// them, derives [Summary], and returns a plan whose slices are
// non-nil. Task references to [ResourceChange] values must be
// consistent; it fails closed on dangling or duplicate ownership.
// helmAction is stored as provided; [New] does not derive or mutate it.
func New(header Header, helmAction HelmAction, changes []ResourceChange, tasks []TaskPlan, drift []ResourceDrift) (Plan, error) {
	if !helmAction.valid() {
		return Plan{}, fmt.Errorf("invalid helm action %s", helmAction)
	}
	if err := validateHelmAction(header, helmAction); err != nil {
		return Plan{}, err
	}

	copiedChanges := slices.Clone(changes)
	if copiedChanges == nil {
		copiedChanges = []ResourceChange{}
	}
	copiedTasks := copyTasks(tasks)
	copiedDrift := copyDrift(drift)
	if copiedDrift == nil {
		copiedDrift = []ResourceDrift{}
	}

	for i := range copiedChanges {
		if err := validateChange(copiedChanges[i]); err != nil {
			return Plan{}, fmt.Errorf("resource %s: %w", copiedChanges[i].Resource, err)
		}
		normalized, nerr := normalizeChange(copiedChanges[i])
		if nerr != nil {
			return Plan{}, fmt.Errorf("resource %s: %w", copiedChanges[i].Resource, nerr)
		}
		copiedChanges[i] = normalized
	}
	for i := range copiedDrift {
		if err := validateDrift(copiedDrift[i]); err != nil {
			return Plan{}, fmt.Errorf("drift %s: %w", copiedDrift[i].Resource, err)
		}
		copiedDrift[i].Fields = copyFields(copiedDrift[i].Fields)
		if copiedDrift[i].Fields == nil {
			copiedDrift[i].Fields = []FieldChange{}
		}
	}
	for i := range copiedTasks {
		normalized, nerr := normalizeTask(copiedTasks[i])
		if nerr != nil {
			return Plan{}, fmt.Errorf("task %s: %w", copiedTasks[i].Name, nerr)
		}
		copiedTasks[i] = normalized
	}
	if err := validateTasks(copiedTasks, copiedChanges); err != nil {
		return Plan{}, err
	}
	if err := validateHelmContent(helmAction, copiedChanges, copiedTasks); err != nil {
		return Plan{}, err
	}

	sortChanges(copiedChanges)
	sortTasks(copiedTasks, copiedChanges)
	sortDrift(copiedDrift)

	return Plan{
		Header:     header,
		HelmAction: helmAction,
		Changes:    copiedChanges,
		Drift:      copiedDrift,
		Tasks:      copiedTasks,
		Summary:    Summarize(copiedChanges),
	}, nil
}

// HasEffects reports whether the plan lists a known resource mutation or
// a task that would change or run. Drift and [HelmAction] are not
// effects.
func (p Plan) HasEffects() bool {
	if len(p.Changes) > 0 {
		return true
	}
	for _, t := range p.Tasks {
		if t.Action != TaskUnchanged || t.WillRun {
			return true
		}
	}
	return false
}

// IsNoOp reports whether the plan is a non-install HelmNone with no
// known effects. Drift is ignored. Keep this expression verbatim.
func (p Plan) IsNoOp() bool {
	return !p.Header.FreshInstall && p.HelmAction == HelmNone && !p.HasEffects()
}

func normalizeChange(c ResourceChange) (ResourceChange, error) {
	c.Before = copySnapshot(c.Before)
	c.After = copySnapshot(c.After)
	c.Origin = copyOrigin(c.Origin)
	c.Apply = copyApply(c.Apply)
	c.Fields = copyFields(c.Fields)
	switch {
	case (c.Action == Update || c.Action == Replace) && c.After != nil:
		fields, err := DiffFields(snapshotObject(c.Before), snapshotObject(c.After))
		if err != nil {
			return ResourceChange{}, err
		}
		c.Fields = fields
	default:
		c.Fields = []FieldChange{}
	}
	if c.Fields == nil {
		c.Fields = []FieldChange{}
	}
	return c, nil
}

func normalizeTask(t TaskPlan) (TaskPlan, error) {
	if t.Definitions == nil {
		t.Definitions = []HookDefinition{}
	}
	if t.Resources == nil {
		t.Resources = []ResourceRef{}
	}
	for i := range t.Definitions {
		normalized, err := normalizeDefinition(t.Definitions[i])
		if err != nil {
			return TaskPlan{}, fmt.Errorf("definition %s: %w", t.Definitions[i].Resource, err)
		}
		t.Definitions[i] = normalized
	}
	return t, nil
}

func normalizeDefinition(d HookDefinition) (HookDefinition, error) {
	d.Before = copySnapshot(d.Before)
	d.After = copySnapshot(d.After)
	d.Fields = copyFields(d.Fields)
	switch {
	case d.Action == Update && d.After != nil:
		fields, err := DiffFields(snapshotObject(d.Before), snapshotObject(d.After))
		if err != nil {
			return HookDefinition{}, err
		}
		d.Fields = fields
	default:
		d.Fields = []FieldChange{}
	}
	if d.Fields == nil {
		d.Fields = []FieldChange{}
	}
	return d, nil
}

func validateTasks(tasks []TaskPlan, changes []ResourceChange) error {
	changeIndex := make(map[string]int, len(changes))
	for i, c := range changes {
		key := c.Resource.identityKey()
		if _, exists := changeIndex[key]; exists {
			changeIndex[key] = -1
			continue
		}
		changeIndex[key] = i
	}

	owned := make(map[string]string, len(changes))
	seenNames := make(map[string]struct{}, len(tasks))
	for i, t := range tasks {
		if t.Name == "" {
			return fmt.Errorf("task %d: name is required", i)
		}
		if _, dup := seenNames[t.Name]; dup {
			return fmt.Errorf("task %s: duplicate task name", t.Name)
		}
		seenNames[t.Name] = struct{}{}
		if err := validateTask(t, changeIndex, owned); err != nil {
			return fmt.Errorf("task %s: %w", t.Name, err)
		}
	}
	return nil
}

func validateTask(t TaskPlan, changeIndex map[string]int, owned map[string]string) error {
	if !t.Phase.valid() {
		return fmt.Errorf("invalid phase %s", t.Phase)
	}
	if !t.Action.valid() {
		return fmt.Errorf("invalid action %s", t.Action)
	}
	if t.Action == TaskDelete && t.WillRun {
		return fmt.Errorf("delete must not will run")
	}
	switch t.Phase {
	case TaskSchedule:
		if len(t.Definitions) > 0 {
			return fmt.Errorf("schedule must not have hook definitions")
		}
		if t.WillRun {
			return fmt.Errorf("schedule must not will run")
		}
		for _, ref := range t.Resources {
			key := ref.identityKey()
			idx, ok := changeIndex[key]
			if !ok {
				return fmt.Errorf("dangling resource %s", ref)
			}
			if idx < 0 {
				return fmt.Errorf("resource %s matches more than one change", ref)
			}
			if prev, taken := owned[key]; taken {
				return fmt.Errorf("resource %s already referenced by task %s", ref, prev)
			}
			owned[key] = t.Name
		}
	case TaskPreDeploy, TaskPostDeploy:
		if len(t.Resources) > 0 {
			return fmt.Errorf("%s must not reference resource changes", t.Phase)
		}
		for i, d := range t.Definitions {
			if err := validateDefinition(d); err != nil {
				return fmt.Errorf("definition %d: %w", i, err)
			}
		}
	}
	return nil
}

func validateDefinition(d HookDefinition) error {
	if !d.definitionActionValid() {
		return fmt.Errorf("invalid action %s", d.Action)
	}
	switch d.Action {
	case Create:
		if d.Before != nil {
			return fmt.Errorf("create must not have a before snapshot")
		}
		if d.After == nil {
			return fmt.Errorf("create requires an after snapshot")
		}
	case Update:
		if d.Before == nil {
			return fmt.Errorf("update requires a before snapshot")
		}
		if d.After == nil {
			return fmt.Errorf("update requires an after snapshot")
		}
	case Delete:
		if d.Before == nil {
			return fmt.Errorf("delete requires a before snapshot")
		}
		if d.After != nil {
			return fmt.Errorf("delete must not have an after snapshot")
		}
	}
	return nil
}

func validateChange(c ResourceChange) error {
	if !c.Action.valid() {
		return fmt.Errorf("invalid action %s", c.Action)
	}
	if err := validateOrigin(c.Origin); err != nil {
		return err
	}
	if err := validateOriginResource(c); err != nil {
		return err
	}
	if err := validateApply(c.Action, c.Apply); err != nil {
		return err
	}
	if err := validateOriginApply(c); err != nil {
		return err
	}
	return validateSnapshots(c)
}

func validateOrigin(o ResourceOrigin) error {
	if !o.Kind.valid() {
		return fmt.Errorf("invalid origin %s", o.Kind)
	}
	switch o.Kind {
	case OriginHelm:
		if o.Helm == nil {
			return fmt.Errorf("helm origin requires helm details")
		}
	case OriginCRD, OriginNamespace:
		if o.Helm != nil {
			return fmt.Errorf("%s origin must not include helm details", o.Kind)
		}
	}
	return nil
}

func validateOriginResource(c ResourceChange) error {
	switch c.Origin.Kind {
	case OriginCRD:
		if c.Resource.APIVersion != "apiextensions.k8s.io/v1" || c.Resource.Kind != "CustomResourceDefinition" {
			return fmt.Errorf("crd origin requires apiextensions.k8s.io/v1 CustomResourceDefinition")
		}
		if c.Resource.Namespace != "" {
			return fmt.Errorf("crd origin must be cluster-scoped")
		}
		switch c.Action {
		case Create, Update:
		default:
			return fmt.Errorf("crd origin does not support %s", c.Action)
		}
	case OriginNamespace:
		if c.Resource.APIVersion != "v1" || c.Resource.Kind != "Namespace" {
			return fmt.Errorf("namespace origin requires v1 Namespace")
		}
		if c.Resource.Namespace != "" {
			return fmt.Errorf("namespace origin must be cluster-scoped")
		}
		switch c.Action {
		case Create, Update:
		default:
			return fmt.Errorf("namespace origin does not support %s", c.Action)
		}
	}
	return nil
}

func validateOriginApply(c ResourceChange) error {
	if c.Apply.Write == nil {
		return nil
	}
	switch c.Origin.Kind {
	case OriginCRD:
		switch c.Action {
		case Create:
			switch c.Apply.Write.Method {
			case WriteCreate:
				return nil
			case WriteServerSide:
				if !c.Apply.Write.ForceConflicts {
					return fmt.Errorf("crd create server_side_apply requires force conflicts")
				}
				return nil
			default:
				return fmt.Errorf("crd create requires create or server_side_apply")
			}
		case Update:
			if c.Apply.Write.Method != WriteServerSide || !c.Apply.Write.ForceConflicts {
				return fmt.Errorf("crd update requires server_side_apply with force conflicts")
			}
		}
	case OriginNamespace:
		if c.Apply.Write.Method != WriteServerSide {
			return fmt.Errorf("namespace origin requires server_side_apply")
		}
		if c.Apply.Write.ForceConflicts {
			return fmt.Errorf("namespace origin must not force conflicts")
		}
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
		if err := validateWrite(*apply.Write); err != nil {
			return err
		}
		if action == Update && apply.Write.Method == WriteCreate {
			return fmt.Errorf("update requires server_side_apply")
		}
		return nil
	case Delete:
		if apply.Write != nil {
			return fmt.Errorf("delete must not have write semantics")
		}
		if apply.Delete == nil {
			return fmt.Errorf("delete requires delete semantics")
		}
		return validateDelete(*apply.Delete)
	case Replace:
		if apply.Write == nil {
			return fmt.Errorf("replace requires write semantics")
		}
		if apply.Delete == nil {
			return fmt.Errorf("replace requires delete semantics")
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
	switch w.Method {
	case WriteServerSide:
		if w.FieldManager == "" {
			return fmt.Errorf("server_side_apply requires a field manager")
		}
	case WriteCreate:
		if w.FieldManager != "" {
			return fmt.Errorf("create write must not set a field manager")
		}
		if w.ForceConflicts {
			return fmt.Errorf("create write must not force conflicts")
		}
	}
	return nil
}

func validateHelmAction(header Header, helmAction HelmAction) error {
	switch helmAction {
	case HelmInstall:
		if !header.FreshInstall {
			return fmt.Errorf("helm install requires a fresh install")
		}
	case HelmNone, HelmUpgrade:
		if header.FreshInstall {
			return fmt.Errorf("fresh install requires helm install")
		}
	default:
		return fmt.Errorf("invalid helm action %s", helmAction)
	}
	return nil
}

func validateHelmContent(helmAction HelmAction, changes []ResourceChange, tasks []TaskPlan) error {
	for _, c := range changes {
		if c.Origin.Kind == OriginNamespace && helmAction != HelmInstall {
			return fmt.Errorf("namespace origin requires helm install")
		}
	}
	if helmAction != HelmNone {
		return nil
	}
	for _, c := range changes {
		if c.Origin.Kind == OriginHelm {
			return fmt.Errorf("helm none must not include helm resource changes")
		}
	}
	for _, t := range tasks {
		if t.Phase != TaskPreDeploy && t.Phase != TaskPostDeploy {
			continue
		}
		if t.Action != TaskUnchanged {
			return fmt.Errorf("helm none must not include changed %s task %s", t.Phase, t.Name)
		}
		if t.WillRun {
			return fmt.Errorf("helm none must not will run %s task %s", t.Phase, t.Name)
		}
	}
	return nil
}

func validateDelete(d DeleteSemantics) error {
	if !d.Propagation.valid() {
		return fmt.Errorf("invalid delete propagation %s", d.Propagation)
	}
	return nil
}

func validateSnapshots(c ResourceChange) error {
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
		if c.After == nil {
			return fmt.Errorf("update requires an after snapshot")
		}
	case Delete:
		if c.Before == nil {
			return fmt.Errorf("delete requires a before snapshot")
		}
		if c.After != nil {
			return fmt.Errorf("delete must not have an after snapshot")
		}
	case Replace:
		if c.Before == nil {
			return fmt.Errorf("replace requires a before snapshot")
		}
		if c.After == nil {
			return fmt.Errorf("replace requires an after snapshot")
		}
	}
	return nil
}

func validateDrift(d ResourceDrift) error {
	if !d.Kind.valid() {
		return fmt.Errorf("invalid drift kind %s", d.Kind)
	}
	switch d.Kind {
	case DriftModified:
		if len(d.Fields) == 0 {
			return fmt.Errorf("modified drift requires fields")
		}
	case DriftMissing:
		if len(d.Fields) > 0 {
			return fmt.Errorf("missing drift must not have fields")
		}
	}
	return nil
}
