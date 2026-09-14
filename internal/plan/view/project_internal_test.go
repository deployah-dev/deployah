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
