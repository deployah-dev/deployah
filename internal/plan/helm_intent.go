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
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"deployah.dev/deployah/internal/plan/semantic"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type plannedObject struct {
	id           ResourceIdentity
	obj          *unstructured.Unstructured
	effective    *unstructured.Unstructured
	locator      ResourceLocator
	generateName bool
	skipGet      bool
}

type helmResourceIntent int

const (
	helmIntentUnchanged helmResourceIntent = iota
	helmIntentCreate
	helmIntentUpdate
	helmIntentPrune
)

type helmPair struct {
	previous *plannedObject
	desired  *plannedObject
	intent   helmResourceIntent
}

func loadPlannedObjects(
	cluster ClusterReader,
	surface *crdSurface,
	manifest, defaultNamespace, releaseName, releaseNamespace string,
) ([]plannedObject, error) {
	objs, err := flattenManifest(manifest)
	if err != nil {
		return nil, err
	}
	out := make([]plannedObject, 0, len(objs))
	seen := make(map[string]struct{}, len(objs))
	for _, obj := range objs {
		resolved, resErr := resolveObject(cluster, surface, obj, defaultNamespace)
		if resErr != nil {
			return nil, resErr
		}
		key := resourceMatchKey(resolved.id)
		if _, dup := seen[key]; dup && !resolved.generateName {
			return nil, fmt.Errorf("duplicate identity %s/%s %s/%s", resolved.id.Group, resolved.id.Kind, resolved.id.Namespace, resolved.id.Name)
		}
		if !resolved.generateName {
			seen[key] = struct{}{}
		}
		resolved.effective = stampCopy(resolved.obj, releaseName, releaseNamespace)
		out = append(out, resolved)
	}
	return out, nil
}

func resolveObject(
	cluster ClusterReader,
	surface *crdSurface,
	obj *unstructured.Unstructured,
	defaultNamespace string,
) (plannedObject, error) {
	gvk := obj.GroupVersionKind()
	generateName, err := generateNameState(obj)
	if err != nil {
		return plannedObject{}, err
	}
	mapping, mapErr := cluster.Mapping(gvk)
	switch {
	case mapErr == nil:
		normalizeScope(obj, mapping, defaultNamespace)
		id := identityOfUnstructured(obj)
		return plannedObject{
			id:           id,
			obj:          obj,
			locator:      locatorFromMapping(id, mapping),
			generateName: generateName,
		}, nil
	case meta.IsNoMatchError(mapErr):
		if name, ok := surface.unserved[gvk]; ok {
			return plannedObject{}, fmt.Errorf("API %s/%s/%s is not served by CRD %s after this deployment", gvk.Group, gvk.Version, gvk.Kind, name)
		}
		desc, ok := surface.served[gvk]
		if !ok {
			return plannedObject{}, fmt.Errorf("resolve resource mapping for %s %s: %w", gvk, obj.GetName(), mapErr)
		}
		normalizeScopeFromDesc(obj, desc, defaultNamespace)
		id := identityOfUnstructured(obj)
		if desc.missingEntire {
			return plannedObject{
				id:           id,
				obj:          obj,
				locator:      locatorFromAPI(id, desc),
				generateName: generateName,
				skipGet:      true,
			}, nil
		}
		return plannedObject{}, fmt.Errorf("cannot establish live state for %s %s: CRD %s would add this API but it is not discoverable", gvk, obj.GetName(), desc.crdName)
	default:
		return plannedObject{}, fmt.Errorf("resolve resource mapping for %s %s: %w", gvk, obj.GetName(), mapErr)
	}
}

func pairHelmResources(previous, desired []plannedObject) []helmPair {
	prevByKey := make(map[string]*plannedObject, len(previous))
	for i := range previous {
		if previous[i].generateName {
			continue
		}
		prevByKey[resourceMatchKey(previous[i].id)] = &previous[i]
	}
	used := make(map[string]struct{}, len(desired))
	pairs := make([]helmPair, 0, len(previous)+len(desired))
	for i := range desired {
		d := &desired[i]
		if d.generateName || d.id.Name == "" {
			pairs = append(pairs, helmPair{desired: d, intent: helmIntentCreate})
			continue
		}
		key := resourceMatchKey(d.id)
		p, ok := prevByKey[key]
		if !ok {
			pairs = append(pairs, helmPair{desired: d, intent: helmIntentCreate})
			continue
		}
		used[key] = struct{}{}
		intent := helmIntentUnchanged
		if !equalRawIntent(p.obj, d.obj) {
			intent = helmIntentUpdate
		}
		pairs = append(pairs, helmPair{previous: p, desired: d, intent: intent})
	}
	for i := range previous {
		p := &previous[i]
		if p.generateName {
			continue
		}
		key := resourceMatchKey(p.id)
		if _, ok := used[key]; ok {
			continue
		}
		pairs = append(pairs, helmPair{previous: p, intent: helmIntentPrune})
	}
	return pairs
}

func helmResourcesChanged(pairs []helmPair) bool {
	for _, p := range pairs {
		if p.intent != helmIntentUnchanged {
			return true
		}
	}
	return false
}

func liveByKey(
	ctx context.Context,
	cluster ClusterReader,
	pairs []helmPair,
) (map[string]*unstructured.Unstructured, error) {
	out := make(map[string]*unstructured.Unstructured, len(pairs))
	got := make(map[string]struct{}, len(pairs))
	load := func(obj *plannedObject) error {
		if obj == nil {
			return nil
		}
		key := resourceMatchKey(obj.id)
		if _, ok := got[key]; ok {
			return nil
		}
		got[key] = struct{}{}
		if obj.generateName || obj.skipGet {
			return nil
		}
		live, err := cluster.Get(ctx, obj.locator)
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("get %s %q: %w", obj.id.Kind, obj.id.Name, err)
		}
		out[key] = live
		return nil
	}
	for _, p := range pairs {
		if err := load(p.desired); err != nil {
			return nil, err
		}
		if p.desired != nil && !p.desired.skipGet {
			continue
		}
		if err := load(p.previous); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func helmResourceChanges(
	origin semantic.ResourceOrigin,
	pairs []helmPair,
	live map[string]*unstructured.Unstructured,
) ([]semantic.ResourceChange, error) {
	changes := make([]semantic.ResourceChange, 0, len(pairs))
	for _, p := range pairs {
		change, ok, err := helmChange(origin, p, live)
		if err != nil {
			return nil, err
		}
		if ok {
			changes = append(changes, change)
		}
	}
	if err := stampHelmApplyOrder(changes); err != nil {
		return nil, err
	}
	return changes, nil
}

func helmChange(
	origin semantic.ResourceOrigin,
	p helmPair,
	live map[string]*unstructured.Unstructured,
) (semantic.ResourceChange, bool, error) {
	switch {
	case p.desired != nil:
		key := resourceMatchKey(p.desired.id)
		cur := live[key]
		if cur == nil {
			return semantic.ResourceChange{
				Resource: resourceRefOf(p.desired.effective),
				Origin:   origin,
				Action:   semantic.Create,
				After:    snapshotOf(p.desired.effective),
				Apply:    writeApply(),
			}, true, nil
		}
		var declared []map[string]any
		if p.previous != nil {
			declared = append(declared, p.previous.effective.Object)
		}
		declared = append(declared, p.desired.effective.Object)
		projected, err := semantic.ProjectOntoDeclared(cur.Object, declared...)
		if err != nil {
			return semantic.ResourceChange{}, false, fmt.Errorf("%s: %w", p.desired.id.Name, err)
		}
		before := &unstructured.Unstructured{Object: projected}
		if equalObjects(before, p.desired.effective) {
			return semantic.ResourceChange{}, false, nil
		}
		return semantic.ResourceChange{
			Resource: resourceRefOf(p.desired.effective),
			Origin:   origin,
			Action:   semantic.Update,
			Before:   snapshotOf(before),
			After:    snapshotOf(p.desired.effective),
			Apply:    writeApply(),
		}, true, nil
	case p.previous != nil:
		key := resourceMatchKey(p.previous.id)
		cur := live[key]
		if cur == nil || liveKeepPolicy(cur) {
			return semantic.ResourceChange{}, false, nil
		}
		return semantic.ResourceChange{
			Resource: resourceRefOf(cur),
			Origin:   origin,
			Action:   semantic.Delete,
			Before:   snapshotOf(cur),
			Apply:    deleteApply(),
		}, true, nil
	default:
		return semantic.ResourceChange{}, false, nil
	}
}

func helmDrift(freshInstall bool, pairs []helmPair, live map[string]*unstructured.Unstructured) ([]semantic.ResourceDrift, error) {
	if freshInstall {
		return []semantic.ResourceDrift{}, nil
	}
	out := make([]semantic.ResourceDrift, 0)
	for _, p := range pairs {
		if p.previous == nil || p.previous.generateName {
			continue
		}
		key := resourceMatchKey(p.previous.id)
		cur := live[key]
		if cur == nil {
			out = append(out, semantic.ResourceDrift{
				Resource: resourceRefOf(p.previous.effective),
				Kind:     semantic.DriftMissing,
			})
			continue
		}
		projected, err := semantic.ProjectOntoDeclared(cur.Object, p.previous.effective.Object)
		if err != nil {
			return nil, fmt.Errorf("drift %s: %w", p.previous.id.Name, err)
		}
		fields, err := semantic.DiffFields(p.previous.effective.Object, projected)
		if err != nil {
			return nil, fmt.Errorf("drift %s: %w", p.previous.id.Name, err)
		}
		if len(fields) == 0 {
			continue
		}
		out = append(out, semantic.ResourceDrift{
			Resource: resourceRefOf(p.previous.effective),
			Kind:     semantic.DriftModified,
			Fields:   fields,
		})
	}
	return out, nil
}
