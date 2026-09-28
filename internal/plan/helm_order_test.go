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

package plan

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/plan/semantic"
)

func TestStampHelmOrder(t *testing.T) {
	t.Parallel()
	kinds := []string{"Service", "ConfigMap", "Deployment", "Ingress", "Secret", "ServiceAccount"}
	install := make([]semantic.ResourceChange, 0, len(kinds))
	for _, kind := range kinds {
		api := "v1"
		switch kind {
		case "Deployment":
			api = "apps/v1"
		case "Ingress":
			api = "networking.k8s.io/v1"
		}
		install = append(install, orderChange(api, kind, "app", semantic.Create))
	}
	tests := []struct {
		name    string
		changes []semantic.ResourceChange
		want    []string
	}{
		{
			name:    "install order",
			changes: install,
			want: []string{
				"ServiceAccount/app/create",
				"Secret/app/create",
				"ConfigMap/app/create",
				"Service/app/create",
				"Deployment/app/create",
				"Ingress/app/create",
			},
		},
		{
			name: "deletes follow targets",
			changes: []semantic.ResourceChange{
				orderChange("v1", "ServiceAccount", "gone", semantic.Delete),
				orderChange("networking.k8s.io/v1", "Ingress", "app", semantic.Create),
				orderChange("v1", "ConfigMap", "app", semantic.Update),
			},
			want: []string{
				"ConfigMap/app/update",
				"Ingress/app/create",
				"ServiceAccount/gone/delete",
			},
		},
		{
			name: "deletes keep previous order",
			changes: []semantic.ResourceChange{
				orderChange("apps/v1", "Deployment", "api", semantic.Delete),
				orderChange("v1", "Service", "api", semantic.Delete),
				orderChange("v1", "ConfigMap", "api", semantic.Delete),
				orderChange("v1", "ServiceAccount", "api", semantic.Delete),
			},
			want: []string{
				"Deployment/api/delete",
				"Service/api/delete",
				"ConfigMap/api/delete",
				"ServiceAccount/api/delete",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			changes := append([]semantic.ResourceChange(nil), tt.changes...)
			require.NoError(t, stampHelmOrder(changes))
			assert.Equal(t, tt.want, orderByRank(changes))
		})
	}
}

func TestStampHelmOrder_SameKindKeepsInputOrder(t *testing.T) {
	t.Parallel()
	const n = 4
	changes := make([]semantic.ResourceChange, 0, n)
	for i := range n {
		name := fmt.Sprintf("n%d", n-1-i)
		changes = append(changes, orderChange("v1", "ConfigMap", name, semantic.Create))
	}
	require.NoError(t, stampHelmOrder(changes))
	for i, c := range changes {
		assert.Equal(t, i+1, c.HelmOrder)
		assert.Equal(t, fmt.Sprintf("n%d", n-1-i), c.Resource.Name)
	}
}

func orderChange(api, kind, name string, action semantic.Action) semantic.ResourceChange {
	obj := map[string]any{
		"apiVersion": api,
		"kind":       kind,
		"metadata":   map[string]any{"name": name, "namespace": "prod"},
	}
	change := semantic.ResourceChange{
		Resource: semantic.ResourceRef{APIVersion: api, Kind: kind, Namespace: "prod", Name: name},
		Action:   action,
	}
	snap := &semantic.ResourceSnapshot{Object: obj}
	if action == semantic.Delete {
		change.Before = snap
		return change
	}
	change.After = snap
	if action == semantic.Update {
		change.Before = &semantic.ResourceSnapshot{Object: map[string]any{
			"apiVersion": api,
			"kind":       kind,
			"metadata":   map[string]any{"name": name, "namespace": "prod"},
			"data":       map[string]any{"key": "old"},
		}}
		change.Fields = []semantic.FieldChange{{Path: "/data/key", Op: semantic.FieldReplace, Before: "old", After: "new"}}
	}
	return change
}

func orderByRank(changes []semantic.ResourceChange) []string {
	ranked := append([]semantic.ResourceChange(nil), changes...)
	for i := range ranked {
		for j := i + 1; j < len(ranked); j++ {
			if ranked[j].HelmOrder < ranked[i].HelmOrder {
				ranked[i], ranked[j] = ranked[j], ranked[i]
			}
		}
	}
	out := make([]string, 0, len(ranked))
	for _, c := range ranked {
		out = append(out, c.Resource.Kind+"/"+c.Resource.Name+"/"+c.Action.String())
	}
	return out
}
