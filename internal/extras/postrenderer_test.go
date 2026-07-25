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

package extras_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"deployah.dev/deployah/internal/extras"
)

// TestPostRenderer_AppendsAndPreservesBraces exercises extras package behavior.
func TestPostRenderer_AppendsAndPreservesBraces(t *testing.T) {
	t.Parallel()
	extra := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "monitoring.coreos.com/v1",
		"kind":       "PrometheusRule",
		"metadata": map[string]any{
			"name":      "alerts",
			"namespace": "apps",
		},
		"spec": map[string]any{
			"groups": []any{
				map[string]any{
					"name": "demo",
					"rules": []any{
						map[string]any{
							"alert": "Down",
							"annotations": map[string]any{
								"summary": "instance {{ $labels.instance }} is down",
							},
						},
					},
				},
			},
		},
	}}
	raw, err := (&extras.Object{Obj: extra}).MarshalYAML()
	require.NoError(t, err)

	pr := &extras.PostRenderer{Manifests: []extras.Object{{
		Path: "alerts.yaml",
		Raw:  raw,
		Obj:  extra,
	}}}

	rendered := bytes.NewBufferString(`apiVersion: v1
kind: ConfigMap
metadata:
  name: generated
  namespace: apps
`)
	out, err := pr.Run(rendered)
	require.NoError(t, err)
	body := out.String()
	assert.Contains(t, body, "name: generated")
	assert.Contains(t, body, "kind: PrometheusRule")
	assert.Contains(t, body, "instance {{ $labels.instance }} is down")
	assert.NotContains(t, body, `{{"{{"}}`)
}

// TestPostRenderer_CollisionFails exercises extras package behavior.
func TestPostRenderer_CollisionFails(t *testing.T) {
	t.Parallel()
	extra := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      "shared",
			"namespace": "apps",
		},
	}}
	raw, err := (&extras.Object{Obj: extra}).MarshalYAML()
	require.NoError(t, err)

	pr := &extras.PostRenderer{Manifests: []extras.Object{{
		Path: "extra.yaml",
		Raw:  raw,
		Obj:  extra,
	}}}
	rendered := bytes.NewBufferString(`apiVersion: v1
kind: ConfigMap
metadata:
  name: shared
  namespace: apps
`)
	_, err = pr.Run(rendered)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collides")
	assert.Contains(t, err.Error(), "extra.yaml")
}

// TestPostRenderer_NilOrEmptyPassthrough exercises extras package behavior.
func TestPostRenderer_NilOrEmptyPassthrough(t *testing.T) {
	t.Parallel()
	in := bytes.NewBufferString("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n")
	out, err := (*extras.PostRenderer)(nil).Run(in)
	require.NoError(t, err)
	assert.Equal(t, in.String(), out.String())

	out, err = (&extras.PostRenderer{}).Run(in)
	require.NoError(t, err)
	assert.Equal(t, in.String(), out.String())
}
