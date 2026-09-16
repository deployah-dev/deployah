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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	memcached "k8s.io/client-go/discovery/cached/memory"
)

// ResourceIdentity is one Kubernetes object's identity for Live reads.
type ResourceIdentity struct {
	Group     string
	Version   string
	Kind      string
	Namespace string
	Name      string
}

// GroupVersionKind returns the API group, version, and kind of id.
func (id ResourceIdentity) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: id.Group, Version: id.Version, Kind: id.Kind}
}

// ResourceLocator is a resolved GET target. Resource and Namespaced come
// from discovery or the extras CRD surface. [ClusterReader.Get] uses
// them and does not discover again.
type ResourceLocator struct {
	Identity   ResourceIdentity
	Resource   schema.GroupVersionResource
	Namespaced bool
}

// ClusterReader is the read-only Kubernetes surface [BuildSemanticPlan]
// uses. Get is a real GET. Mapping is discovery only. There are no
// Create, Apply, Patch, or Delete methods.
type ClusterReader interface {
	Get(ctx context.Context, loc ResourceLocator) (*unstructured.Unstructured, error)
	Mapping(gvk schema.GroupVersionKind) (*meta.RESTMapping, error)
}

type restClusterReader struct {
	dyn    dynamic.Interface
	mapper meta.RESTMapper
}

var _ ClusterReader = (*restClusterReader)(nil)

// NewClusterReader builds the production [ClusterReader] for cfg.
func NewClusterReader(cfg *rest.Config) (ClusterReader, error) {
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build dynamic client: %w", err)
	}
	disco, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build discovery client: %w", err)
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memcached.NewMemCacheClient(disco))
	return &restClusterReader{dyn: dyn, mapper: mapper}, nil
}

func (c *restClusterReader) Get(ctx context.Context, loc ResourceLocator) (*unstructured.Unstructured, error) {
	nri := c.dyn.Resource(loc.Resource)
	var ri dynamic.ResourceInterface
	if loc.Namespaced {
		ri = nri.Namespace(loc.Identity.Namespace)
	} else {
		ri = nri
	}
	return ri.Get(ctx, loc.Identity.Name, metav1.GetOptions{})
}

func (c *restClusterReader) Mapping(gvk schema.GroupVersionKind) (*meta.RESTMapping, error) {
	return c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
}

func locatorFromMapping(id ResourceIdentity, mapping *meta.RESTMapping) ResourceLocator {
	return ResourceLocator{
		Identity:   id,
		Resource:   mapping.Resource,
		Namespaced: mapping.Scope.Name() == meta.RESTScopeNameNamespace,
	}
}

func locatorFromAPI(id ResourceIdentity, d apiDesc) ResourceLocator {
	return ResourceLocator{
		Identity:   id,
		Resource:   d.gvr,
		Namespaced: !d.cluster,
	}
}

func identityOfUnstructured(obj *unstructured.Unstructured) ResourceIdentity {
	gvk := obj.GroupVersionKind()
	return ResourceIdentity{
		Group:     gvk.Group,
		Version:   gvk.Version,
		Kind:      gvk.Kind,
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
}
