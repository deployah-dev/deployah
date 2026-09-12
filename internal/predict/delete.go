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
	"k8s.io/client-go/util/retry"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func predictPrune(ctx context.Context, cluster Cluster, previous resourceObj) (Result, error) {
	live, err := cluster.Get(ctx, previous.id)
	if apierrors.IsNotFound(err) {
		return Result{Identity: previous.id, Action: ActionNoOp}, nil
	}
	if err != nil {
		// Prediction-safety: Helm may log-and-continue on this GET.
		// Predict refuses to claim a resulting state.
		return Result{}, fmt.Errorf("could not get information about the resource %s: %w",
			resourceString(previous.obj), err)
	}
	if live.GetAnnotations()[kube.ResourcePolicyAnno] == kube.KeepPolicy {
		return Result{
			Identity:  previous.id,
			Action:    ActionNoOp,
			Live:      live,
			Predicted: live.DeepCopy(),
		}, nil
	}
	delErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return cluster.Delete(ctx, previous.id)
	})
	if delErr != nil && !apierrors.IsNotFound(delErr) {
		return Result{}, fmt.Errorf(
			"failed to delete resource namespace=%s, name=%s, kind=%s: %w",
			previous.id.Namespace, previous.id.Name, previous.id.Kind, delErr)
	}
	return Result{
		Identity: previous.id,
		Action:   ActionDelete,
		Live:     live,
	}, nil
}
