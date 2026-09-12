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

package semantic_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"deployah.dev/deployah/internal/plan/semantic"
)

func TestEnumString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "create", got: semantic.Create.String(), want: "create"},
		{name: "update", got: semantic.Update.String(), want: "update"},
		{name: "delete", got: semantic.Delete.String(), want: "delete"},
		{name: "action replace", got: semantic.Replace.String(), want: "replace"},
		{name: "unknown action", got: semantic.Action(0).String(), want: "Action(0)"},
		{name: "complete", got: semantic.CompletenessComplete.String(), want: "complete"},
		{name: "partial", got: semantic.CompletenessPartial.String(), want: "partial"},
		{name: "unknown completeness", got: semantic.Completeness(0).String(), want: "Completeness(0)"},
		{name: "helm", got: semantic.OriginHelm.String(), want: "helm"},
		{name: "unknown origin", got: semantic.OriginKind(0).String(), want: "OriginKind(0)"},
		{name: "server_side_apply", got: semantic.WriteServerSide.String(), want: "server_side_apply"},
		{name: "unknown write method", got: semantic.WriteMethod(0).String(), want: "WriteMethod(0)"},
		{name: "background", got: semantic.PropagationBackground.String(), want: "background"},
		{name: "unknown delete propagation", got: semantic.DeletePropagation(0).String(), want: "DeletePropagation(0)"},
		{name: "field add", got: semantic.FieldAdd.String(), want: "add"},
		{name: "field remove", got: semantic.FieldRemove.String(), want: "remove"},
		{name: "field replace", got: semantic.FieldReplace.String(), want: "replace"},
		{name: "unknown field op", got: semantic.FieldOp(0).String(), want: "FieldOp(0)"},
		{name: "warning", got: semantic.DiagnosticWarning.String(), want: "warning"},
		{name: "unknown severity", got: semantic.DiagnosticSeverity(0).String(), want: "DiagnosticSeverity(0)"},
		{name: "prediction_limitation", got: semantic.CategoryPredictionLimitation.String(), want: "prediction_limitation"},
		{name: "unknown category", got: semantic.DiagnosticCategory(0).String(), want: "DiagnosticCategory(0)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.got)
		})
	}
}

func TestResourceRefString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ref  semantic.ResourceRef
		want string
	}{
		{
			name: "named",
			ref:  semantic.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "app"},
			want: "ConfigMap/prod/app",
		},
		{
			name: "generateName",
			ref:  semantic.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", GenerateName: "app-"},
			want: "ConfigMap/prod/generateName=app-",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.ref.String())
		})
	}
}
