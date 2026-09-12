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
	"strings"

	"helm.sh/helm/v4/pkg/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/csaupgrade"
	"k8s.io/client-go/util/retry"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func applyCreate(ctx context.Context, cluster Cluster, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	var predicted *unstructured.Unstructured
	err := retry.OnError(retry.DefaultRetry, isResourceQuotaConflict, func() error {
		var applyErr error
		predicted, applyErr = cluster.Apply(ctx, obj)
		return applyErr
	})
	if err != nil {
		return nil, err
	}
	return predicted, nil
}

func applyUpdate(ctx context.Context, cluster Cluster, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	return cluster.Apply(ctx, obj)
}

// isResourceQuotaConflict matches Helm pkg/kube isResourceQuotaConflict.
// The string match is Helm's predicate, not a local invention.
func isResourceQuotaConflict(err error) bool {
	if err == nil || !apierrors.IsConflict(err) {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Operation cannot be fulfilled on resourcequotas") &&
		strings.Contains(msg, "the object has been modified")
}

// migrateManagedFields dry-runs Helm's install-adoption CSA-to-SSA
// managed-fields patch. An empty computed patch is exact SSA. A
// non-empty patch that the API accepts sets
// [LimitationManagedFieldsMigration]. A failed dry-run is an error.
func migrateManagedFields(ctx context.Context, cluster Cluster, id Identity, live **unstructured.Unstructured) (string, error) {
	limitation := ""
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		fresh, getErr := cluster.Get(ctx, id)
		if getErr != nil {
			return fmt.Errorf("failed to get object %s/%s %s: %w",
				id.Namespace, id.Name, id.GroupVersionKind().String(), getErr)
		}
		*live = fresh
		manager := kube.ManagedFieldsManager
		patch, patchErr := csaupgrade.UpgradeManagedFieldsPatch(
			fresh, sets.New(manager), manager)
		if patchErr != nil {
			return fmt.Errorf("failed to upgrade managed fields for object %s/%s %s: %w",
				id.Namespace, id.Name, id.GroupVersionKind().String(), patchErr)
		}
		if len(patch) == 0 {
			limitation = ""
			return nil
		}
		limitation = LimitationManagedFieldsMigration
		if jsonErr := cluster.JSONPatch(ctx, id, patch); jsonErr != nil {
			if !apierrors.IsConflict(jsonErr) {
				return fmt.Errorf("failed to patch object to upgrade CSA field manager %s/%s %s: %w",
					id.Namespace, id.Name, id.GroupVersionKind().String(), jsonErr)
			}
			return jsonErr
		}
		return nil
	})
	return limitation, err
}
