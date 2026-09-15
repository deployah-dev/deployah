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

package predict_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"deployah.dev/deployah/internal/predict"
)

func TestEqualPredictedState(t *testing.T) {
	t.Parallel()
	live := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name":              "prod",
			"uid":               "live-uid",
			"resourceVersion":   "12",
			"generation":        int64(3),
			"creationTimestamp": "2020-01-01T00:00:00Z",
			"managedFields":     []any{map[string]any{"manager": "kube-apiserver"}},
			"labels":            map[string]any{"name": "prod"},
		},
		"status": map[string]any{"phase": "Active"},
	}}
	predicted := live.DeepCopy()
	predictedMeta := requireNestedMap(t, predicted.Object, "metadata")
	predictedMeta["uid"] = "pred-uid"
	predictedMeta["resourceVersion"] = "99"
	predictedMeta["generation"] = int64(9)
	predictedMeta["creationTimestamp"] = "2024-01-01T00:00:00Z"
	predictedMeta["managedFields"] = []any{map[string]any{"manager": "deployah"}}
	predicted.Object["status"] = map[string]any{"phase": "Terminating"}
	changed := predicted.DeepCopy()
	requireNestedMap(t, changed.Object, "metadata")["labels"] = map[string]any{"name": "other"}

	tests := []struct {
		name string
		a, b *unstructured.Unstructured
		want bool
	}{
		{name: "ignores bookkeeping", a: live, b: predicted, want: true},
		{name: "label change", a: live, b: changed, want: false},
		{name: "nil predicted", a: live, want: false},
		{name: "nil live", b: predicted, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, predict.EqualPredictedState(tt.a, tt.b))
		})
	}
}

func requireNestedMap(t *testing.T, obj map[string]any, key string) map[string]any {
	t.Helper()
	m, ok := obj[key].(map[string]any)
	if !ok {
		t.Fatalf("EqualPredictedState fixture missing %q map", key)
	}
	return m
}
