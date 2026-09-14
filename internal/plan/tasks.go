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

package plan

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

type previousTask struct {
	On     spec.TaskOn
	Weight int
}

type hookDoc struct {
	Ref    semantic.ResourceRef
	Object map[string]any
	Weight int
	Events []v1.HookEvent
}

func assembleTasks(
	resolved *spec.ResolvedSpec,
	prep helm.ReleasePrep,
	desiredHooks []*v1.Hook,
	changes []semantic.ResourceChange,
) ([]semantic.TaskPlan, error) {
	if resolved == nil {
		return []semantic.TaskPlan{}, nil
	}

	previous, err := previousTasks(prep.Current)
	if err != nil {
		return nil, err
	}
	desiredDocs, err := desiredHookDocs(desiredHooks, resolved.Tasks)
	if err != nil {
		return nil, fmt.Errorf("desired hooks: %w", err)
	}
	var previousHooks []*v1.Hook
	if prep.Current != nil {
		previousHooks = prep.Current.Hooks
	}
	previousDocs, err := previousHookDocs(previousHooks)
	if err != nil {
		return nil, fmt.Errorf("current release hooks: %w", err)
	}

	names := taskNames(resolved.Tasks, previous, previousDocs)
	fresh := prep.Operation == helm.OperationInstall || prep.Current == nil

	tasks := make([]semantic.TaskPlan, 0, len(names))
	for _, name := range names {
		current, hasCurrent := resolved.Tasks[name]
		prev, hasPrev := previous[name]
		if hasCurrent && current.Task.On == spec.TaskOnManual {
			if !hasPrev && len(previousDocs[name]) == 0 {
				continue
			}
			hasCurrent = false
		}
		if !hasCurrent && hasPrev && prev.On == spec.TaskOnManual {
			continue
		}
		if hasCurrent {
			prevOn, prevOnErr := previousTaskOn(hasPrev, prev, previousDocs[name])
			if prevOnErr != nil {
				return nil, fmt.Errorf("task %s: %w", name, prevOnErr)
			}
			if prevOn != "" && incompatibleTaskOn(current.Task.On, prevOn) {
				return nil, fmt.Errorf("task %s: cannot change on from %s to %s", name, prevOn, current.Task.On)
			}
		}

		on, onErr := taskOn(hasCurrent, current, hasPrev, prev, previousDocs[name])
		if onErr != nil {
			return nil, fmt.Errorf("task %s: %w", name, onErr)
		}
		switch on {
		case spec.TaskOnSchedule:
			task, ok := scheduleTask(name, hasCurrent, hasPrev, current, prev, changes, previous)
			if ok {
				tasks = append(tasks, task)
			}
		case spec.TaskOnPreDeploy, spec.TaskOnPostDeploy:
			task, hookErr := hookTask(name, on, hasCurrent, hasPrev, current, prev, desiredDocs[name], previousDocs[name])
			if hookErr != nil {
				return nil, fmt.Errorf("task %s: %w", name, hookErr)
			}
			tasks = append(tasks, task)
		default:
			return nil, fmt.Errorf("task %s: unsupported on %s", name, on)
		}
	}
	applyHelmWillRun(tasks, fresh, len(changes) > 0)
	return tasks, nil
}

func incompatibleTaskOn(current, previous spec.TaskOn) bool {
	return current.IsHook() && previous.IsScheduled() || current.IsScheduled() && previous.IsHook()
}

func previousTaskOn(hasPrev bool, prev previousTask, docs map[string]hookDoc) (spec.TaskOn, error) {
	if hasPrev {
		return prev.On, nil
	}
	if len(docs) == 0 {
		return "", nil
	}
	return taskOnFromDocs(docs)
}

func taskOn(hasCurrent bool, current spec.ResolvedTask, hasPrev bool, prev previousTask, previousDocs map[string]hookDoc) (spec.TaskOn, error) {
	if hasCurrent {
		return current.Task.On, nil
	}
	if hasPrev {
		return prev.On, nil
	}
	return taskOnFromDocs(previousDocs)
}

func taskOnFromDocs(docs map[string]hookDoc) (spec.TaskOn, error) {
	if len(docs) == 0 {
		return "", fmt.Errorf("no hook documents")
	}
	var on spec.TaskOn
	for _, d := range docs {
		got, err := taskOnFromEvents(d.Events)
		if err != nil {
			return "", err
		}
		if on == "" {
			on = got
			continue
		}
		if got != on {
			return "", fmt.Errorf("mixed hook phases %s and %s", on, got)
		}
	}
	return on, nil
}

func taskOnFromEvents(events []v1.HookEvent) (spec.TaskOn, error) {
	pre, post := false, false
	for _, e := range events {
		switch e {
		case v1.HookPreInstall, v1.HookPreUpgrade:
			pre = true
		case v1.HookPostInstall, v1.HookPostUpgrade:
			post = true
		default:
			return "", fmt.Errorf("unsupported hook event %s", e)
		}
	}
	switch {
	case pre && !post:
		return spec.TaskOnPreDeploy, nil
	case post && !pre:
		return spec.TaskOnPostDeploy, nil
	case pre && post:
		return "", fmt.Errorf("mixed pre and post hook events")
	default:
		return "", fmt.Errorf("no helm hook events")
	}
}

func scheduleTask(
	name string,
	hasCurrent, hasPrev bool,
	current spec.ResolvedTask,
	prev previousTask,
	changes []semantic.ResourceChange,
	previous map[string]previousTask,
) (semantic.TaskPlan, bool) {
	refs := scheduleRefs(name, changes, previous)
	action := semantic.TaskUnchanged
	switch {
	case hasCurrent && !hasPrev:
		action = semantic.TaskCreate
	case !hasCurrent && hasPrev:
		action = semantic.TaskDelete
	case len(refs) > 0:
		action = semantic.TaskUpdate
	}
	if action == semantic.TaskUnchanged && len(refs) == 0 {
		return semantic.TaskPlan{}, false
	}
	weight := 0
	if hasCurrent {
		weight = current.HookWeight
	} else {
		weight = prev.Weight
	}
	return semantic.TaskPlan{
		Name:       name,
		Phase:      semantic.TaskSchedule,
		Action:     action,
		WillRun:    false,
		Resources:  refs,
		HookWeight: weight,
	}, true
}

func scheduleRefs(name string, changes []semantic.ResourceChange, previous map[string]previousTask) []semantic.ResourceRef {
	refs := make([]semantic.ResourceRef, 0)
	for _, c := range changes {
		if resourceTaskName(c, previous) != name {
			continue
		}
		refs = append(refs, c.Resource)
	}
	return refs
}

func resourceTaskName(c semantic.ResourceChange, previous map[string]previousTask) string {
	if task := labelValue(snapshotMap(c.After), spec.LabelTask); task != "" {
		return task
	}
	if task := labelValue(snapshotMap(c.Before), spec.LabelTask); task != "" {
		return task
	}
	if task := labelValue(snapshotMap(c.Before), spec.LabelComponent); task != "" {
		if _, ok := previous[task]; ok {
			return task
		}
	}
	return ""
}

func hookTask(
	name string,
	on spec.TaskOn,
	hasCurrent, hasPrev bool,
	current spec.ResolvedTask,
	prev previousTask,
	desired, previous map[string]hookDoc,
) (semantic.TaskPlan, error) {
	defs, err := diffHookDocs(desired, previous)
	if err != nil {
		return semantic.TaskPlan{}, err
	}
	action := semantic.TaskUnchanged
	switch {
	case hasCurrent && !hasPrev:
		action = semantic.TaskCreate
	case !hasCurrent:
		action = semantic.TaskDelete
	case len(defs) > 0:
		action = semantic.TaskUpdate
	}
	weight := 0
	switch {
	case hasCurrent:
		weight = current.HookWeight
	case hasPrev:
		weight = prev.Weight
	default:
		weight = hookDocsWeight(previous)
	}
	phase := semantic.TaskPreDeploy
	if on == spec.TaskOnPostDeploy {
		phase = semantic.TaskPostDeploy
	}
	return semantic.TaskPlan{
		Name:        name,
		Phase:       phase,
		Action:      action,
		WillRun:     false,
		Definitions: defs,
		HookWeight:  weight,
	}, nil
}

func applyHelmWillRun(tasks []semantic.TaskPlan, fresh, helmSide bool) {
	helmWillRun := fresh || helmSide
	if !helmWillRun {
		for _, t := range tasks {
			if hookPhase(t.Phase) && t.Action != semantic.TaskUnchanged {
				helmWillRun = true
				break
			}
		}
	}
	for i, t := range tasks {
		if t.Action == semantic.TaskDelete || !hookPhase(t.Phase) {
			continue
		}
		tasks[i].WillRun = helmWillRun
	}
}

func hookPhase(p semantic.TaskPhase) bool {
	return p == semantic.TaskPreDeploy || p == semantic.TaskPostDeploy
}

func diffHookDocs(desired, previous map[string]hookDoc) ([]semantic.HookDefinition, error) {
	if desired == nil {
		desired = map[string]hookDoc{}
	}
	if previous == nil {
		previous = map[string]hookDoc{}
	}
	keys := make(map[string]struct{}, len(desired)+len(previous))
	for k := range desired {
		keys[k] = struct{}{}
	}
	for k := range previous {
		keys[k] = struct{}{}
	}
	defs := make([]semantic.HookDefinition, 0)
	for key := range keys {
		after, hasAfter := desired[key]
		before, hasBefore := previous[key]
		switch {
		case hasAfter && !hasBefore:
			defs = append(defs, semantic.HookDefinition{
				Resource:   after.Ref,
				Action:     semantic.Create,
				After:      &semantic.ResourceSnapshot{Object: after.Object},
				HookWeight: after.Weight,
			})
		case !hasAfter && hasBefore:
			defs = append(defs, semantic.HookDefinition{
				Resource:   before.Ref,
				Action:     semantic.Delete,
				Before:     &semantic.ResourceSnapshot{Object: before.Object},
				HookWeight: before.Weight,
			})
		default:
			fields, err := semantic.DiffFields(before.Object, after.Object)
			if err != nil {
				return nil, err
			}
			if len(fields) == 0 {
				continue
			}
			defs = append(defs, semantic.HookDefinition{
				Resource:   after.Ref,
				Action:     semantic.Update,
				Before:     &semantic.ResourceSnapshot{Object: before.Object},
				After:      &semantic.ResourceSnapshot{Object: after.Object},
				Fields:     fields,
				HookWeight: after.Weight,
			})
		}
	}
	return defs, nil
}

func desiredHookDocs(hooks []*v1.Hook, current map[string]spec.ResolvedTask) (map[string]map[string]hookDoc, error) {
	out := make(map[string]map[string]hookDoc)
	for _, h := range hooks {
		obj, skip, err := parseHookObject(h)
		if err != nil {
			return nil, err
		}
		if skip {
			continue
		}
		name := obj.GetLabels()[spec.LabelTask]
		if name == "" {
			return nil, fmt.Errorf("hook %s: missing %s label", hookDisplayName(h, obj), spec.LabelTask)
		}
		rt, ok := current[name]
		if !ok || !rt.Task.On.IsHook() {
			return nil, fmt.Errorf("hook %s: %s %q is not a current preDeploy or postDeploy task", hookDisplayName(h, obj), spec.LabelTask, name)
		}
		if addErr := addHookDoc(out, name, h, obj); addErr != nil {
			return nil, addErr
		}
	}
	return out, nil
}

func previousHookDocs(hooks []*v1.Hook) (map[string]map[string]hookDoc, error) {
	out := make(map[string]map[string]hookDoc)
	for _, h := range hooks {
		obj, skip, err := parseHookObject(h)
		if err != nil {
			return nil, err
		}
		if skip {
			continue
		}
		labels := obj.GetLabels()
		name := labels[spec.LabelTask]
		if name == "" {
			name = labels[spec.LabelComponent]
		}
		if name == "" {
			return nil, fmt.Errorf("hook %s: missing %s label", hookDisplayName(h, obj), spec.LabelComponent)
		}
		if addErr := addHookDoc(out, name, h, obj); addErr != nil {
			return nil, addErr
		}
	}
	return out, nil
}

func parseHookObject(h *v1.Hook) (*unstructured.Unstructured, bool, error) {
	if h == nil || strings.TrimSpace(h.Manifest) == "" {
		return nil, true, nil
	}
	obj, err := unstructuredFromYAML(h.Manifest)
	if err != nil {
		return nil, false, fmt.Errorf("parse hook %s: %w", h.Name, err)
	}
	return obj, false, nil
}

func hookDisplayName(h *v1.Hook, obj *unstructured.Unstructured) string {
	display := ""
	if h != nil {
		display = strings.TrimSpace(h.Name)
	}
	if display == "" && obj != nil {
		display = obj.GetName()
	}
	return display
}

func addHookDoc(out map[string]map[string]hookDoc, name string, h *v1.Hook, obj *unstructured.Unstructured) error {
	ref := semantic.ResourceRef{
		APIVersion: obj.GetAPIVersion(),
		Kind:       obj.GetKind(),
		Namespace:  obj.GetNamespace(),
		Name:       obj.GetName(),
	}
	if ref.Name == "" {
		ref.GenerateName = obj.GetGenerateName()
	}
	key := ref.APIVersion + "\x00" + ref.Kind + "\x00" + ref.Namespace + "\x00" + ref.Name + "\x00" + ref.GenerateName
	docs := out[name]
	if docs == nil {
		docs = map[string]hookDoc{}
		out[name] = docs
	}
	if _, dup := docs[key]; dup {
		return fmt.Errorf("task %s: duplicate hook identity %s", name, ref)
	}
	docs[key] = hookDoc{
		Ref:    ref,
		Object: obj.Object,
		Weight: hookWeight(h, obj),
		Events: hookEvents(h, obj),
	}
	return nil
}

func hookEvents(h *v1.Hook, obj *unstructured.Unstructured) []v1.HookEvent {
	if h != nil && len(h.Events) > 0 {
		return h.Events
	}
	raw := ""
	if obj != nil {
		raw = obj.GetAnnotations()[v1.HookAnnotation]
	}
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	events := make([]v1.HookEvent, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		events = append(events, v1.HookEvent(part))
	}
	return events
}

func hookDocsWeight(docs map[string]hookDoc) int {
	first := true
	weight := 0
	for _, d := range docs {
		if first || d.Weight > weight {
			weight = d.Weight
			first = false
		}
	}
	return weight
}

func hookWeight(h *v1.Hook, obj *unstructured.Unstructured) int {
	if h != nil && h.Weight != 0 {
		return h.Weight
	}
	if obj == nil {
		return 0
	}
	raw := obj.GetAnnotations()[v1.HookWeightAnnotation]
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return n
}

func unstructuredFromYAML(raw string) (*unstructured.Unstructured, error) {
	var obj unstructured.Unstructured
	if err := yamlutil.Unmarshal([]byte(raw), &obj); err != nil {
		return nil, err
	}
	if len(obj.Object) == 0 {
		return nil, fmt.Errorf("empty document")
	}
	return &obj, nil
}

func previousTasks(rel *v1.Release) (map[string]previousTask, error) {
	out := make(map[string]previousTask)
	if rel == nil {
		return out, nil
	}
	tasks := mapAt(rel.Config, "deployah", "resolved", "tasks")
	if tasks == nil {
		return out, nil
	}
	for name, raw := range tasks {
		entry, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("current release task %s: expected object", name)
		}
		on := spec.TaskOn(asString(entry["on"]))
		if on == spec.TaskOnManual || on == "" {
			continue
		}
		out[name] = previousTask{On: on, Weight: asInt(entry["hookWeight"])}
	}
	return out, nil
}

func taskNames(
	current map[string]spec.ResolvedTask,
	previous map[string]previousTask,
	previousDocs map[string]map[string]hookDoc,
) []string {
	seen := make(map[string]struct{})
	add := func(name string) {
		if name == "" {
			return
		}
		seen[name] = struct{}{}
	}
	for name, rt := range current {
		if rt.Task.On == spec.TaskOnManual {
			continue
		}
		add(name)
	}
	for name := range previous {
		add(name)
	}
	for name := range previousDocs {
		add(name)
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	return names
}

func labelValue(obj map[string]any, key string) string {
	meta, ok := obj["metadata"].(map[string]any)
	if !ok || meta == nil {
		return ""
	}
	switch labels := meta["labels"].(type) {
	case map[string]string:
		return labels[key]
	case map[string]any:
		return asString(labels[key])
	default:
		return ""
	}
}

func asString(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	case string:
		i, err := strconv.Atoi(n)
		if err != nil {
			return 0
		}
		return i
	default:
		return 0
	}
}

func mapAt(root map[string]any, keys ...string) map[string]any {
	cur := root
	if cur == nil {
		return nil
	}
	for _, key := range keys {
		next, ok := cur[key].(map[string]any)
		if !ok {
			return nil
		}
		cur = next
	}
	return cur
}
