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
	"errors"
	"fmt"

	"deployah.dev/deployah/internal/helm"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// Predict reports the Kubernetes resource state Deployah's Helm
// server-side apply path would produce for in. It does not mutate the
// cluster. Production [Cluster] implementations send server dry-run on
// every write.
//
// Exact prediction requires that the release namespace already exists
// for namespaced resources and that every resource group/version/kind
// is REST-mappable. Missing-namespace [apierrors.IsNotFound] and
// unmapped-kind [meta.IsNoMatchError] are returned as native typed
// errors, not rewritten to [ActionNoOp]. Those errors do not
// necessarily prove a real Deployah deploy would fail.
func Predict(ctx context.Context, cluster Cluster, in Input) ([]Result, error) {
	if err := validateInput(in); err != nil {
		return nil, err
	}
	if cluster == nil {
		return nil, errors.New("cluster is required")
	}

	previous, err := loadResources(cluster, in.Previous, in.Namespace)
	if err != nil {
		return nil, err
	}
	desired, err := loadResources(cluster, in.Desired, in.Namespace)
	if err != nil {
		return nil, err
	}
	for i := range desired {
		stampMetadata(desired[i].obj, in.ReleaseName, in.Namespace)
		desired[i].id = identityOf(desired[i].obj)
	}

	prevByObjectKey := make(map[string]resourceObj, len(previous))
	for _, p := range previous {
		prevByObjectKey[objectKey(p.id)] = p
	}

	results := make([]Result, 0, len(desired)+len(previous))
	for _, d := range desired {
		r, predErr := predictDesired(ctx, cluster, in, d, prevByObjectKey)
		if predErr != nil {
			return nil, predErr
		}
		results = append(results, r)
	}
	for _, p := range previous {
		if desiredHasResource(desired, p.id) {
			continue
		}
		r, pruneErr := predictPrune(ctx, cluster, p)
		if pruneErr != nil {
			return nil, pruneErr
		}
		results = append(results, r)
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results, nil
}

func validateInput(in Input) error {
	switch in.Operation {
	case helm.OperationInstall, helm.OperationUpgrade:
	default:
		return fmt.Errorf("invalid helm operation %d", in.Operation)
	}
	if in.ReleaseName == "" {
		return errors.New("release name is required")
	}
	if in.Namespace == "" {
		return errors.New("release namespace is required")
	}
	return nil
}

func desiredHasResource(desired []resourceObj, id Identity) bool {
	for _, d := range desired {
		if sameResource(d.id, id) {
			return true
		}
	}
	return false
}

func isNewToRelease(op helm.Operation, id Identity, previous map[string]resourceObj) bool {
	if op == helm.OperationInstall {
		return true
	}
	_, ok := previous[objectKey(id)]
	return !ok
}

func predictDesired(
	ctx context.Context,
	cluster Cluster,
	in Input,
	desired resourceObj,
	previous map[string]resourceObj,
) (Result, error) {
	generateName, err := generateNameState(desired.obj)
	if err != nil {
		return Result{}, err
	}
	if generateName {
		predicted, applyErr := applyCreate(ctx, cluster, desired.obj)
		if applyErr != nil {
			return Result{}, applyErr
		}
		return Result{
			Identity:  desired.id,
			Action:    ActionCreate,
			Predicted: predicted,
		}, nil
	}

	needOwnership := isNewToRelease(in.Operation, desired.id, previous)
	live, err := cluster.Get(ctx, desired.id)
	if err != nil && !apierrors.IsNotFound(err) {
		return Result{}, fmt.Errorf("could not get information about the resource %s: %w",
			resourceString(desired.obj), err)
	}
	if apierrors.IsNotFound(err) {
		live = nil
	}

	if needOwnership && live != nil {
		if ownErr := checkOwnership(live, in.ReleaseName, in.Namespace); ownErr != nil {
			return Result{}, ownershipConflict(live, ownErr)
		}
	}

	if live == nil {
		predicted, applyErr := applyCreate(ctx, cluster, desired.obj)
		if applyErr != nil {
			return Result{}, applyErr
		}
		return Result{
			Identity:  desired.id,
			Action:    ActionCreate,
			Predicted: predicted,
		}, nil
	}

	limitation := ""
	if in.Operation == helm.OperationInstall && needOwnership {
		var migErr error
		limitation, migErr = migrateManagedFields(ctx, cluster, desired.id, &live)
		if migErr != nil {
			return Result{}, migErr
		}
	}

	if limitation != "" {
		predicted, applyErr := applyUpdate(ctx, cluster, desired.obj)
		if applyErr != nil {
			return Result{
				Identity:   desired.id,
				Action:     ActionUpdate,
				Live:       live,
				Limitation: limitation,
			}, nil
		}
		return Result{
			Identity:   desired.id,
			Action:     ActionUpdate,
			Live:       live,
			Predicted:  predicted,
			Limitation: limitation,
		}, nil
	}

	predicted, applyErr := applyUpdate(ctx, cluster, desired.obj)
	if applyErr != nil {
		return Result{}, applyErr
	}
	if equalBookkeeping(live, predicted) {
		return Result{
			Identity:  desired.id,
			Action:    ActionNoOp,
			Live:      live,
			Predicted: predicted,
		}, nil
	}
	return Result{
		Identity:  desired.id,
		Action:    ActionUpdate,
		Live:      live,
		Predicted: predicted,
	}, nil
}
