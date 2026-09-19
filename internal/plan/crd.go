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
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/predict"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

// inspectedCRD is one relevant CRD document from a source file. object.Raw
// is that document's bytes only. The source [extras.RawFile] is unchanged.
type inspectedCRD struct {
	object extras.Object
	typed  *apiextensionsv1.CustomResourceDefinition
}

type crdSurface struct {
	served      map[schema.GroupVersionKind]apiDesc
	unserved    map[schema.GroupVersionKind]string
	byGVR       map[schema.GroupVersionResource]apiDesc
	specChanged map[schema.GroupVersionKind]struct{}
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
		served:      make(map[schema.GroupVersionKind]apiDesc),
		unserved:    make(map[schema.GroupVersionKind]string),
		byGVR:       make(map[schema.GroupVersionResource]apiDesc),
		specChanged: make(map[schema.GroupVersionKind]struct{}),
	}
}

// predictCRDs inspects every non-empty YAML document in each source
// file. Inspection objects are document slices only. Source
// [extras.RawFile] bytes stay untouched for Helm.
func predictCRDs(ctx context.Context, cluster predict.Cluster, files []extras.RawFile, policy extras.Policy) ([]semantic.ResourceChange, *crdSurface, error) {
	surface := newCRDSurface()
	if len(files) == 0 {
		return nil, surface, nil
	}
	crds, inspectErr := inspectCRDDocuments(files)
	if inspectErr != nil {
		return nil, nil, inspectErr
	}
	changes := make([]semantic.ResourceChange, 0, len(crds))
	var previouslyServed []apiDesc
	origin := semantic.ResourceOrigin{Kind: semantic.OriginCRD}
	order := 1
	for _, crd := range crds {
		o := crd.object
		typed := crd.typed
		id := predict.Identity{
			Group:   "apiextensions.k8s.io",
			Version: "v1",
			Kind:    "CustomResourceDefinition",
			Name:    typed.Name,
		}
		live, err := cluster.Get(ctx, id)
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
			predicted, createErr := cluster.Create(ctx, body)
			if createErr != nil {
				return nil, nil, fmt.Errorf("create CRD %s: %w", typed.Name, createErr)
			}
			final = predicted
			change = &semantic.ResourceChange{
				Resource: semantic.ResourceRef{
					APIVersion: "apiextensions.k8s.io/v1",
					Kind:       "CustomResourceDefinition",
					Name:       typed.Name,
				},
				Origin:     origin,
				Action:     semantic.Create,
				After:      snapshotOf(predicted),
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
			predicted, applyErr := cluster.Apply(ctx, body, predict.ApplyOptions{
				FieldManager:   extras.CRDFieldManager,
				ForceConflicts: true,
			})
			if applyErr != nil {
				return nil, nil, fmt.Errorf("apply CRD %s: %w", typed.Name, applyErr)
			}
			final = predicted
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
					After:      snapshotOf(predicted),
					Apply:      writeCRDApply(),
					ApplyOrder: order,
				}
			} else {
				change = &semantic.ResourceChange{
					Resource:   ref,
					Origin:     origin,
					Action:     semantic.Update,
					Before:     snapshotOf(live),
					After:      snapshotOf(predicted),
					Apply:      writeCRDApply(),
					ApplyOrder: order,
				}
				changed, changeErr := specChanged(live, predicted)
				if changeErr != nil {
					return nil, nil, changeErr
				}
				if changed {
					if markErr := surface.markSpecChanged(live, predicted, typed.Name); markErr != nil {
						return nil, nil, markErr
					}
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
		if live != nil {
			liveAPI, liveErr := crdAPIFromObject(live, typed.Name)
			if liveErr != nil {
				return nil, nil, liveErr
			}
			for _, ver := range liveAPI.Versions {
				previouslyServed = append(previouslyServed, apiDesc{
					gvk:     schema.GroupVersionKind{Group: liveAPI.Group, Version: ver, Kind: liveAPI.Kind},
					crdName: liveAPI.Name,
				})
			}
		}
	}
	// Discovery still exposes the live CRD while prediction runs. Record APIs
	// removed by the final CRD set so Helm resources cannot be planned against
	// endpoints that disappear before Helm executes.
	for _, previous := range previouslyServed {
		if _, ok := surface.served[previous.gvk]; ok {
			continue
		}
		if err := surface.addUnserved(previous.gvk, previous.crdName); err != nil {
			return nil, nil, err
		}
	}
	return changes, surface, nil
}

// inspectCRDDocuments yields one [inspectedCRD] per relevant CRD
// document. Parsed objects are never written back into [extras.RawFile].
func inspectCRDDocuments(files []extras.RawFile) ([]inspectedCRD, error) {
	out := make([]inspectedCRD, 0, len(files))
	for i := range files {
		docs, err := yamlDocuments(files[i].Raw)
		if err != nil {
			return nil, fmt.Errorf("%s: split CRD documents: %w", files[i].Path, err)
		}
		for _, doc := range docs {
			o := extras.Object{Path: files[i].Path, Raw: doc}
			typed, decodeErr := extras.DecodeCRD(o)
			if decodeErr != nil {
				continue
			}
			if typed.Kind != "CustomResourceDefinition" {
				continue
			}
			out = append(out, inspectedCRD{object: o, typed: typed})
		}
	}
	return out, nil
}

func yamlDocuments(raw []byte) ([][]byte, error) {
	reader := yamlutil.NewYAMLReader(bufio.NewReader(bytes.NewReader(raw)))
	var docs [][]byte
	for {
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			return nil, err
		}
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}
		docs = append(docs, doc)
	}
}

func specChanged(live, predicted *unstructured.Unstructured) (bool, error) {
	if live == nil || predicted == nil {
		return false, nil
	}
	liveSpec, _, err := unstructured.NestedMap(live.Object, "spec")
	if err != nil {
		return false, fmt.Errorf("live CRD spec: %w", err)
	}
	predSpec, _, err := unstructured.NestedMap(predicted.Object, "spec")
	if err != nil {
		return false, fmt.Errorf("predicted CRD spec: %w", err)
	}
	return !equality.Semantic.DeepEqual(liveSpec, predSpec), nil
}

func (s *crdSurface) markSpecChanged(live, predicted *unstructured.Unstructured, name string) error {
	for _, obj := range []*unstructured.Unstructured{live, predicted} {
		api, err := crdAPIFromObject(obj, name)
		if err != nil {
			return err
		}
		for _, ver := range append(append([]string{}, api.Versions...), api.Unserved...) {
			gvk := schema.GroupVersionKind{Group: api.Group, Version: ver, Kind: api.Kind}
			s.specChanged[gvk] = struct{}{}
		}
	}
	return nil
}

func (s *crdSurface) add(api crdAPI, missingEntire bool) error {
	// A CRD may keep versions with served=false only. That contributes
	// no REST APIs; do not invent a served mapping.
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
	// GVK -> REST mapping must be unique. Two CRDs cannot serve the same
	// group/version/kind with different plurals, scopes, or CRD names.
	if existing, ok := s.served[d.gvk]; ok && (existing.gvr != d.gvr || existing.cluster != d.cluster || existing.crdName != d.crdName) {
		return fmt.Errorf("conflicting CRD API %s/%s/%s: CRD %s and %s", d.gvk.Group, d.gvk.Version, d.gvk.Kind, existing.crdName, d.crdName)
	}
	// GVR -> kind must be unique. Two CRDs cannot share group/version/plural
	// with different kinds, scopes, or CRD names (Widget vs Gadget both
	// serving example.com/v1/objects).
	if existing, ok := s.byGVR[d.gvr]; ok && (existing.gvk != d.gvk || existing.cluster != d.cluster || existing.crdName != d.crdName) {
		return fmt.Errorf("conflicting CRD resource %s/%s/%s: CRD %s and %s", d.gvr.Group, d.gvr.Version, d.gvr.Resource, existing.crdName, d.crdName)
	}
	s.served[d.gvk] = d
	s.byGVR[d.gvr] = d
	return nil
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
		objs, err := predict.FlattenManifest(manifest)
		if err != nil {
			return err
		}
		for _, obj := range objs {
			gvk := obj.GroupVersionKind()
			if name, ok := surface.unserved[gvk]; ok {
				return fmt.Errorf("API %s/%s/%s is not served by CRD %s after this deployment", gvk.Group, gvk.Version, gvk.Kind, name)
			}
		}
	}
	return nil
}
