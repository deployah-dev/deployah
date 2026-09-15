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

	"helm.sh/helm/v4/pkg/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/predict"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func predictNamespace(ctx context.Context, cluster predict.Cluster, op helm.Operation, name string) (*semantic.ResourceChange, bool, error) {
	id := predict.Identity{Version: "v1", Kind: "Namespace", Name: name}
	if op == helm.OperationUpgrade {
		_, err := cluster.Get(ctx, id)
		if apierrors.IsNotFound(err) {
			return nil, false, fmt.Errorf("upgrade requires namespace %q to exist", name)
		}
		if err != nil {
			return nil, false, fmt.Errorf("get namespace %s: %w", name, err)
		}
		return nil, false, nil
	}
	if op != helm.OperationInstall {
		return nil, false, nil
	}

	live, err := cluster.Get(ctx, id)
	missing := apierrors.IsNotFound(err)
	if missing {
		live = nil
		err = nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get namespace %s: %w", name, err)
	}

	desired := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name":   name,
			"labels": map[string]any{"name": name},
		},
	}}
	predicted, err := cluster.Apply(ctx, desired, predict.ApplyOptions{
		FieldManager:   kube.ManagedFieldsManager,
		ForceConflicts: false,
	})
	if err != nil {
		return nil, missing, fmt.Errorf("predict namespace %s: %w", name, err)
	}

	ref := semantic.ResourceRef{APIVersion: "v1", Kind: "Namespace", Name: name}
	origin := semantic.ResourceOrigin{Kind: semantic.OriginNamespace}
	if live == nil {
		return &semantic.ResourceChange{
			Resource:   ref,
			Origin:     origin,
			Action:     semantic.Create,
			After:      snapshotOf(predicted),
			Apply:      writeApply(),
			ApplyOrder: 1,
		}, true, nil
	}
	if predict.EqualPredictedState(live, predicted) {
		return nil, false, nil
	}
	return &semantic.ResourceChange{
		Resource:   ref,
		Origin:     origin,
		Action:     semantic.Update,
		Before:     snapshotOf(live),
		After:      snapshotOf(predicted),
		Apply:      writeApply(),
		ApplyOrder: 1,
	}, false, nil
}

func checkInstallNamespaceOverlap(manifest, namespace string) error {
	objs, err := predict.FlattenManifest(manifest)
	if err != nil {
		return fmt.Errorf("parse desired manifest: %w", err)
	}
	for _, obj := range objs {
		if obj.GetAPIVersion() == "v1" && obj.GetKind() == "Namespace" && obj.GetName() == namespace {
			return fmt.Errorf("install cannot plan target namespace %q: helm CreateNamespace and the rendered manifest both write it, and those sequential server-side applies cannot be composed", namespace)
		}
	}
	return nil
}
