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

	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/predict"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

const (
	limitationTargetNamespace = "target namespace is created earlier in this deployment"
	limitationCRDSpecChange   = "prediction used the currently installed CRD, but that CRD's spec changes before Helm executes"
)

type prereqCluster struct {
	inner           predict.Cluster
	missingTargetNS bool
	targetNS        string
	surface         *crdSurface
}

func newPrereqCluster(inner predict.Cluster, missingTargetNS bool, targetNS string, surface *crdSurface) *prereqCluster {
	if surface == nil {
		surface = newCRDSurface()
	}
	return &prereqCluster{
		inner:           inner,
		missingTargetNS: missingTargetNS,
		targetNS:        targetNS,
		surface:         surface,
	}
}

var _ predict.Cluster = (*prereqCluster)(nil)

func (c *prereqCluster) Get(ctx context.Context, id predict.Identity) (*unstructured.Unstructured, error) {
	gvk := id.GroupVersionKind()
	if desc, ok := c.surface.served[gvk]; ok && desc.missingEntire {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: gvk.Group, Resource: desc.gvr.Resource}, id.Name)
	}
	if c.missingTargetNS && id.Namespace != "" && id.Namespace == c.targetNS {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: id.Group, Resource: id.Kind}, id.Name)
	}
	return c.inner.Get(ctx, id)
}

func (c *prereqCluster) Create(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	return c.inner.Create(ctx, obj)
}

func (c *prereqCluster) Apply(ctx context.Context, obj *unstructured.Unstructured, opts predict.ApplyOptions) (*unstructured.Unstructured, error) {
	if c.needsSyntheticApply(obj) {
		return obj.DeepCopy(), nil
	}
	return c.inner.Apply(ctx, obj, opts)
}

func (c *prereqCluster) JSONPatch(ctx context.Context, id predict.Identity, patch []byte) error {
	return c.inner.JSONPatch(ctx, id, patch)
}

func (c *prereqCluster) Delete(ctx context.Context, id predict.Identity) error {
	return c.inner.Delete(ctx, id)
}

func (c *prereqCluster) Mapping(gvk schema.GroupVersionKind) (*meta.RESTMapping, error) {
	if name, ok := c.surface.unserved[gvk]; ok {
		return nil, fmt.Errorf("API %s/%s/%s is not served by CRD %s after this deployment", gvk.Group, gvk.Version, gvk.Kind, name)
	}
	mapping, err := c.inner.Mapping(gvk)
	if err == nil {
		return mapping, nil
	}
	if !meta.IsNoMatchError(err) {
		return nil, err
	}
	desc, ok := c.surface.served[gvk]
	if !ok {
		return nil, err
	}
	if desc.missingEntire {
		scope := meta.RESTScopeNamespace
		if desc.cluster {
			scope = meta.RESTScopeRoot
		}
		return &meta.RESTMapping{
			Resource:         desc.gvr,
			GroupVersionKind: desc.gvk,
			Scope:            scope,
		}, nil
	}
	return nil, fmt.Errorf("cannot predict %s: CRD %s would add this API but it is not discoverable on the cluster yet", gvk, desc.crdName)
}

func (c *prereqCluster) needsSyntheticApply(obj *unstructured.Unstructured) bool {
	if obj == nil {
		return false
	}
	gvk := obj.GroupVersionKind()
	if desc, ok := c.surface.served[gvk]; ok && desc.missingEntire {
		return true
	}
	return c.namespacedInMissingTarget(obj)
}

func limitationDiagnostics(c *prereqCluster, results []predict.Result) []semantic.Diagnostic {
	if c == nil {
		return nil
	}
	var diags []semantic.Diagnostic
	for _, r := range results {
		obj := firstObject(r.Predicted, r.Live)
		ref := resourceRef(r.Identity, obj)
		seen := make(map[string]struct{})
		add := func(reason string) {
			if reason == "" {
				return
			}
			if _, ok := seen[reason]; ok {
				return
			}
			seen[reason] = struct{}{}
			diags = append(diags, limitationDiagnostic(ref, reason))
		}
		if obj != nil && c.needsSyntheticApply(obj) {
			gvk := obj.GroupVersionKind()
			if desc, ok := c.surface.served[gvk]; ok && desc.missingEntire {
				add(fmt.Sprintf("API %s/%s/%s becomes available after CRD %s is created earlier in this deployment", gvk.Group, gvk.Version, gvk.Kind, desc.crdName))
			}
			if c.namespacedInMissingTarget(obj) {
				add(limitationTargetNamespace)
			}
		}
		if _, ok := c.surface.specChanged[r.Identity.GroupVersionKind()]; ok {
			add(limitationCRDSpecChange)
		}
	}
	return diags
}

func (c *prereqCluster) namespacedInMissingTarget(obj *unstructured.Unstructured) bool {
	if obj == nil || !c.missingTargetNS || obj.GetNamespace() != c.targetNS {
		return false
	}
	gvk := obj.GroupVersionKind()
	mapping, err := c.inner.Mapping(gvk)
	if err == nil {
		return mapping.Scope.Name() == meta.RESTScopeNameNamespace
	}
	desc, ok := c.surface.served[gvk]
	return ok && !desc.cluster
}
