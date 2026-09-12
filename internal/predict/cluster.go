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

package predict

import (
	"context"
	"fmt"

	"helm.sh/helm/v4/pkg/kube"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	memcached "k8s.io/client-go/discovery/cached/memory"
)

// Cluster is the Kubernetes surface [Predict] uses. The production
// implementation dry-runs every write. Tests substitute a fake.
type Cluster interface {
	Get(ctx context.Context, id Identity) (*unstructured.Unstructured, error)
	Apply(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error)
	JSONPatch(ctx context.Context, id Identity, patch []byte) error
	Delete(ctx context.Context, id Identity) error
	Mapping(gvk schema.GroupVersionKind) (*meta.RESTMapping, error)
}

type restCluster struct {
	dyn    dynamic.Interface
	mapper meta.RESTMapper
}

var _ Cluster = (*restCluster)(nil)

// NewCluster builds the production [Cluster] for cfg. Construction matches
// the existing dynamic-client plus discovery REST-mapper stack. Apply,
// JSONPatch, and Delete always send server dry-run.
func NewCluster(cfg *rest.Config) (Cluster, error) {
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build dynamic client: %w", err)
	}
	disco, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build discovery client: %w", err)
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memcached.NewMemCacheClient(disco))
	return newRESTCluster(dyn, mapper), nil
}

func newRESTCluster(dyn dynamic.Interface, mapper meta.RESTMapper) *restCluster {
	return &restCluster{dyn: dyn, mapper: mapper}
}

func (c *restCluster) Get(ctx context.Context, id Identity) (*unstructured.Unstructured, error) {
	ri, err := c.resource(id.GroupVersionKind(), id.Namespace)
	if err != nil {
		return nil, err
	}
	return ri.Get(ctx, id.Name, metav1.GetOptions{})
}

func (c *restCluster) Apply(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	data, err := runtime.Encode(unstructured.UnstructuredJSONScheme, obj)
	if err != nil {
		return nil, fmt.Errorf("failed to encode object %s/%s %s: %w",
			obj.GetNamespace(), obj.GetName(), obj.GroupVersionKind().String(), err)
	}
	ri, err := c.resource(obj.GroupVersionKind(), obj.GetNamespace())
	if err != nil {
		return nil, err
	}
	predicted, err := ri.Patch(ctx, obj.GetName(), types.ApplyPatchType, data, applyPatchOptions())
	if err != nil {
		if apierrors.IsConflict(err) {
			return nil, fmt.Errorf("conflict occurred while applying object %s/%s %s: %w",
				obj.GetNamespace(), obj.GetName(), obj.GroupVersionKind().String(), err)
		}
		return nil, fmt.Errorf("server-side apply failed for object %s/%s %s: %w",
			obj.GetNamespace(), obj.GetName(), obj.GroupVersionKind().String(), err)
	}
	return predicted, nil
}

func (c *restCluster) JSONPatch(ctx context.Context, id Identity, patch []byte) error {
	ri, err := c.resource(id.GroupVersionKind(), id.Namespace)
	if err != nil {
		return err
	}
	_, err = ri.Patch(ctx, id.Name, types.JSONPatchType, patch, jsonPatchOptions())
	return err
}

func (c *restCluster) Delete(ctx context.Context, id Identity) error {
	ri, err := c.resource(id.GroupVersionKind(), id.Namespace)
	if err != nil {
		return err
	}
	return ri.Delete(ctx, id.Name, deleteOptions())
}

func (c *restCluster) Mapping(gvk schema.GroupVersionKind) (*meta.RESTMapping, error) {
	return c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
}

func (c *restCluster) resource(gvk schema.GroupVersionKind, namespace string) (dynamic.ResourceInterface, error) {
	mapping, err := c.Mapping(gvk)
	if err != nil {
		return nil, err
	}
	nri := c.dyn.Resource(mapping.Resource)
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		return nri.Namespace(namespace), nil
	}
	return nri, nil
}

func applyPatchOptions() metav1.PatchOptions {
	return metav1.PatchOptions{
		DryRun:          []string{metav1.DryRunAll},
		FieldManager:    kube.ManagedFieldsManager,
		Force:           new(false),
		FieldValidation: metav1.FieldValidationStrict,
	}
}

func jsonPatchOptions() metav1.PatchOptions {
	return metav1.PatchOptions{
		DryRun:          []string{metav1.DryRunAll},
		FieldManager:    kube.ManagedFieldsManager,
		FieldValidation: metav1.FieldValidationStrict,
	}
}

func deleteOptions() metav1.DeleteOptions {
	return metav1.DeleteOptions{
		DryRun:            []string{metav1.DryRunAll},
		PropagationPolicy: new(metav1.DeletePropagationBackground),
	}
}
