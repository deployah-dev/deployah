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
			p, err := semantic.New(semantic.Input{Header: semantic.Header{}, HelmAction: semantic.HelmUpgrade, Changes: tt.changes, Tasks: nil})
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
	p, err := semantic.New(semantic.Input{Header: semantic.Header{}, HelmAction: semantic.HelmUpgrade, Changes: []semantic.ResourceChange{
		{
			Resource: res,
			Action:   semantic.Delete,
			Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
		},
		{
			Resource: res,
			Action:   semantic.Update,
			Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
			After:    &semantic.ResourceSnapshot{Object: cm("app", "v2")},
			Fields:   mustFields(cm("app", "v1"), cm("app", "v2")),
		},
		{
			Resource: res,
			Action:   semantic.Create,
			After:    &semantic.ResourceSnapshot{Object: cm("app", "v1")},
		},
	}, Tasks: nil})
	require.NoError(t, err)
	require.Len(t, p.Changes, 3)
	assert.Equal(t, []semantic.Action{
		semantic.Create, semantic.Update, semantic.Delete,
	}, []semantic.Action{
		p.Changes[0].Action, p.Changes[1].Action, p.Changes[2].Action,
	})
}

func TestNew_FieldChangeOrder(t *testing.T) {
	t.Parallel()
	p, err := semantic.New(semantic.Input{Header: semantic.Header{}, HelmAction: semantic.HelmUpgrade, Changes: []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Action:   semantic.Update,
		Before:   &semantic.ResourceSnapshot{Object: map[string]any{"z": "1", "a": "1", "m": "1"}},
		After:    &semantic.ResourceSnapshot{Object: map[string]any{"z": "2", "a": "2", "m": "2"}},
		Fields: []semantic.FieldChange{
			{Path: "/z", Op: semantic.FieldReplace, Before: "1", After: "2"},
			{Path: "/a", Op: semantic.FieldReplace, Before: "1", After: "2"},
			{Path: "/m", Op: semantic.FieldReplace, Before: "1", After: "2"},
		},
	}}, Tasks: nil})
	require.NoError(t, err)
	require.Len(t, p.Changes[0].Fields, 3)
	assert.Equal(t, []string{"/a", "/m", "/z"}, []string{
		p.Changes[0].Fields[0].Path,
		p.Changes[0].Fields[1].Path,
		p.Changes[0].Fields[2].Path,
	})
}

func TestNew_HelmOrderThenRef(t *testing.T) {
	t.Parallel()
	later := createChange("z", "v1")
	later.HelmOrder = 2
	tiedHigh := createChange("m", "v1")
	tiedHigh.HelmOrder = 1
	tiedLow := createChange("a", "v1")
	tiedLow.HelmOrder = 1
	p, err := semantic.New(semantic.Input{Header: semantic.Header{}, HelmAction: semantic.HelmUpgrade, Changes: []semantic.ResourceChange{later, tiedHigh, tiedLow}, Tasks: nil})
	require.NoError(t, err)
	require.Len(t, p.Changes, 3)
	assert.Equal(t, "a", p.Changes[0].Resource.Name)
	assert.Equal(t, "m", p.Changes[1].Resource.Name)
	assert.Equal(t, "z", p.Changes[2].Resource.Name)
}

func namedCreate(res semantic.ResourceRef) semantic.ResourceChange {
	name := res.Name
	if name == "" {
		name = "generated"
	}
	return semantic.ResourceChange{
		Resource: res,
		Action:   semantic.Create,
		After:    &semantic.ResourceSnapshot{Object: cm(name, "v1")},
	}
}
