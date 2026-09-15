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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestCRDSurface_AddConflict(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		first   crdAPI
		second  crdAPI
		wantErr string
	}{
		{
			name: "same GVK different plural",
			first: crdAPI{
				Name: "widgets.example.com", Group: "example.com", Kind: "Widget", Plural: "widgets", Versions: []string{"v1"},
			},
			second: crdAPI{
				Name: "objects.example.com", Group: "example.com", Kind: "Widget", Plural: "objects", Versions: []string{"v1"},
			},
			wantErr: "conflicting CRD API",
		},
		{
			name: "same GVR different kind",
			first: crdAPI{
				Name: "widgets.example.com", Group: "example.com", Kind: "Widget", Plural: "objects", Versions: []string{"v1"},
			},
			second: crdAPI{
				Name: "gadgets.example.com", Group: "example.com", Kind: "Gadget", Plural: "objects", Versions: []string{"v1"},
			},
			wantErr: "conflicting CRD resource",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newCRDSurface()
			require.NoError(t, s.add(tt.first, false))
			err := s.add(tt.second, false)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestCRDSurface_Add(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		apis []crdAPI
		want []schema.GroupVersionKind
	}{
		{
			name: "multi-version and distinct CRDs",
			apis: []crdAPI{
				{Name: "widgets.example.com", Group: "example.com", Kind: "Widget", Plural: "widgets", Versions: []string{"v1", "v2"}},
				{Name: "gadgets.other.com", Group: "other.com", Kind: "Gadget", Plural: "gadgets", Versions: []string{"v1"}},
			},
			want: []schema.GroupVersionKind{
				{Group: "example.com", Version: "v1", Kind: "Widget"},
				{Group: "example.com", Version: "v2", Kind: "Widget"},
				{Group: "other.com", Version: "v1", Kind: "Gadget"},
			},
		},
		{
			name: "same descriptor twice",
			apis: []crdAPI{
				{Name: "widgets.example.com", Group: "example.com", Kind: "Widget", Plural: "widgets", Versions: []string{"v1"}},
				{Name: "widgets.example.com", Group: "example.com", Kind: "Widget", Plural: "widgets", Versions: []string{"v1"}},
			},
			want: []schema.GroupVersionKind{
				{Group: "example.com", Version: "v1", Kind: "Widget"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newCRDSurface()
			for _, api := range tt.apis {
				require.NoError(t, s.add(api, false))
			}
			for _, gvk := range tt.want {
				assert.Contains(t, s.served, gvk)
			}
			assert.Len(t, s.served, len(tt.want))
		})
	}
}
