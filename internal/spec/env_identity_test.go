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
// See the License for the specific language governing the License.

package spec_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/validation"

	"deployah.dev/deployah/internal/spec"

	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
)

func TestNormalizeEnv_IdentityFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		input        string
		original     string
		mapKey       string
		deploymentID string
	}{
		{
			name:         "plain production",
			input:        "production",
			original:     "production",
			mapKey:       "production",
			deploymentID: "production",
		},
		{
			name:         "hyphenated logical name",
			input:        "review-pr-123",
			original:     "review-pr-123",
			mapKey:       "review-pr-123",
			deploymentID: "review-pr-123",
		},
		{
			name:         "wildcard review instance",
			input:        "review/pr-123",
			original:     "review/pr-123",
			mapKey:       "review",
			deploymentID: "review--pr-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := spec.NormalizeEnv(tt.input)
			assert.Equal(t, tt.original, got.Original)
			assert.Equal(t, tt.mapKey, got.MapKey)
			assert.Equal(t, tt.deploymentID, got.DeploymentID)
			require.Empty(t, validation.IsValidLabelValue(got.MapKey))
		})
	}
}

func TestReleaseName_WildcardDoesNotCollideWithHyphenated(t *testing.T) {
	t.Parallel()

	slash := spec.NormalizeEnv("review/pr-123").ReleaseName("shop")
	hyphen := spec.NormalizeEnv("review-pr-123").ReleaseName("shop")
	assert.NotEqual(t, slash, hyphen)
	assert.Equal(t, "shop-review--pr-123", slash)
	assert.Equal(t, "shop-review-pr-123", hyphen)
	require.NoError(t, chartutil.ValidateReleaseName(slash))
	require.NoError(t, chartutil.ValidateReleaseName(hyphen))
}

func TestReleaseName_SiblingWildcardsDiffer(t *testing.T) {
	t.Parallel()

	a := spec.NormalizeEnv("review/pr-123").ReleaseName("shop")
	b := spec.NormalizeEnv("review/pr-456").ReleaseName("shop")
	assert.NotEqual(t, a, b)
}

func TestReleaseName_Deterministic(t *testing.T) {
	t.Parallel()

	first := spec.NormalizeEnv("review/pr-123").ReleaseName("shop")
	second := spec.NormalizeEnv("review/pr-123").ReleaseName("shop")
	assert.Equal(t, first, second)
}

func TestReleaseName_EmptyEnvironment(t *testing.T) {
	t.Parallel()

	got := spec.NormalizeEnv("").ReleaseName("shop")
	assert.Equal(t, "shop", got)
	require.NoError(t, chartutil.ValidateReleaseName(got))
}

func TestReleaseName_LengthAndHelmSyntax(t *testing.T) {
	t.Parallel()

	maxProject := strings.Repeat("p", spec.MaxProjectNameLength)
	maxEnv := strings.Repeat("e", spec.MaxEnvironmentNameLength)
	longSuffix := strings.Repeat("s", spec.MaxEnvironmentNameLength)

	cases := []struct {
		name    string
		project string
		env     string
	}{
		{name: "max project", project: maxProject, env: "prod"},
		{name: "max logical env", project: "shop", env: maxEnv},
		{name: "max project and env", project: maxProject, env: maxEnv},
		{name: "max project and wildcard", project: maxProject, env: "review/" + longSuffix},
		{name: "plain short", project: "shop", env: "production"},
		{name: "wildcard short", project: "shop", env: "review/pr-123"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := spec.NormalizeEnv(tt.env).ReleaseName(tt.project)
			assert.LessOrEqual(t, len(got), spec.HelmReleaseNameMax)
			require.NoError(t, chartutil.ValidateReleaseName(got), "ReleaseName(%q, %q) = %q", tt.project, tt.env, got)
			assert.False(t, strings.HasSuffix(got, "-"))
		})
	}
}

func TestReleaseName_LongSharedPrefixDistinct(t *testing.T) {
	t.Parallel()

	project := strings.Repeat("p", spec.MaxProjectNameLength)
	prefix := strings.Repeat("a", 50)
	a := spec.NormalizeEnv("review/" + prefix + "one").ReleaseName(project)
	b := spec.NormalizeEnv("review/" + prefix + "two").ReleaseName(project)
	assert.NotEqual(t, a, b)
	assert.LessOrEqual(t, len(a), spec.HelmReleaseNameMax)
	assert.LessOrEqual(t, len(b), spec.HelmReleaseNameMax)
	require.NoError(t, chartutil.ValidateReleaseName(a))
	require.NoError(t, chartutil.ValidateReleaseName(b))
}

func TestValidateRequestedEnv(t *testing.T) {
	t.Parallel()

	valid := []string{"review", "production", "staging-blue", "review/pr-123", "review/feature-123"}
	for _, name := range valid {
		t.Run("valid "+name, func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, spec.ValidateRequestedEnv(name))
		})
	}

	invalid := []struct {
		name string
		in   string
	}{
		{name: "trailing slash", in: "review/"},
		{name: "double slash", in: "review//pr-123"},
		{name: "nested slash", in: "review/pr/123"},
		{name: "uppercase instance", in: "review/PR-123"},
		{name: "underscore instance", in: "review/pr_123"},
		{name: "space instance", in: "review/pr 123"},
		{name: "consecutive dash instance", in: "review/pr--123"},
		{name: "consecutive dash logical", in: "review--x"},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Error(t, spec.ValidateRequestedEnv(tt.in))
		})
	}
}

func TestReleaseName_AcceptedOriginalsStayDistinct(t *testing.T) {
	t.Parallel()

	project := "shop"
	originals := []string{
		"review",
		"review-pr-123",
		"review/pr-123",
		"review/pr-456",
		"review/feature-123",
		"production",
		"staging-blue",
	}
	seen := map[string]string{}
	for _, orig := range originals {
		require.NoError(t, spec.ValidateRequestedEnv(orig))
		got := spec.NormalizeEnv(orig).ReleaseName(project)
		require.NoError(t, chartutil.ValidateReleaseName(got))
		if prev, ok := seen[got]; ok {
			t.Fatalf("ReleaseName(%q) collided %q and %q", got, prev, orig)
		}
		seen[got] = orig
	}
}

func FuzzReleaseNameCollision(f *testing.F) {
	f.Add("shop", "review/pr-123", "review-pr-123")
	f.Add("shop", "review/pr-123", "review/pr-456")
	f.Add("billing", "production", "staging")
	f.Fuzz(func(t *testing.T, project, a, b string) {
		if spec.ValidateProjectName(project) != nil {
			return
		}
		if spec.ValidateRequestedEnv(a) != nil || spec.ValidateRequestedEnv(b) != nil {
			return
		}
		if a == b {
			return
		}
		na := spec.NormalizeEnv(a).ReleaseName(project)
		nb := spec.NormalizeEnv(b).ReleaseName(project)
		if err := chartutil.ValidateReleaseName(na); err != nil {
			t.Fatalf("ReleaseName(%q, %q) = %q: %v", project, a, na, err)
		}
		if err := chartutil.ValidateReleaseName(nb); err != nil {
			t.Fatalf("ReleaseName(%q, %q) = %q: %v", project, b, nb, err)
		}
		if na == nb {
			t.Fatalf("collision: %q and %q both map to %s", a, b, na)
		}
		assert.LessOrEqual(t, len(na), spec.HelmReleaseNameMax)
		assert.LessOrEqual(t, len(nb), spec.HelmReleaseNameMax)
	})
}

func ExampleEnvIdentity_ReleaseName() {
	fmt.Println(spec.NormalizeEnv("review/pr-123").ReleaseName("shop"))
	fmt.Println(spec.NormalizeEnv("review-pr-123").ReleaseName("shop"))
	// Output:
	// shop-review--pr-123
	// shop-review-pr-123
}
