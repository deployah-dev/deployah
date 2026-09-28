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

package plan_test

import (
	"context"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type liveCall struct {
	verb      string
	resource  string
	version   string
	namespace string
	name      string
	selector  string
}

type storedLive struct {
	gvr       schema.GroupVersionResource
	namespace string
	name      string
	obj       *unstructured.Unstructured
}

// fakeLive is a read-only LiveReader. Get returns not-found unless an
// object was added for that mapping, namespace, and name.
type fakeLive struct {
	mu         sync.Mutex
	items      []storedLive
	getErrors  map[string]error
	listErrors map[string]error
	calls      []liveCall
}

func (f *fakeLive) add(gvr schema.GroupVersionResource, namespace, name string, obj *unstructured.Unstructured) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items = append(f.items, storedLive{gvr: gvr, namespace: namespace, name: name, obj: obj})
}

func (f *fakeLive) failGet(name string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErrors == nil {
		f.getErrors = map[string]error{}
	}
	f.getErrors[name] = err
}

func (f *fakeLive) failList(gvr schema.GroupVersionResource, namespace string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErrors == nil {
		f.listErrors = map[string]error{}
	}
	f.listErrors[gvr.String()+"\x00"+namespace] = err
}

func (f *fakeLive) Calls() []liveCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]liveCall, 0, len(f.calls))
	out = append(out, f.calls...)
	return out
}

func (f *fakeLive) Get(_ context.Context, mapping *meta.RESTMapping, namespace, name string) (*unstructured.Unstructured, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, liveCall{
		verb: "get", resource: mapping.Resource.String(), version: mapping.GroupVersionKind.Version,
		namespace: namespace, name: name,
	})
	if err := f.getErrors[name]; err != nil {
		return nil, err
	}
	for _, item := range f.items {
		if item.gvr == mapping.Resource && item.namespace == namespace && item.name == name {
			return item.obj.DeepCopy(), nil
		}
	}
	return nil, apierrors.NewNotFound(mapping.Resource.GroupResource(), name)
}

func (f *fakeLive) List(_ context.Context, mapping *meta.RESTMapping, namespace string, selector labels.Selector) ([]unstructured.Unstructured, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sel := ""
	if selector != nil {
		sel = selector.String()
	}
	f.calls = append(f.calls, liveCall{
		verb: "list", resource: mapping.Resource.String(), version: mapping.GroupVersionKind.Version,
		namespace: namespace, selector: sel,
	})
	if err := f.listErrors[mapping.Resource.String()+"\x00"+namespace]; err != nil {
		return nil, err
	}
	namespaced := mapping.Scope != nil && mapping.Scope.Name() == meta.RESTScopeNameNamespace
	var out []unstructured.Unstructured
	for _, item := range f.items {
		if item.gvr != mapping.Resource {
			continue
		}
		if namespaced && item.namespace != namespace {
			continue
		}
		if selector != nil && !selector.Matches(labels.Set(item.obj.GetLabels())) {
			continue
		}
		out = append(out, *item.obj.DeepCopy())
	}
	return out, nil
}
