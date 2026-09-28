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
	"bytes"
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/resource"

	"deployah.dev/deployah/internal/plan/semantic"
)

// logicalIdentity pairs a named resource by group, kind, effective
// namespace, and name. API version is left out. generateName has none.
type logicalIdentity struct {
	Group     string
	Kind      string
	Namespace string
	Name      string
}

func (id logicalIdentity) String() string {
	if id.Group == "" {
		return id.Kind + " " + id.Namespace + "/" + id.Name
	}
	return id.Group + "/" + id.Kind + " " + id.Namespace + "/" + id.Name
}

// declarationKey pairs a nameless generateName resource. It is not
// logical identity, and it cannot find a live object.
type declarationKey struct {
	Group        string
	Kind         string
	Namespace    string
	GenerateName string
}

func (k declarationKey) String() string {
	if k.Group == "" {
		return k.Kind + " " + k.Namespace + "/generateName=" + k.GenerateName
	}
	return k.Group + "/" + k.Kind + " " + k.Namespace + "/generateName=" + k.GenerateName
}

// declaration is one flattened Previous or Desired object. Pairing
// uses effectiveNamespace and does not write it back. A name sets
// identity. generateName sets pairKey. mapping is this object's REST
// mapping, so Drift GETs Previous on its own.
type declaration struct {
	obj                *unstructured.Unstructured
	mapping            *meta.RESTMapping
	effectiveNamespace string
	namespaced         bool
	identity           *logicalIdentity
	pairKey            *declarationKey
}

func (d declaration) matchKey() string {
	if d.identity != nil {
		id := d.identity
		return "id\x00" + id.Group + "\x00" + id.Kind + "\x00" + id.Namespace + "\x00" + id.Name
	}
	k := d.pairKey
	return "gen\x00" + k.Group + "\x00" + k.Kind + "\x00" + k.Namespace + "\x00" + k.GenerateName
}

func flattenManifest(manifest string) ([]*unstructured.Unstructured, error) {
	if strings.TrimSpace(manifest) == "" {
		return nil, nil
	}
	infos, err := resource.NewLocalBuilder().
		ContinueOnError().
		Flatten().
		Unstructured().
		Stream(bytes.NewBufferString(manifest), "manifest").
		Do().Infos()
	if err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	out := make([]*unstructured.Unstructured, 0, len(infos))
	for _, info := range infos {
		if u, ok := info.Object.(*unstructured.Unstructured); ok {
			out = append(out, u.DeepCopy())
			continue
		}
		m, convErr := runtime.DefaultUnstructuredConverter.ToUnstructured(info.Object)
		if convErr != nil {
			return nil, fmt.Errorf("convert object to unstructured: %w", convErr)
		}
		out = append(out, &unstructured.Unstructured{Object: m})
	}
	return out, nil
}

// generateNameState reports whether obj is a generateName resource.
// Name and generateName together is an error. Both empty is an error.
func generateNameState(obj *unstructured.Unstructured) (bool, error) {
	name := obj.GetName()
	generateName := obj.GetGenerateName()
	if name == "" && generateName != "" {
		return true, nil
	}
	if name != "" && generateName != "" {
		return false, errors.New("metadata.name and metadata.generateName cannot both be set")
	}
	if name == "" {
		return false, errors.New("resource requires metadata.name or metadata.generateName")
	}
	return false, nil
}

func declareAll(mapper RESTMapper, objs []*unstructured.Unstructured, releaseNamespace, side string) ([]declaration, error) {
	out := make([]declaration, 0, len(objs))
	seenID := make(map[logicalIdentity]struct{}, len(objs))
	seenGen := make(map[declarationKey]struct{})
	for _, obj := range objs {
		d, loadErr := declareOne(mapper, obj, releaseNamespace, side)
		if loadErr != nil {
			return nil, loadErr
		}
		if d.identity != nil {
			if _, ok := seenID[*d.identity]; ok {
				return nil, fmt.Errorf("%s: duplicate resource %s", side, d.identity)
			}
			seenID[*d.identity] = struct{}{}
		} else {
			if _, ok := seenGen[*d.pairKey]; ok {
				return nil, fmt.Errorf("%s: ambiguous generateName pairing %s", side, d.pairKey)
			}
			seenGen[*d.pairKey] = struct{}{}
		}
		out = append(out, d)
	}
	return out, nil
}

func declareOne(mapper RESTMapper, obj *unstructured.Unstructured, releaseNamespace, side string) (declaration, error) {
	genOnly, err := generateNameState(obj)
	if err != nil {
		return declaration{}, fmt.Errorf("%s: %s: %w", side, resourceLabel(obj), err)
	}
	gvk := obj.GroupVersionKind()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return declaration{}, fmt.Errorf("%s: resolve resource mapping for %s %s: %w", side, gvk, resourceLabel(obj), err)
	}
	if mapping == nil || mapping.Scope == nil {
		return declaration{}, fmt.Errorf("%s: resolve resource mapping for %s %s: missing scope", side, gvk, resourceLabel(obj))
	}
	namespaced := mapping.Scope.Name() == meta.RESTScopeNameNamespace
	effective := ""
	if namespaced {
		effective = obj.GetNamespace()
		if effective == "" {
			effective = releaseNamespace
		}
	}
	d := declaration{
		obj:                obj,
		mapping:            mapping,
		effectiveNamespace: effective,
		namespaced:         namespaced,
	}
	if genOnly {
		d.pairKey = &declarationKey{
			Group:        gvk.Group,
			Kind:         gvk.Kind,
			Namespace:    effective,
			GenerateName: obj.GetGenerateName(),
		}
		return d, nil
	}
	d.identity = &logicalIdentity{
		Group:     gvk.Group,
		Kind:      gvk.Kind,
		Namespace: effective,
		Name:      obj.GetName(),
	}
	return d, nil
}

func resourceLabel(obj *unstructured.Unstructured) string {
	name := obj.GetName()
	if name == "" {
		name = "generateName=" + obj.GetGenerateName()
	}
	ns := obj.GetNamespace()
	if ns == "" {
		return obj.GetKind() + "/" + name
	}
	return obj.GetKind() + " " + ns + "/" + name
}

// comparisonCopy deep-copies an object for field comparison. If a
// namespaced object omitted its namespace, the copy gets the effective
// one. A cluster-scoped namespace stays as declared.
func comparisonCopy(d declaration) map[string]any {
	cp := d.obj.DeepCopy()
	if d.namespaced && cp.GetNamespace() == "" {
		cp.SetNamespace(d.effectiveNamespace)
	}
	if cp.Object == nil {
		return map[string]any{}
	}
	return cp.Object
}

// diffDeclared pairs Previous with Desired. Creates and updates keep
// Desired order. Deletes follow, in Previous order.
func diffDeclared(previous, desired []declaration) ([]semantic.ResourceChange, error) {
	prevBy := make(map[string]declaration, len(previous))
	for _, d := range previous {
		prevBy[d.matchKey()] = d
	}
	used := make(map[string]struct{}, len(previous))
	changes := make([]semantic.ResourceChange, 0)
	for _, d := range desired {
		key := d.matchKey()
		prev, ok := prevBy[key]
		if !ok {
			changes = append(changes, createChange(d))
			continue
		}
		used[key] = struct{}{}
		change, changed, diffErr := updateIfDifferent(prev, d)
		if diffErr != nil {
			return nil, diffErr
		}
		if changed {
			changes = append(changes, change)
		}
	}
	for _, d := range previous {
		if _, ok := used[d.matchKey()]; ok {
			continue
		}
		changes = append(changes, deleteChange(d))
	}
	return changes, nil
}

func createChange(d declaration) semantic.ResourceChange {
	return semantic.ResourceChange{
		Resource: resourceRef(d),
		Action:   semantic.Create,
		After:    snapshotOf(d.obj),
	}
}

func deleteChange(d declaration) semantic.ResourceChange {
	return semantic.ResourceChange{
		Resource: resourceRef(d),
		Action:   semantic.Delete,
		Before:   snapshotOf(d.obj),
	}
}

func updateIfDifferent(prev, desired declaration) (semantic.ResourceChange, bool, error) {
	fields, err := semantic.DiffFields(comparisonCopy(prev), comparisonCopy(desired))
	if err != nil {
		return semantic.ResourceChange{}, false, fmt.Errorf("diff %s: %w", resourceRef(desired), err)
	}
	if len(fields) == 0 {
		return semantic.ResourceChange{}, false, nil
	}
	return semantic.ResourceChange{
		Resource: resourceRef(desired),
		Action:   semantic.Update,
		Before:   snapshotOf(prev.obj),
		After:    snapshotOf(desired.obj),
		Fields:   fields,
	}, true, nil
}

func resourceRef(d declaration) semantic.ResourceRef {
	ref := semantic.ResourceRef{
		APIVersion: d.obj.GetAPIVersion(),
		Kind:       d.obj.GetKind(),
		Namespace:  d.effectiveNamespace,
		Name:       d.obj.GetName(),
	}
	if ref.Name == "" {
		ref.GenerateName = d.obj.GetGenerateName()
	}
	return ref
}

func snapshotOf(obj *unstructured.Unstructured) *semantic.ResourceSnapshot {
	if obj == nil {
		return nil
	}
	if obj.Object == nil {
		return &semantic.ResourceSnapshot{}
	}
	// [semantic.New] copies the map. Passing Object here avoids
	// runtime.DeepCopyJSON, which panics on ordinary Go int values.
	return &semantic.ResourceSnapshot{Object: obj.Object}
}
