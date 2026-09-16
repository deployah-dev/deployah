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
			p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, tt.changes, nil, nil)
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
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, []semantic.ResourceChange{
		{
			Resource: res,
			Origin:   helmOrigin(),
			Action:   semantic.Delete,
			Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
			Apply: semantic.ApplySemantics{
				Delete: &semantic.DeleteSemantics{Propagation: semantic.PropagationBackground},
			},
		},
		{
			Resource: res,
			Origin:   helmOrigin(),
			Action:   semantic.Replace,
			Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
			After:    &semantic.ResourceSnapshot{Object: cm("app", "v2")},
			Apply:    bothApply(),
		},
		{
			Resource: res,
			Origin:   helmOrigin(),
			Action:   semantic.Update,
			Before:   &semantic.ResourceSnapshot{Object: cm("app", "v1")},
			After:    &semantic.ResourceSnapshot{Object: cm("app", "v2")},
			Apply:    writeApply(),
		},
		{
			Resource: res,
			Origin:   helmOrigin(),
			Action:   semantic.Create,
			After:    &semantic.ResourceSnapshot{Object: cm("app", "v1")},
			Apply:    writeApply(),
		},
	}, nil, nil)
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
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, []semantic.ResourceChange{{
		Resource: ref("ConfigMap", "app"),
		Origin:   helmOrigin(),
		Action:   semantic.Update,
		Before:   &semantic.ResourceSnapshot{Object: map[string]any{"z": "1", "a": "1", "m": "1"}},
		After:    &semantic.ResourceSnapshot{Object: map[string]any{"z": "2", "a": "2", "m": "2"}},
		Apply:    writeApply(),
	}}, nil, nil)
	require.NoError(t, err)
	require.Len(t, p.Changes[0].Fields, 3)
	assert.Equal(t, []string{"/a", "/m", "/z"}, []string{
		p.Changes[0].Fields[0].Path,
		p.Changes[0].Fields[1].Path,
		p.Changes[0].Fields[2].Path,
	})
}

func TestNew_DriftOrder(t *testing.T) {
	t.Parallel()
	app := ref("ConfigMap", "app")
	web := ref("ConfigMap", "web")
	p, err := semantic.New(semantic.Header{}, semantic.HelmUpgrade, nil, nil, []semantic.ResourceDrift{
		{Resource: web, Kind: semantic.DriftMissing},
		{Resource: app, Kind: semantic.DriftMissing},
		{
			Resource: app,
			Kind:     semantic.DriftModified,
			Fields: []semantic.FieldChange{{
				Path:   "/data/key",
				Op:     semantic.FieldReplace,
				Before: "1",
				After:  "2",
			}},
		},
	})
	require.NoError(t, err)
	require.Len(t, p.Drift, 3)
	assert.Equal(t, "app", p.Drift[0].Resource.Name)
	assert.Equal(t, semantic.DriftModified, p.Drift[0].Kind)
	assert.Equal(t, "app", p.Drift[1].Resource.Name)
	assert.Equal(t, semantic.DriftMissing, p.Drift[1].Kind)
	assert.Equal(t, "web", p.Drift[2].Resource.Name)
}

func TestNew_OriginRankBeforeApplyOrder(t *testing.T) {
	t.Parallel()
	helm := createChange("app", "v1")
	helm.ApplyOrder = 1
	ns := nsCreate("prod")
	ns.ApplyOrder = 1
	crd := crdCreate("widgets.example.com")
	crd.ApplyOrder = 9
	p, err := semantic.New(semantic.Header{FreshInstall: true}, semantic.HelmInstall, []semantic.ResourceChange{helm, ns, crd}, nil, nil)
	require.NoError(t, err)
	require.Len(t, p.Changes, 3)
	assert.Equal(t, semantic.OriginCRD, p.Changes[0].Origin.Kind)
	assert.Equal(t, semantic.OriginNamespace, p.Changes[1].Origin.Kind)
	assert.Equal(t, semantic.OriginHelm, p.Changes[2].Origin.Kind)
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
		After:    &semantic.ResourceSnapshot{Object: cm(name, "v1")},
		Apply:    writeApply(),
	}
}
