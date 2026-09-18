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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/plan/semantic"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type crdSurface struct {
	served   map[schema.GroupVersionKind]apiDesc
	unserved map[schema.GroupVersionKind]string
	byGVR    map[schema.GroupVersionResource]apiDesc
}

type apiDesc struct {
	gvk           schema.GroupVersionKind
	gvr           schema.GroupVersionResource
	cluster       bool
	crdName       string
	missingEntire bool
}

type crdAPI struct {
	Name     string
	Group    string
	Kind     string
	Plural   string
	Cluster  bool
	Versions []string
	Unserved []string
}

func newCRDSurface() *crdSurface {
	return &crdSurface{
		served:   make(map[schema.GroupVersionKind]apiDesc),
		unserved: make(map[schema.GroupVersionKind]string),
		byGVR:    make(map[schema.GroupVersionResource]apiDesc),
	}
}

func planCRDs(ctx context.Context, cluster ClusterReader, crds []extras.Object, policy extras.Policy) ([]semantic.ResourceChange, *crdSurface, error) {
	surface := newCRDSurface()
	if len(crds) == 0 {
		return nil, surface, nil
	}
	changes := make([]semantic.ResourceChange, 0, len(crds))
	origin := semantic.ResourceOrigin{Kind: semantic.OriginCRD}
	order := 1
	for _, o := range crds {
		typed, err := extras.DecodeCRD(o)
		if err != nil {
			return nil, nil, err
		}
		id := ResourceIdentity{
			Group:   "apiextensions.k8s.io",
			Version: "v1",
			Kind:    "CustomResourceDefinition",
			Name:    typed.Name,
		}
		mapping, mapErr := cluster.Mapping(id.GroupVersionKind())
		if mapErr != nil {
			return nil, nil, fmt.Errorf("resolve CRD mapping for %s: %w", typed.Name, mapErr)
		}
		live, err := cluster.Get(ctx, locatorFromMapping(id, mapping))
		if err != nil && !apierrors.IsNotFound(err) {
			return nil, nil, fmt.Errorf("get CRD %s: %w", typed.Name, err)
		}
		missing := apierrors.IsNotFound(err)
		if missing {
			live = nil
		}

		var change *semantic.ResourceChange
		var final *unstructured.Unstructured
		switch {
		case policy == extras.PolicyCreate && missing:
			body, createErr := extras.CreateObject(o)
			if createErr != nil {
				return nil, nil, createErr
			}
			final = body
			change = &semantic.ResourceChange{
				Resource: semantic.ResourceRef{
					APIVersion: "apiextensions.k8s.io/v1",
					Kind:       "CustomResourceDefinition",
					Name:       typed.Name,
				},
				Origin:     origin,
				Action:     semantic.Create,
				After:      snapshotOf(body),
				Apply:      writeCreate(),
				ApplyOrder: order,
			}
		case policy == extras.PolicyCreate:
			final = live
		case policy == extras.PolicyCreateReplace:
			body, applyErr := extras.ApplyObject(o)
			if applyErr != nil {
				return nil, nil, applyErr
			}
			final = body
			ref := semantic.ResourceRef{
				APIVersion: "apiextensions.k8s.io/v1",
				Kind:       "CustomResourceDefinition",
				Name:       typed.Name,
			}
			if missing {
				change = &semantic.ResourceChange{
					Resource:   ref,
					Origin:     origin,
					Action:     semantic.Create,
					After:      snapshotOf(body),
					Apply:      writeCRDApply(),
					ApplyOrder: order,
				}
			} else {
				projected, projErr := semantic.ProjectOntoDeclared(live.Object, body.Object)
				if projErr != nil {
					return nil, nil, fmt.Errorf("CRD %s: %w", typed.Name, projErr)
				}
				change = &semantic.ResourceChange{
					Resource:   ref,
					Origin:     origin,
					Action:     semantic.Update,
					Before:     snapshotOf(&unstructured.Unstructured{Object: projected}),
					After:      snapshotOf(body),
					Apply:      writeCRDApply(),
					ApplyOrder: order,
				}
			}
		}
		if change != nil {
			changes = append(changes, *change)
			order++
		}
		api, err := crdAPIFromObject(final, typed.Name)
		if err != nil {
			return nil, nil, err
		}
		if addErr := surface.add(api, missing); addErr != nil {
			return nil, nil, addErr
		}
	}
	return changes, surface, nil
}

func (s *crdSurface) add(api crdAPI, missingEntire bool) error {
	for _, ver := range api.Versions {
		gvk := schema.GroupVersionKind{Group: api.Group, Version: ver, Kind: api.Kind}
		gvr := schema.GroupVersionResource{Group: api.Group, Version: ver, Resource: api.Plural}
		if err := s.addServed(apiDesc{
			gvk:           gvk,
			gvr:           gvr,
			cluster:       api.Cluster,
			crdName:       api.Name,
			missingEntire: missingEntire,
		}); err != nil {
			return err
		}
	}
	for _, ver := range api.Unserved {
		gvk := schema.GroupVersionKind{Group: api.Group, Version: ver, Kind: api.Kind}
		if err := s.addUnserved(gvk, api.Name); err != nil {
			return err
		}
	}
	return nil
}

func (s *crdSurface) addServed(d apiDesc) error {
	if existing, ok := s.served[d.gvk]; ok && (existing.gvr != d.gvr || existing.cluster != d.cluster || existing.crdName != d.crdName) {
		return fmt.Errorf("conflicting CRD API %s/%s/%s: CRD %s and %s", d.gvk.Group, d.gvk.Version, d.gvk.Kind, existing.crdName, d.crdName)
	}
	if existing, ok := s.byGVR[d.gvr]; ok && (existing.gvk != d.gvk || existing.cluster != d.cluster || existing.crdName != d.crdName) {
		return fmt.Errorf("conflicting CRD resource %s/%s/%s: CRD %s and %s", d.gvr.Group, d.gvr.Version, d.gvr.Resource, existing.crdName, d.crdName)
	}
	s.served[d.gvk] = d
	s.byGVR[d.gvr] = d
	return nil
}

func (s *crdSurface) crdForKind(gvk schema.GroupVersionKind) (string, bool) {
	for served, d := range s.served {
		if served.Group == gvk.Group && served.Kind == gvk.Kind {
			return d.crdName, true
		}
	}
	return "", false
}

func (s *crdSurface) addUnserved(gvk schema.GroupVersionKind, crdName string) error {
	if existing, ok := s.served[gvk]; ok {
		return fmt.Errorf("CRD %s marks %s unserved but CRD %s serves it", crdName, gvk, existing.crdName)
	}
	if name, ok := s.unserved[gvk]; ok && name != crdName {
		return fmt.Errorf("conflicting unserved API %s: CRD %s and %s", gvk, name, crdName)
	}
	s.unserved[gvk] = crdName
	return nil
}

func crdAPIFromObject(obj *unstructured.Unstructured, name string) (crdAPI, error) {
	if obj == nil {
		return crdAPI{}, fmt.Errorf("CRD %s: missing final object", name)
	}
	group, _, err := unstructured.NestedString(obj.Object, "spec", "group")
	if err != nil {
		return crdAPI{}, fmt.Errorf("CRD %s spec.group: %w", name, err)
	}
	if group == "" {
		return crdAPI{}, fmt.Errorf("CRD %s spec.group is empty", name)
	}
	kind, _, err := unstructured.NestedString(obj.Object, "spec", "names", "kind")
	if err != nil {
		return crdAPI{}, fmt.Errorf("CRD %s spec.names.kind: %w", name, err)
	}
	if kind == "" {
		return crdAPI{}, fmt.Errorf("CRD %s spec.names.kind is empty", name)
	}
	plural, _, err := unstructured.NestedString(obj.Object, "spec", "names", "plural")
	if err != nil {
		return crdAPI{}, fmt.Errorf("CRD %s spec.names.plural: %w", name, err)
	}
	if plural == "" {
		return crdAPI{}, fmt.Errorf("CRD %s spec.names.plural is empty", name)
	}
	scope, _, err := unstructured.NestedString(obj.Object, "spec", "scope")
	if err != nil {
		return crdAPI{}, fmt.Errorf("CRD %s spec.scope: %w", name, err)
	}
	if scope == "" {
		return crdAPI{}, fmt.Errorf("CRD %s spec.scope is empty", name)
	}
	rawVersions, _, err := unstructured.NestedSlice(obj.Object, "spec", "versions")
	if err != nil {
		return crdAPI{}, fmt.Errorf("CRD %s spec.versions: %w", name, err)
	}
	api := crdAPI{
		Name:    name,
		Group:   group,
		Kind:    kind,
		Plural:  plural,
		Cluster: scope == "Cluster",
	}
	for i, raw := range rawVersions {
		m, isMap := raw.(map[string]any)
		if !isMap {
			return crdAPI{}, fmt.Errorf("CRD %s spec.versions[%d] is not an object", name, i)
		}
		ver, _, verErr := unstructured.NestedString(m, "name")
		if verErr != nil {
			return crdAPI{}, fmt.Errorf("CRD %s spec.versions[%d].name: %w", name, i, verErr)
		}
		if ver == "" {
			return crdAPI{}, fmt.Errorf("CRD %s spec.versions[%d].name is empty", name, i)
		}
		served := false
		if servedRaw, hasServed := m["served"]; hasServed {
			b, isBool := servedRaw.(bool)
			if !isBool {
				return crdAPI{}, fmt.Errorf("CRD %s spec.versions[%d]: served is %T, want bool", name, i, servedRaw)
			}
			served = b
		}
		if served {
			api.Versions = append(api.Versions, ver)
			continue
		}
		api.Unserved = append(api.Unserved, ver)
	}
	return api, nil
}

func checkRenderedAPIs(manifests []string, surface *crdSurface) error {
	if surface == nil {
		return nil
	}
	for _, manifest := range manifests {
		objs, err := flattenManifest(manifest)
		if err != nil {
			return err
		}
		for _, obj := range objs {
			gvk := obj.GroupVersionKind()
			if name, ok := surface.unserved[gvk]; ok {
				return fmt.Errorf("API %s/%s/%s is not served by CRD %s after this deployment", gvk.Group, gvk.Version, gvk.Kind, name)
			}
			if _, ok := surface.served[gvk]; ok {
				continue
			}
			if name, ok := surface.crdForKind(gvk); ok {
				return fmt.Errorf("API %s/%s/%s is not served by CRD %s after this deployment", gvk.Group, gvk.Version, gvk.Kind, name)
			}
		}
	}
	return nil
}
