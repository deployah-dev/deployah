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

package semantic_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/plan/semantic"
)

func TestProjectOntoDeclared(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		live     map[string]any
		declared map[string]any
		want     map[string]any
	}{
		{
			name: "array image kept and tail dropped",
			live: map[string]any{
				"spec": map[string]any{
					"template": map[string]any{
						"spec": map[string]any{
							"containers": []any{
								map[string]any{"name": "app", "image": "nginx:live"},
								map[string]any{"name": "sidecar", "image": "busybox"},
							},
						},
					},
				},
				"status": map[string]any{"replicas": 2},
			},
			declared: map[string]any{
				"spec": map[string]any{
					"template": map[string]any{
						"spec": map[string]any{
							"containers": []any{
								map[string]any{"name": "app", "image": "nginx:desired"},
							},
						},
					},
				},
			},
			want: map[string]any{
				"spec": map[string]any{
					"template": map[string]any{
						"spec": map[string]any{
							"containers": []any{
								map[string]any{"name": "app", "image": "nginx:live"},
							},
						},
					},
				},
			},
		},
		{
			name: "live-only map keys dropped",
			live: map[string]any{
				"data":   map[string]any{"key": "v1", "extra": "live-only"},
				"status": map[string]any{"observed": 1},
				"metadata": map[string]any{
					"name":            "app",
					"resourceVersion": "9",
					"uid":             "abc",
				},
			},
			declared: map[string]any{
				"data": map[string]any{"key": "v1"},
				"metadata": map[string]any{
					"name": "app",
				},
			},
			want: map[string]any{
				"data":     map[string]any{"key": "v1"},
				"metadata": map[string]any{"name": "app"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := semantic.ProjectOntoDeclared(tt.live, tt.declared)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProjectOntoDeclared_NamedMisalignment(t *testing.T) {
	t.Parallel()
	live := map[string]any{
		"spec": map[string]any{
			"containers": []any{map[string]any{"name": "sidecar", "image": "busybox"}},
		},
	}
	declared := map[string]any{
		"spec": map[string]any{
			"containers": []any{map[string]any{"name": "app", "image": "nginx"}},
		},
	}
	_, err := semantic.ProjectOntoDeclared(live, declared)
	require.Error(t, err)
	assert.ErrorContains(t, err, "misaligned")
}
