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
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"deployah.dev/deployah/internal/helm"
)

// LimitationManagedFieldsMigration marks a [Result] whose Predicted object
// is not an exact Helm write: install adoption needed a managed-fields
// JSON patch that server dry-run accepted, but that patch was not
// persisted before the SSA dry-run.
const LimitationManagedFieldsMigration = "managed-fields-migration"

// Action is the semantic Kubernetes operation [Predict] would perform for
// one resource. The zero value is invalid.
type Action int

const (
	// ActionCreate is a Desired resource with no live object.
	ActionCreate Action = iota + 1
	// ActionUpdate is a Desired write that would change live state, or an
	// install adoption whose prediction is limited.
	ActionUpdate
	// ActionDelete is a Previous resource that Helm would prune.
	ActionDelete
	// ActionNoOp is no observable change: already gone, keep policy, or
	// normalized Live equals Predicted.
	ActionNoOp
)

func (a Action) String() string {
	switch a {
	case ActionCreate:
		return "Create"
	case ActionUpdate:
		return "Update"
	case ActionDelete:
		return "Delete"
	case ActionNoOp:
		return "NoOp"
	default:
		return fmt.Sprintf("Action(%d)", int(a))
	}
}

// Identity is one Kubernetes object's Helm-facing identity. Group, Kind,
// Namespace, and Name are used for prune matching. Version is used for
// new-to-release matching and API requests.
type Identity struct {
	Group     string
	Version   string
	Kind      string
	Namespace string
	Name      string
}

// Input is one Helm-resource prediction request. SSA is an invariant, not
// a field. Previous must be [helm.ReleasePrep.Current] Manifest on
// upgrade and empty on install.
type Input struct {
	Operation   helm.Operation
	ReleaseName string
	Namespace   string
	Previous    string
	Desired     string
}

// Result is the prediction for one flattened resource.
//
// Predicted is nil when the object would be gone after delete or was
// already absent, or when Limitation is set and an exact predicted
// state is unavailable. Keep-policy NoOp sets Predicted to a DeepCopy
// of Live. Action plus Limitation disambiguates a nil Predicted.
type Result struct {
	Identity   Identity
	Action     Action
	Live       *unstructured.Unstructured
	Predicted  *unstructured.Unstructured
	Limitation string
}

// InputFromPrep builds an [Input] from Stage A release prep. It fails
// closed: nil prep, an unknown or zero Operation, upgrade with a nil
// Current, and install with a non-nil Current are errors.
func InputFromPrep(prep *helm.ReleasePrep, name, namespace, desired string) (Input, error) {
	if prep == nil {
		return Input{}, errors.New("release prep is required")
	}
	switch prep.Operation {
	case helm.OperationInstall:
		if prep.Current != nil {
			return Input{}, errors.New("install prep must not have a current release")
		}
		return Input{
			Operation:   helm.OperationInstall,
			ReleaseName: name,
			Namespace:   namespace,
			Desired:     desired,
		}, nil
	case helm.OperationUpgrade:
		if prep.Current == nil {
			return Input{}, errors.New("upgrade prep requires a current release")
		}
		return Input{
			Operation:   helm.OperationUpgrade,
			ReleaseName: name,
			Namespace:   namespace,
			Previous:    prep.Current.Manifest,
			Desired:     desired,
		}, nil
	default:
		return Input{}, fmt.Errorf("invalid helm operation %d", prep.Operation)
	}
}
