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

package spec_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"deployah.dev/deployah/internal/spec"

	corev1 "k8s.io/api/core/v1"
)

// TestResolveProfileNames covers default prepend, opt-out, and unknown names.
func TestResolveProfileNames(t *testing.T) {
	t.Parallel()

	profiles := map[string]spec.PlatformProfile{
		"default":    {NodeSelector: map[string]string{"workload": "general"}},
		"public-web": {PodLabels: map[string]string{"tier": "web"}},
	}

	tests := []struct {
		name      string
		component []string
		platform  map[string]spec.PlatformProfile
		want      []string
		wantErr   string
		errCode   string
	}{
		{
			name:      "omitted with default",
			component: nil,
			platform:  profiles,
			want:      []string{"default"},
		},
		{
			name:      "omitted without default",
			component: nil,
			platform:  map[string]spec.PlatformProfile{"public-web": {}},
			want:      nil,
		},
		{
			name:      "explicit list prepends default",
			component: []string{"public-web"},
			platform:  profiles,
			want:      []string{"default", "public-web"},
		},
		{
			name:      "explicit list without default",
			component: []string{"public-web"},
			platform:  map[string]spec.PlatformProfile{"public-web": {}},
			want:      []string{"public-web"},
		},
		{
			name:      "empty array with default is blocked",
			component: []string{},
			platform:  profiles,
			wantErr:   "not allowed",
			errCode:   spec.ErrCodeProfileOptOutBlocked,
		},
		{
			name:      "empty array without default opts out",
			component: []string{},
			platform:  map[string]spec.PlatformProfile{"public-web": {}},
			want:      []string{},
		},
		{
			name:      "named profiles without platform section",
			component: []string{"public-web"},
			platform:  nil,
			wantErr:   "no profiles section",
			errCode:   spec.ErrCodeProfileNotFound,
		},
		{
			name:      "default listed by user is not duplicated",
			component: []string{"default", "public-web"},
			platform:  profiles,
			want:      []string{"default", "public-web"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := spec.ResolveProfileNames(tt.component, tt.platform)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				var re *spec.ResolutionError
				require.ErrorAs(t, err, &re)
				assert.Equal(t, tt.errCode, re.Code)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestMergeProfiles covers map overlay, toleration union, and intersections.
func TestMergeProfiles(t *testing.T) {
	t.Parallel()

	profiles := map[string]spec.PlatformProfile{
		"a": {
			NodeSelector: map[string]string{"workload": "general", "zone": "a"},
			PodLabels:    map[string]string{"tier": "web"},
			Tolerations: []corev1.Toleration{
				{Key: "batch", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
			},
			AllowedDomains: []string{"public", "partner"},
			MaxResources:   &spec.ProfileMaxResources{CPU: spec.MustQuantity("2000m"), Memory: spec.MustQuantity("4Gi")},
			StorageClass:   "standard",
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: new(true),
			},
		},
		"b": {
			NodeSelector: map[string]string{"accelerator": "nvidia", "zone": "b"},
			Tolerations: []corev1.Toleration{
				{Key: "batch", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
				{Key: "nvidia.com/gpu", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
			},
			AllowedDomains: []string{"public", "internal"},
			MaxResources:   &spec.ProfileMaxResources{CPU: spec.MustQuantity("1000m"), Memory: spec.MustQuantity("8Gi")},
			StorageClass:   "fast",
			ContainerSecurityContext: &corev1.SecurityContext{
				ReadOnlyRootFilesystem: new(true),
			},
		},
		"unconstrained": {
			NodeSelector: map[string]string{"pool": "shared"},
		},
		"partner-only": {
			AllowedDomains: []string{"partner"},
		},
	}

	t.Run("unknown name lists available", func(t *testing.T) {
		t.Parallel()
		_, err := spec.MergeProfiles([]string{"missing"}, profiles)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"missing"`)
		assert.Contains(t, err.Error(), "available:")
		var re *spec.ResolutionError
		require.ErrorAs(t, err, &re)
		assert.Equal(t, spec.ErrCodeProfileNotFound, re.Code)
	})

	t.Run("deep merge maps last wins", func(t *testing.T) {
		t.Parallel()
		merged, err := spec.MergeProfiles([]string{"a", "b"}, profiles)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{
			"workload":    "general",
			"zone":        "b",
			"accelerator": "nvidia",
		}, merged.NodeSelector)
		assert.Equal(t, map[string]string{"tier": "web"}, merged.PodLabels)
	})

	t.Run("tolerations concatenate and dedupe", func(t *testing.T) {
		t.Parallel()
		merged, err := spec.MergeProfiles([]string{"a", "b"}, profiles)
		require.NoError(t, err)
		require.Len(t, merged.Tolerations, 2)
		assert.Equal(t, "batch", merged.Tolerations[0].Key)
		assert.Equal(t, "nvidia.com/gpu", merged.Tolerations[1].Key)
	})

	t.Run("allowedDomains intersection", func(t *testing.T) {
		t.Parallel()
		merged, err := spec.MergeProfiles([]string{"a", "b"}, profiles)
		require.NoError(t, err)
		assert.Equal(t, []string{"public"}, merged.AllowedDomains)
	})

	t.Run("disjoint allowedDomains is empty deny-all", func(t *testing.T) {
		t.Parallel()
		merged, err := spec.MergeProfiles([]string{"b", "partner-only"}, profiles)
		require.NoError(t, err)
		assert.Empty(t, merged.AllowedDomains)
		assert.NotNil(t, merged.AllowedDomains)

		data, err := json.Marshal(merged)
		require.NoError(t, err)
		assert.Contains(t, string(data), `"allowedDomains":[]`)

		yamlData, err := yaml.Marshal(merged)
		require.NoError(t, err)
		var roundTrip spec.PlatformProfile
		require.NoError(t, yaml.Unmarshal(yamlData, &roundTrip))
		assert.NotNil(t, roundTrip.AllowedDomains)
		assert.Empty(t, roundTrip.AllowedDomains)
	})

	t.Run("unconstrained allowedDomains does not narrow", func(t *testing.T) {
		t.Parallel()
		merged, err := spec.MergeProfiles([]string{"a", "unconstrained"}, profiles)
		require.NoError(t, err)
		assert.Equal(t, []string{"public", "partner"}, merged.AllowedDomains)
	})

	t.Run("maxResources min wins", func(t *testing.T) {
		t.Parallel()
		merged, err := spec.MergeProfiles([]string{"a", "b"}, profiles)
		require.NoError(t, err)
		require.NotNil(t, merged.MaxResources)
		assert.Equal(t, "1", merged.MaxResources.CPU.String())
		assert.Equal(t, "4Gi", merged.MaxResources.Memory.String())
	})

	t.Run("storageClass last wins", func(t *testing.T) {
		t.Parallel()
		merged, err := spec.MergeProfiles([]string{"a", "b"}, profiles)
		require.NoError(t, err)
		assert.Equal(t, "fast", merged.StorageClass)
	})

	t.Run("security contexts merge", func(t *testing.T) {
		t.Parallel()
		merged, err := spec.MergeProfiles([]string{"a", "b"}, profiles)
		require.NoError(t, err)
		require.NotNil(t, merged.SecurityContext)
		require.NotNil(t, merged.SecurityContext.RunAsNonRoot)
		assert.True(t, *merged.SecurityContext.RunAsNonRoot)
		require.NotNil(t, merged.ContainerSecurityContext)
		require.NotNil(t, merged.ContainerSecurityContext.ReadOnlyRootFilesystem)
		assert.True(t, *merged.ContainerSecurityContext.ReadOnlyRootFilesystem)
	})

	t.Run("security contexts override fields when both set", func(t *testing.T) {
		t.Parallel()
		uid1000 := int64(1000)
		uid2000 := int64(2000)
		both := map[string]spec.PlatformProfile{
			"base": {
				SecurityContext: &corev1.PodSecurityContext{
					RunAsNonRoot: new(true),
					RunAsUser:    &uid1000,
				},
				ContainerSecurityContext: &corev1.SecurityContext{
					ReadOnlyRootFilesystem: new(true),
					RunAsUser:              &uid1000,
				},
			},
			"overlay": {
				SecurityContext: &corev1.PodSecurityContext{
					RunAsUser: &uid2000,
				},
				ContainerSecurityContext: &corev1.SecurityContext{
					RunAsUser: &uid2000,
				},
			},
		}
		merged, err := spec.MergeProfiles([]string{"base", "overlay"}, both)
		require.NoError(t, err)
		require.NotNil(t, merged.SecurityContext)
		require.NotNil(t, merged.SecurityContext.RunAsNonRoot)
		assert.True(t, *merged.SecurityContext.RunAsNonRoot)
		require.NotNil(t, merged.SecurityContext.RunAsUser)
		assert.Equal(t, uid2000, *merged.SecurityContext.RunAsUser)
		require.NotNil(t, merged.ContainerSecurityContext)
		require.NotNil(t, merged.ContainerSecurityContext.ReadOnlyRootFilesystem)
		assert.True(t, *merged.ContainerSecurityContext.ReadOnlyRootFilesystem)
		require.NotNil(t, merged.ContainerSecurityContext.RunAsUser)
		assert.Equal(t, uid2000, *merged.ContainerSecurityContext.RunAsUser)
	})

	t.Run("security context false bool overlay wins", func(t *testing.T) {
		t.Parallel()
		both := map[string]spec.PlatformProfile{
			"permissive": {
				SecurityContext: &corev1.PodSecurityContext{
					RunAsNonRoot: new(true),
				},
				ContainerSecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: new(true),
					ReadOnlyRootFilesystem:   new(true),
				},
			},
			"restrictive": {
				SecurityContext: &corev1.PodSecurityContext{
					RunAsNonRoot: new(false),
				},
				ContainerSecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: new(false),
					ReadOnlyRootFilesystem:   new(false),
				},
			},
		}
		merged, err := spec.MergeProfiles([]string{"permissive", "restrictive"}, both)
		require.NoError(t, err)
		require.NotNil(t, merged.SecurityContext.RunAsNonRoot)
		assert.False(t, *merged.SecurityContext.RunAsNonRoot)
		require.NotNil(t, merged.ContainerSecurityContext.AllowPrivilegeEscalation)
		assert.False(t, *merged.ContainerSecurityContext.AllowPrivilegeEscalation)
		require.NotNil(t, merged.ContainerSecurityContext.ReadOnlyRootFilesystem)
		assert.False(t, *merged.ContainerSecurityContext.ReadOnlyRootFilesystem)
	})

	t.Run("maxResources complementary fields keep both", func(t *testing.T) {
		t.Parallel()
		parts := map[string]spec.PlatformProfile{
			"cpu-only":    {MaxResources: &spec.ProfileMaxResources{CPU: spec.MustQuantity("500m")}},
			"memory-only": {MaxResources: &spec.ProfileMaxResources{Memory: spec.MustQuantity("1Gi")}},
		}
		merged, err := spec.MergeProfiles([]string{"cpu-only", "memory-only"}, parts)
		require.NoError(t, err)
		require.NotNil(t, merged.MaxResources)
		assert.Equal(t, "500m", merged.MaxResources.CPU.String())
		assert.Equal(t, "1Gi", merged.MaxResources.Memory.String())
	})

	t.Run("empty maxResources collapse to nil", func(t *testing.T) {
		t.Parallel()
		empty := map[string]spec.PlatformProfile{
			"a": {MaxResources: &spec.ProfileMaxResources{}},
			"b": {MaxResources: &spec.ProfileMaxResources{}},
		}
		merged, err := spec.MergeProfiles([]string{"a", "b"}, empty)
		require.NoError(t, err)
		assert.Nil(t, merged.MaxResources)
	})

	t.Run("empty names returns zero", func(t *testing.T) {
		t.Parallel()
		merged, err := spec.MergeProfiles(nil, profiles)
		require.NoError(t, err)
		assert.Equal(t, spec.PlatformProfile{}, merged)
	})
}

// TestValidateProfileAgainstComponent covers domain, storage, and ceiling
// checks.
func TestValidateProfileAgainstComponent(t *testing.T) {
	t.Parallel()

	comp := spec.Component{
		Resources: spec.Resources{
			CPU:    spec.MustQuantity("2000m"),
			Memory: spec.MustQuantity("1Gi"),
		},
	}
	env := &spec.PlatformEnvironment{
		StorageClasses: map[string]spec.PlatformStorageClass{
			"fast": {ClassName: "fast-ssd"},
		},
	}

	t.Run("domain not allowed", func(t *testing.T) {
		t.Parallel()
		err := spec.ValidateProfileAgainstComponent("api", comp, spec.PlatformProfile{
			AllowedDomains: []string{"public"},
		}, env, "internal")
		require.Error(t, err)
		var re *spec.ResolutionError
		require.ErrorAs(t, err, &re)
		assert.Equal(t, spec.ErrCodeProfileDomainNotAllowed, re.Code)
	})

	t.Run("empty allowedDomains deny-all", func(t *testing.T) {
		t.Parallel()
		err := spec.ValidateProfileAgainstComponent("api", comp, spec.PlatformProfile{
			AllowedDomains: []string{},
		}, env, "public")
		require.Error(t, err)
		var re *spec.ResolutionError
		require.ErrorAs(t, err, &re)
		assert.Equal(t, spec.ErrCodeProfileDomainNotAllowed, re.Code)
	})

	t.Run("domain ignored without expose key", func(t *testing.T) {
		t.Parallel()
		err := spec.ValidateProfileAgainstComponent("api", comp, spec.PlatformProfile{
			AllowedDomains: []string{"public"},
		}, env, "")
		require.NoError(t, err)
	})

	t.Run("storage class missing", func(t *testing.T) {
		t.Parallel()
		err := spec.ValidateProfileAgainstComponent("api", comp, spec.PlatformProfile{
			StorageClass: "missing",
		}, env, "")
		require.Error(t, err)
		var re *spec.ResolutionError
		require.ErrorAs(t, err, &re)
		assert.Equal(t, spec.ErrCodeProfileStorageClassNotFound, re.Code)
	})

	t.Run("cpu exceeds ceiling", func(t *testing.T) {
		t.Parallel()
		err := spec.ValidateProfileAgainstComponent("api", comp, spec.PlatformProfile{
			MaxResources: &spec.ProfileMaxResources{CPU: spec.MustQuantity("1000m")},
		}, env, "")
		require.Error(t, err)
		var re *spec.ResolutionError
		require.ErrorAs(t, err, &re)
		assert.Equal(t, spec.ErrCodeProfileResourceExceeded, re.Code)
	})

	t.Run("memory exceeds ceiling", func(t *testing.T) {
		t.Parallel()
		err := spec.ValidateProfileAgainstComponent("api", comp, spec.PlatformProfile{
			MaxResources: &spec.ProfileMaxResources{Memory: spec.MustQuantity("512Mi")},
		}, env, "")
		require.Error(t, err)
		var re *spec.ResolutionError
		require.ErrorAs(t, err, &re)
		assert.Equal(t, spec.ErrCodeProfileResourceExceeded, re.Code)
		assert.Contains(t, re.Error(), "memory request")
	})

	t.Run("storage class with no environment storageClasses", func(t *testing.T) {
		t.Parallel()
		err := spec.ValidateProfileAgainstComponent("api", comp, spec.PlatformProfile{
			StorageClass: "fast",
		}, &spec.PlatformEnvironment{}, "")
		require.Error(t, err)
		var re *spec.ResolutionError
		require.ErrorAs(t, err, &re)
		assert.Equal(t, spec.ErrCodeProfileStorageClassNotFound, re.Code)
		assert.Contains(t, re.Error(), "has no storageClasses")
	})

	t.Run("within ceiling", func(t *testing.T) {
		t.Parallel()
		err := spec.ValidateProfileAgainstComponent("api", comp, spec.PlatformProfile{
			MaxResources:   &spec.ProfileMaxResources{CPU: spec.MustQuantity("2000m"), Memory: spec.MustQuantity("2Gi")},
			StorageClass:   "fast",
			AllowedDomains: []string{"public"},
		}, env, "public")
		require.NoError(t, err)
	})
}
