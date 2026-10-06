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

// Plan is one run's Helm release intent, its Previous-to-Desired
// resource changes, and its Previous-to-Live drift. Build it with
// [New]. Snapshots still contain secrets, and this type is not the
// plan JSON document.
type Plan struct {
	Header     Header
	HelmAction HelmAction
	Changes    []ResourceChange
	Drift      []DriftChange
	Tasks      []TaskPlan
	ChartCRDs  []ChartCRD
	Summary    Summary
}

// Input is the caller data for [New]. Task WillRun values in Tasks
// are ignored. [New] derives them.
type Input struct {
	Header     Header
	HelmAction HelmAction
	Changes    []ResourceChange
	Tasks      []TaskPlan
	Drift      []DriftChange
	ChartCRDs  []ChartCRD
}

// New copies in and returns a complete plan, or a zero Plan and an
// error. Nil slices become empty slices.
//
// Order is: copy and normalize, enum and shape checks, derived
// WillRun, Helm and content checks, drift and chart CRD checks, sort,
// then [Summary]. Summary counts resource changes only. Chart CRDs
// stay in the order given. Changes, tasks, and drift are sorted.
func New(in Input) (Plan, error) {
	changes := normalizeChanges(in.Changes)
	tasks, normErr := normalizeTasks(in.Tasks)
	if normErr != nil {
		return Plan{}, normErr
	}
	drift := copyDrift(in.Drift)
	crds := slices.Clone(in.ChartCRDs)
	if crds == nil {
		crds = []ChartCRD{}
	}

	if !in.HelmAction.valid() {
		return Plan{}, fmt.Errorf("invalid helm action %s", in.HelmAction)
	}
	if err := validateHelmAction(in.Header, in.HelmAction); err != nil {
		return Plan{}, err
	}
	for i := range changes {
		if err := validateChange(changes[i]); err != nil {
			return Plan{}, fmt.Errorf("resource %s: %w", changes[i].Resource, err)
		}
	}
	if err := validateTasks(tasks, changes); err != nil {
		return Plan{}, err
	}

	deriveWillRun(tasks, in.HelmAction)

	if err := validateHelmContent(in.HelmAction, changes, tasks); err != nil {
		return Plan{}, err
	}
	if err := validateDriftSet(in.Header, drift); err != nil {
		return Plan{}, err
	}
	if err := validateChartCRDs(crds); err != nil {
		return Plan{}, err
	}

	sortChanges(changes)
	sortTasks(tasks, changes)
	sortDrift(drift)

	return Plan{
		Header:     in.Header,
		HelmAction: in.HelmAction,
		Changes:    changes,
		Tasks:      tasks,
		Drift:      drift,
		ChartCRDs:  crds,
		Summary:    Summarize(changes),
	}, nil
}

func normalizeChanges(in []ResourceChange) []ResourceChange {
	out := slices.Clone(in)
	if out == nil {
		out = []ResourceChange{}
	}
	for i := range out {
		out[i] = normalizeChange(out[i])
	}
	return out
}

func normalizeTasks(in []TaskPlan) ([]TaskPlan, error) {
	out := copyTasks(in)
	for i := range out {
		normalized, err := normalizeTask(out[i])
		if err != nil {
			return nil, fmt.Errorf("task %s: %w", out[i].Name, err)
		}
		out[i] = normalized
	}
	return out, nil
}

// deriveWillRun overwrites caller WillRun. Schedule and delete tasks
// do not run. Other preDeploy and postDeploy tasks run only when the
// Helm action is install or upgrade.
func deriveWillRun(tasks []TaskPlan, helmAction HelmAction) {
	transition := helmAction == HelmInstall || helmAction == HelmUpgrade
	for i := range tasks {
		tasks[i].WillRun = false
		if !transition {
			continue
		}
		if tasks[i].Phase != TaskPreDeploy && tasks[i].Phase != TaskPostDeploy {
			continue
		}
		if tasks[i].Action == TaskDelete {
			continue
		}
		tasks[i].WillRun = true
	}
}

func validateChartCRDs(crds []ChartCRD) error {
	for i, c := range crds {
		if err := validateChartCRD(c); err != nil {
			return fmt.Errorf("chart crd %d: %w", i, err)
		}
	}
	return nil
}

func validateChartCRD(c ChartCRD) error {
	if c.Kind != "CustomResourceDefinition" {
		return fmt.Errorf("kind must be CustomResourceDefinition")
	}
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}
	if !c.Lifecycle.valid() {
		return fmt.Errorf("invalid lifecycle %s", c.Lifecycle)
	}
	wantProcess := c.Lifecycle == ChartCRDProcess
	if c.WillProcess != wantProcess {
		return fmt.Errorf("willProcess must be %t for lifecycle %s", wantProcess, c.Lifecycle)
	}
	return nil
}

// HasEffects reports whether the plan would change or run something.
// A chart CRD with [ChartCRD.WillProcess] counts. A bare [HelmAction]
// and [Plan.Drift] do not.
func (p Plan) HasEffects() bool {
	if len(p.Changes) > 0 {
		return true
	}
	for _, t := range p.Tasks {
		if t.Action != TaskUnchanged || t.WillRun {
			return true
		}
	}
	for _, crd := range p.ChartCRDs {
		if crd.WillProcess {
			return true
		}
	}
	return false
}

// IsNoOp reports a non-install [HelmNone] plan with no effects.
// Drift can still be present.
func (p Plan) IsNoOp() bool {
	return !p.Header.FreshInstall &&
		p.HelmAction == HelmNone &&
		!p.HasEffects()
}

func normalizeChange(c ResourceChange) ResourceChange {
	c.Before = copySnapshot(c.Before)
	c.After = copySnapshot(c.After)
	c.Fields = copyFields(c.Fields)
	if c.Fields == nil {
		c.Fields = []FieldChange{}
	}
	return c
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
	if d.Fields == nil {
		d.Fields = []FieldChange{}
	}
	return d, nil
}

func validateTasks(tasks []TaskPlan, changes []ResourceChange) error {
	changeIndex := make(map[string]int, len(changes))
	for i, c := range changes {
		key := c.Resource.refKey()
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
	switch t.Phase {
	case TaskSchedule:
		if len(t.Definitions) > 0 {
			return fmt.Errorf("schedule must not have hook definitions")
		}
		for _, ref := range t.Resources {
			key := ref.refKey()
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
		if len(d.Fields) != 0 {
			return fmt.Errorf("create must not have field changes")
		}
	case Update:
		if d.Before == nil {
			return fmt.Errorf("update requires a before snapshot")
		}
		if d.After == nil {
			return fmt.Errorf("update requires an after snapshot")
		}
		if len(d.Fields) == 0 {
			return fmt.Errorf("update requires a field change")
		}
	case Delete:
		if d.Before == nil {
			return fmt.Errorf("delete requires a before snapshot")
		}
		if d.After != nil {
			return fmt.Errorf("delete must not have an after snapshot")
		}
		if len(d.Fields) != 0 {
			return fmt.Errorf("delete must not have field changes")
		}
	}
	return nil
}

func validateChange(c ResourceChange) error {
	if !c.Action.valid() {
		return fmt.Errorf("invalid action %s", c.Action)
	}
	return validateSnapshots(c)
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
	if helmAction == HelmNone && len(changes) > 0 {
		return fmt.Errorf("helm none must not include resource changes")
	}
	if helmAction != HelmNone {
		return nil
	}
	for _, t := range tasks {
		if t.Phase != TaskPreDeploy && t.Phase != TaskPostDeploy {
			continue
		}
		if t.Action != TaskUnchanged {
			return fmt.Errorf("helm none must not include changed %s task %s", t.Phase, t.Name)
		}
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
		if len(c.Fields) != 0 {
			return fmt.Errorf("create must not have field changes")
		}
	case Update:
		if c.Before == nil {
			return fmt.Errorf("update requires a before snapshot")
		}
		if c.After == nil {
			return fmt.Errorf("update requires an after snapshot")
		}
		if len(c.Fields) == 0 {
			return fmt.Errorf("update requires a field change")
		}
	case Delete:
		if c.Before == nil {
			return fmt.Errorf("delete requires a before snapshot")
		}
		if c.After != nil {
			return fmt.Errorf("delete must not have an after snapshot")
		}
		if len(c.Fields) != 0 {
			return fmt.Errorf("delete must not have field changes")
		}
	}
	return nil
}
