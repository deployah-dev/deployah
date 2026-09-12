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

package frompredict

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/predict"
)

// FromResults maps predictor results onto a semantic [semantic.Plan].
// It does not mutate results or the unstructured objects they hold.
func FromResults(header semantic.Header, results []predict.Result) (semantic.Plan, error) {
	origin := semantic.ResourceOrigin{
		Kind: semantic.OriginHelm,
		Helm: &semantic.HelmOrigin{
			Release:   header.Release,
			Namespace: header.Namespace,
		},
	}

	changes := make([]semantic.ResourceChange, 0, len(results))
	diags := make([]semantic.Diagnostic, 0)
	for i, r := range results {
		change, diag, err := mapResult(origin, r)
		if err != nil {
			return semantic.Plan{}, fmt.Errorf("result %d: %w", i, err)
		}
		if diag != nil {
			diags = append(diags, *diag)
		}
		if change != nil {
			changes = append(changes, *change)
		}
	}
	return semantic.New(header, changes, diags)
}

func mapResult(origin semantic.ResourceOrigin, r predict.Result) (*semantic.ResourceChange, *semantic.Diagnostic, error) {
	switch r.Action {
	case predict.ActionNoOp:
		if r.Limitation == "" {
			return nil, nil, nil
		}
		ref := resourceRef(r.Identity, firstObject(r.Live, r.Predicted))
		d := limitationDiagnostic(ref, r.Limitation)
		return nil, &d, nil
	case predict.ActionCreate:
		if r.Predicted == nil {
			return nil, nil, fmt.Errorf("create requires a predicted object")
		}
		ref := resourceRef(r.Identity, r.Predicted)
		change := semantic.ResourceChange{
			Resource: ref,
			Origin:   origin,
			Action:   semantic.Create,
			After:    snapshotOf(r.Predicted),
			Apply:    writeApply(),
		}
		return &change, limitationIfSet(ref, r.Limitation), nil
	case predict.ActionUpdate:
		if r.Live == nil {
			return nil, nil, fmt.Errorf("update requires a live object")
		}
		ref := resourceRef(r.Identity, firstObject(r.Live, r.Predicted))
		change := semantic.ResourceChange{
			Resource: ref,
			Origin:   origin,
			Action:   semantic.Update,
			Before:   snapshotOf(r.Live),
			Apply:    writeApply(),
		}
		if r.Predicted != nil {
			change.After = snapshotOf(r.Predicted)
		}
		return &change, limitationIfSet(ref, r.Limitation), nil
	case predict.ActionDelete:
		if r.Live == nil {
			return nil, nil, fmt.Errorf("delete requires a live object")
		}
		ref := resourceRef(r.Identity, r.Live)
		change := semantic.ResourceChange{
			Resource: ref,
			Origin:   origin,
			Action:   semantic.Delete,
			Before:   snapshotOf(r.Live),
			Apply:    deleteApply(),
		}
		return &change, limitationIfSet(ref, r.Limitation), nil
	default:
		return nil, nil, fmt.Errorf("invalid predict action %s", r.Action)
	}
}

func limitationIfSet(ref semantic.ResourceRef, limitation string) *semantic.Diagnostic {
	if limitation == "" {
		return nil
	}
	d := limitationDiagnostic(ref, limitation)
	return &d
}

func limitationDiagnostic(ref semantic.ResourceRef, limitation string) semantic.Diagnostic {
	return semantic.Diagnostic{
		Severity: semantic.DiagnosticWarning,
		Category: semantic.CategoryPredictionLimitation,
		Message:  fmt.Sprintf("prediction is not exact: %s", limitation),
		Resource: &ref,
	}
}

func resourceRef(id predict.Identity, obj *unstructured.Unstructured) semantic.ResourceRef {
	gv := schema.GroupVersion{Group: id.Group, Version: id.Version}
	ref := semantic.ResourceRef{
		APIVersion: gv.String(),
		Kind:       id.Kind,
		Namespace:  id.Namespace,
		Name:       id.Name,
	}
	if ref.Name == "" && obj != nil {
		ref.GenerateName = obj.GetGenerateName()
	}
	return ref
}

func firstObject(a, b *unstructured.Unstructured) *unstructured.Unstructured {
	if a != nil {
		return a
	}
	return b
}

func snapshotOf(obj *unstructured.Unstructured) *semantic.ResourceSnapshot {
	if obj == nil {
		return nil
	}
	if obj.Object == nil {
		return &semantic.ResourceSnapshot{}
	}
	// [semantic.New] copies the map. Passing Object here avoids
	// runtime.DeepCopyJSON, which panics on ordinary Go int values.
	return &semantic.ResourceSnapshot{Object: obj.Object}
}

func writeApply() semantic.ApplySemantics {
	return semantic.ApplySemantics{
		Write: &semantic.WriteSemantics{
			Method:         semantic.WriteServerSide,
			FieldManager:   "deployah",
			ForceConflicts: false,
		},
	}
}

func deleteApply() semantic.ApplySemantics {
	return semantic.ApplySemantics{
		Delete: &semantic.DeleteSemantics{
			Propagation: semantic.PropagationBackground,
		},
	}
}
