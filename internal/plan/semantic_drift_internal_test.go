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
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestProjectValue_DeclaredSurface(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		declared map[string]any
		live     map[string]any
		want     map[string]any
	}{
		{
			name: "declared keys and extra list items",
			declared: map[string]any{
				"data":  map[string]any{"key": "v"},
				"items": []any{map[string]any{"name": "a"}},
				"count": 1,
			},
			live: map[string]any{
				"data": map[string]any{"key": "v", "extra": "nope"},
				"items": []any{
					map[string]any{"name": "a", "extra": "nope"},
					map[string]any{"name": "b"},
				},
				"count":  "nope",
				"status": map[string]any{"ok": true},
			},
			want: map[string]any{
				"data": map[string]any{"key": "v"},
				"items": []any{
					map[string]any{"name": "a"},
					map[string]any{"name": "b"},
				},
				"count": "nope",
			},
		},
		{
			name:     "empty map declares no keys",
			declared: map[string]any{"limits": map[string]any{}},
			live:     map[string]any{"limits": map[string]any{"cpu": "1"}},
			want:     map[string]any{"limits": map[string]any{}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := projectValue(tt.declared, tt.live).(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFoldSecretStringData(t *testing.T) {
	t.Parallel()
	obj := map[string]any{
		"data":       map[string]any{"password": "cHJldg==", "kept": "eQ=="},
		"stringData": map[string]any{"password": "new", "token": "abc", "weird": 1},
	}
	stored := map[string]any{"stringData": map[string]any{"password": "new"}}
	foldSecretStringData(obj, "v1", "Secret")
	data, ok := obj["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("new")), data["password"])
	assert.Equal(t, "eQ==", data["kept"])
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("abc")), data["token"])
	assert.Equal(t, map[string]any{"weird": 1}, obj["stringData"])
	assert.Equal(t, map[string]any{"password": "new"}, stored["stringData"])

	other := map[string]any{"stringData": map[string]any{"password": "new"}}
	foldSecretStringData(other, "v1", "ConfigMap")
	_, hasData := other["data"]
	assert.False(t, hasData)
	assert.Contains(t, other, "stringData")

	core := map[string]any{"stringData": map[string]any{"password": "new"}}
	foldSecretStringData(core, "core/v1", "Secret")
	coreData, ok := core["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("new")), coreData["password"])
	_, still := core["stringData"]
	assert.False(t, still)
}

func TestApplyAbsenceEquivalence_PathAware(t *testing.T) {
	t.Parallel()
	prev := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name": "app",
							"resources": map[string]any{
								"limits":   map[string]any{},
								"requests": map[string]any{"cpu": "100m"},
							},
						},
						map[string]any{
							"name": "side",
							"resources": map[string]any{
								"limits": map[string]any{"cpu": "1"},
							},
						},
					},
				},
			},
		},
	}
	live := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name": "app",
							"resources": map[string]any{
								"requests": map[string]any{"cpu": "100m"},
							},
						},
						map[string]any{
							"name": "side",
							"resources": map[string]any{
								"limits": map[string]any{"cpu": "1"},
							},
						},
					},
				},
			},
		},
	}
	applyAbsenceEquivalence(prev, live, schema.GroupKind{Group: "apps", Kind: "Deployment"})
	spec, ok := prev["spec"].(map[string]any)
	require.True(t, ok)
	template, ok := spec["template"].(map[string]any)
	require.True(t, ok)
	podSpec, ok := template["spec"].(map[string]any)
	require.True(t, ok)
	containers, ok := podSpec["containers"].([]any)
	require.True(t, ok)
	app, ok := containers[0].(map[string]any)
	require.True(t, ok)
	resources, ok := app["resources"].(map[string]any)
	require.True(t, ok)
	_, hasLimits := resources["limits"]
	assert.False(t, hasLimits)
	assert.Equal(t, map[string]any{"cpu": "100m"}, resources["requests"])
	sideContainer, ok := containers[1].(map[string]any)
	require.True(t, ok)
	side, ok := sideContainer["resources"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"cpu": "1"}, side["limits"])

	widget := map[string]any{"spec": map[string]any{"limits": map[string]any{}}}
	applyAbsenceEquivalence(widget, map[string]any{}, schema.GroupKind{Group: "example.com", Kind: "Widget"})
	widgetSpec, ok := widget["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{}, widgetSpec["limits"])

	cron := map[string]any{
		"spec": map[string]any{
			"jobTemplate": map[string]any{
				"spec": map[string]any{
					"template": map[string]any{
						"spec": map[string]any{
							"containers": []any{
								map[string]any{"resources": map[string]any{"limits": map[string]any{}}},
							},
						},
					},
				},
			},
		},
	}
	applyAbsenceEquivalence(cron, map[string]any{}, schema.GroupKind{Group: "batch", Kind: "CronJob"})
	cronSpec, ok := cron["spec"].(map[string]any)
	require.True(t, ok)
	jobTemplate, ok := cronSpec["jobTemplate"].(map[string]any)
	require.True(t, ok)
	jobSpec, ok := jobTemplate["spec"].(map[string]any)
	require.True(t, ok)
	podTemplate, ok := jobSpec["template"].(map[string]any)
	require.True(t, ok)
	cronPod, ok := podTemplate["spec"].(map[string]any)
	require.True(t, ok)
	cronContainers, ok := cronPod["containers"].([]any)
	require.True(t, ok)
	cronContainer, ok := cronContainers[0].(map[string]any)
	require.True(t, ok)
	cronResources, ok := cronContainer["resources"].(map[string]any)
	require.True(t, ok)
	_, cronLimits := cronResources["limits"]
	assert.False(t, cronLimits)

	paths := expandPattern(map[string]any{
		"spec": map[string]any{"containers": []any{
			map[string]any{"name": "a"},
			map[string]any{"name": "b"},
		}},
	}, []string{"spec", "containers", "*", "name"})
	assert.Equal(t, [][]string{
		{"spec", "containers", "0", "name"},
		{"spec", "containers", "1", "name"},
	}, paths)
	assert.Empty(t, expandPattern(map[string]any{"spec": "nope"}, []string{"spec", "*"}))
}
