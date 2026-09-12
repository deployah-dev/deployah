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
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/plan/semantic"
)

func TestNew_OrderTieBreakers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		changes []semantic.ResourceChange
		want    []semantic.ResourceRef
	}{
		{
			name: "apiVersion",
			changes: []semantic.ResourceChange{
				namedCreate(semantic.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "app"}),
				namedCreate(semantic.ResourceRef{APIVersion: "apps/v1", Kind: "ConfigMap", Namespace: "prod", Name: "app"}),
			},
			want: []semantic.ResourceRef{
				{APIVersion: "apps/v1", Kind: "ConfigMap", Namespace: "prod", Name: "app"},
				{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "app"},
			},
		},
		{
			name: "kind",
			changes: []semantic.ResourceChange{
				namedCreate(semantic.ResourceRef{APIVersion: "v1", Kind: "Secret", Namespace: "prod", Name: "app"}),
				namedCreate(semantic.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "app"}),
			},
			want: []semantic.ResourceRef{
				{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "app"},
				{APIVersion: "v1", Kind: "Secret", Namespace: "prod", Name: "app"},
			},
		},
		{
			name: "namespace",
			changes: []semantic.ResourceChange{
				namedCreate(semantic.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "app"}),
				namedCreate(semantic.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: "dev", Name: "app"}),
			},
			want: []semantic.ResourceRef{
				{APIVersion: "v1", Kind: "ConfigMap", Namespace: "dev", Name: "app"},
				{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "app"},
			},
		},
		{
			name: "name",
			changes: []semantic.ResourceChange{
				namedCreate(semantic.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "z"}),
				namedCreate(semantic.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "a"}),
			},
			want: []semantic.ResourceRef{
				{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "a"},
				{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", Name: "z"},
			},
		},
		{
			name: "generateName",
			changes: []semantic.ResourceChange{
				namedCreate(semantic.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", GenerateName: "z-"}),
				namedCreate(semantic.ResourceRef{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", GenerateName: "a-"}),
			},
			want: []semantic.ResourceRef{
				{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", GenerateName: "a-"},
				{APIVersion: "v1", Kind: "ConfigMap", Namespace: "prod", GenerateName: "z-"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := semantic.New(semantic.Header{}, tt.changes, nil)
			require.NoError(t, err)
			require.Len(t, p.Changes, len(tt.want))
			got := make([]semantic.ResourceRef, 0, len(p.Changes))
			for _, c := range p.Changes {
				got = append(got, c.Resource)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNew_ActionRank(t *testing.T) {
	t.Parallel()
	res := ref("ConfigMap", "app")
	p, err := semantic.New(semantic.Header{}, []semantic.ResourceChange{
		{
			Resource: res,
			Origin:   helmOrigin(),
			Action:   semantic.Delete,
			Before:   snap(cm("app", "v1")),
			Apply:    deleteApply(),
		},
		{
			Resource: res,
			Origin:   helmOrigin(),
			Action:   semantic.Replace,
			Before:   snap(cm("app", "v1")),
			After:    snap(cm("app", "v2")),
			Apply:    bothApply(),
		},
		{
			Resource: res,
			Origin:   helmOrigin(),
			Action:   semantic.Update,
			Before:   snap(cm("app", "v1")),
			After:    snap(cm("app", "v2")),
			Apply:    writeApply(),
		},
		{
			Resource: res,
			Origin:   helmOrigin(),
			Action:   semantic.Create,
			After:    snap(cm("app", "v1")),
			Apply:    writeApply(),
		},
	}, nil)
	require.NoError(t, err)
	require.Len(t, p.Changes, 4)
	assert.Equal(t, []semantic.Action{
		semantic.Create, semantic.Update, semantic.Replace, semantic.Delete,
	}, []semantic.Action{
		p.Changes[0].Action, p.Changes[1].Action, p.Changes[2].Action, p.Changes[3].Action,
	})
}

func TestNew_FieldChangeOrder(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Header{}, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   snap(map[string]any{"z": "1", "a": "1", "m": "1"}),
		After:    snap(map[string]any{"z": "2", "a": "2", "m": "2"}),
		Apply:    writeApply(),
	}}, nil)
	require.NoError(t, err)
	require.Len(t, p.Changes[0].Fields, 3)
	assert.Equal(t, []string{"/a", "/m", "/z"}, []string{
		p.Changes[0].Fields[0].Path,
		p.Changes[0].Fields[1].Path,
		p.Changes[0].Fields[2].Path,
	})
}

func TestNew_DiagnosticOrder(t *testing.T) {
	t.Parallel()
	app := ref("ConfigMap", "app")
	web := ref("ConfigMap", "web")
	p, err := semantic.New(semantic.Header{}, nil, []semantic.Diagnostic{
		{
			Severity: semantic.DiagnosticWarning,
			Category: semantic.CategoryPredictionLimitation,
			Message:  "z last",
			Resource: &web,
		},
		{
			Severity: semantic.DiagnosticWarning,
			Category: semantic.CategoryPredictionLimitation,
			Message:  "nil resource",
		},
		{
			Severity: semantic.DiagnosticWarning,
			Category: semantic.CategoryPredictionLimitation,
			Message:  "b second",
			Resource: &app,
		},
		{
			Severity: semantic.DiagnosticWarning,
			Category: semantic.CategoryPredictionLimitation,
			Message:  "a first",
			Resource: &app,
		},
	})
	require.NoError(t, err)
	require.Len(t, p.Diagnostics, 4)
	assert.Nil(t, p.Diagnostics[0].Resource)
	assert.Equal(t, "nil resource", p.Diagnostics[0].Message)
	assert.Equal(t, "app", p.Diagnostics[1].Resource.Name)
	assert.Equal(t, "a first", p.Diagnostics[1].Message)
	assert.Equal(t, "app", p.Diagnostics[2].Resource.Name)
	assert.Equal(t, "b second", p.Diagnostics[2].Message)
	assert.Equal(t, "web", p.Diagnostics[3].Resource.Name)
}

func namedCreate(res semantic.ResourceRef) semantic.ResourceChange {
	name := res.Name
	if name == "" {
		name = "generated"
	}
	return semantic.ResourceChange{
		Resource: res,
		Origin:   helmOrigin(),
		Action:   semantic.Create,
		After:    snap(cm(name, "v1")),
		Apply:    writeApply(),
	}
}
