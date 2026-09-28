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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/dynamic"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LiveReader reads Live objects for Drift. It only gets and lists.
// It does not write or run a server-side dry-run. Helm still renders
// Desired with its own client-side dry-run on [SemanticBuildClient].
type LiveReader interface {
	// Get returns the named object. A missing object is a NotFound
	// error. namespace applies only when mapping is namespaced.
	Get(ctx context.Context, mapping *meta.RESTMapping, namespace, name string) (*unstructured.Unstructured, error)
	// List returns objects that match selector. namespace applies only
	// when mapping is namespaced. selector must be non-nil.
	List(ctx context.Context, mapping *meta.RESTMapping, namespace string, selector labels.Selector) ([]unstructured.Unstructured, error)
}

// DynamicLiveReader is a [LiveReader] backed by a dynamic client.
// It sends only get and list requests.
type DynamicLiveReader struct {
	client dynamic.Interface
}

// NewDynamicLiveReader returns a reader for client. client must be non-nil
// before Get or List is called.
func NewDynamicLiveReader(client dynamic.Interface) *DynamicLiveReader {
	return &DynamicLiveReader{client: client}
}

var _ LiveReader = (*DynamicLiveReader)(nil)

// Get implements [LiveReader].
func (r *DynamicLiveReader) Get(ctx context.Context, mapping *meta.RESTMapping, namespace, name string) (*unstructured.Unstructured, error) {
	ri, err := r.resource(mapping, namespace)
	if err != nil {
		return nil, err
	}
	obj, err := ri.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return obj.DeepCopy(), nil
}

// List implements [LiveReader].
func (r *DynamicLiveReader) List(ctx context.Context, mapping *meta.RESTMapping, namespace string, selector labels.Selector) ([]unstructured.Unstructured, error) {
	if selector == nil {
		return nil, fmt.Errorf("live list requires a label selector")
	}
	ri, err := r.resource(mapping, namespace)
	if err != nil {
		return nil, err
	}
	list, err := ri.List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}
	out := make([]unstructured.Unstructured, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, *list.Items[i].DeepCopy())
	}
	return out, nil
}

func (r *DynamicLiveReader) resource(mapping *meta.RESTMapping, namespace string) (dynamic.ResourceInterface, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("live read requires a dynamic client")
	}
	if mapping == nil || mapping.Scope == nil {
		return nil, fmt.Errorf("live read requires a REST mapping with scope")
	}
	ri := r.client.Resource(mapping.Resource)
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		return ri.Namespace(namespace), nil
	}
	return ri, nil
}
