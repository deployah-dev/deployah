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

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/predict"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

const limitationTargetNamespace = "target namespace is created earlier in this deployment"

type prereqCluster struct {
	inner           predict.Cluster
	missingTargetNS bool
	targetNS        string
}

func newPrereqCluster(inner predict.Cluster, missingTargetNS bool, targetNS string) *prereqCluster {
	return &prereqCluster{
		inner:           inner,
		missingTargetNS: missingTargetNS,
		targetNS:        targetNS,
	}
}

var _ predict.Cluster = (*prereqCluster)(nil)

func (c *prereqCluster) Get(ctx context.Context, id predict.Identity) (*unstructured.Unstructured, error) {
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
	return c.inner.Mapping(gvk)
}

func (c *prereqCluster) needsSyntheticApply(obj *unstructured.Unstructured) bool {
	return c.namespacedInMissingTarget(obj)
}

func limitationDiagnostics(c *prereqCluster, results []predict.Result) []semantic.Diagnostic {
	if c == nil {
		return nil
	}
	var diags []semantic.Diagnostic
	for _, r := range results {
		obj := firstObject(r.Predicted, r.Live)
		if obj == nil || !c.needsSyntheticApply(obj) {
			continue
		}
		diags = append(diags, limitationDiagnostic(resourceRef(r.Identity, obj), limitationTargetNamespace))
	}
	return diags
}

func (c *prereqCluster) namespacedInMissingTarget(obj *unstructured.Unstructured) bool {
	if obj == nil || !c.missingTargetNS || obj.GetNamespace() != c.targetNS {
		return false
	}
	mapping, err := c.inner.Mapping(obj.GroupVersionKind())
	return err == nil && mapping.Scope.Name() == meta.RESTScopeNameNamespace
}
