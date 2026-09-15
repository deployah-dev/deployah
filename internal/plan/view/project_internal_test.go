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

package view

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"deployah.dev/deployah/internal/plan/semantic"
)

func TestProjectFields_KeepsAncestorsAndNameSibling(t *testing.T) {
	t.Parallel()
	before := map[string]any{
		"spec": map[string]any{
			"containers": []any{
				map[string]any{
					"name":  "api",
					"image": "old",
					"ports": []any{map[string]any{"containerPort": 8080}},
				},
				map[string]any{
					"name":  "sidecar",
					"image": "side",
				},
			},
		},
		"keep": "yes",
	}
	after := map[string]any{
		"spec": map[string]any{
			"containers": []any{
				map[string]any{
					"name":  "api",
					"image": "new",
					"ports": []any{map[string]any{"containerPort": 8080}},
				},
				map[string]any{
					"name":  "sidecar",
					"image": "side",
				},
			},
		},
		"keep": "yes",
	}
	gotBefore, gotAfter := projectFields(before, after, []semantic.FieldChange{{
		Path:   "/spec/containers/0/image",
		Op:     semantic.FieldReplace,
		Before: "old",
		After:  "new",
	}})
	wantContainer := func(image string) map[string]any {
		return map[string]any{
			"spec": map[string]any{
				"containers": []any{
					map[string]any{"name": "api", "image": image},
				},
			},
		}
	}
	assert.Equal(t, wantContainer("old"), gotBefore)
	assert.Equal(t, wantContainer("new"), gotAfter)
}

func TestProjectFields_AddAfterOnlyRemoveBeforeOnly(t *testing.T) {
	t.Parallel()
	obj := map[string]any{"data": map[string]any{"keep": "yes"}}
	gotBefore, gotAfter := projectFields(obj, map[string]any{
		"data": map[string]any{"keep": "yes", "extra": "x"},
	}, []semantic.FieldChange{{
		Path:  "/data/extra",
		Op:    semantic.FieldAdd,
		After: "x",
	}})
	assert.Empty(t, gotBefore)
	assert.Equal(t, map[string]any{"data": map[string]any{"extra": "x"}}, gotAfter)

	gotBefore, gotAfter = projectFields(map[string]any{
		"data": map[string]any{"keep": "yes", "gone": "y"},
	}, obj, []semantic.FieldChange{{
		Path:   "/data/gone",
		Op:     semantic.FieldRemove,
		Before: "y",
	}})
	assert.Equal(t, map[string]any{"data": map[string]any{"gone": "y"}}, gotBefore)
	assert.Empty(t, gotAfter)
}

func TestProjectFields_WholeNamedListElement(t *testing.T) {
	t.Parallel()
	added := map[string]any{
		"name":    "sidecar",
		"image":   "busybox:1.36",
		"command": []any{"sleep", "3600"},
		"env":     []any{map[string]any{"name": "MODE", "value": "side"}},
	}
	one := map[string]any{
		"spec": map[string]any{
			"containers": []any{
				map[string]any{"name": "api", "image": "app:1"},
			},
		},
	}
	two := map[string]any{
		"spec": map[string]any{
			"containers": []any{
				map[string]any{"name": "api", "image": "app:1"},
				added,
			},
		},
	}
	projected := map[string]any{
		"spec": map[string]any{
			"containers": []any{added},
		},
	}
	tests := []struct {
		name       string
		before     map[string]any
		after      map[string]any
		field      semantic.FieldChange
		wantBefore map[string]any
		wantAfter  map[string]any
	}{
		{
			name:   "add complete named item",
			before: one,
			after:  two,
			field: semantic.FieldChange{
				Path:  "/spec/containers/1",
				Op:    semantic.FieldAdd,
				After: added,
			},
			wantBefore: map[string]any{},
			wantAfter:  projected,
		},
		{
			name:   "remove complete named item",
			before: two,
			after:  one,
			field: semantic.FieldChange{
				Path:   "/spec/containers/1",
				Op:     semantic.FieldRemove,
				Before: added,
			},
			wantBefore: projected,
			wantAfter:  map[string]any{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotBefore, gotAfter := projectFields(tt.before, tt.after, []semantic.FieldChange{tt.field})
			assert.Equal(t, tt.wantBefore, gotBefore)
			assert.Equal(t, tt.wantAfter, gotAfter)
		})
	}
}
