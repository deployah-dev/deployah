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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/validation"

	"deployah.dev/deployah/internal/spec"
)

func TestNormalizeEnv_IdentityFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		original string
		mapKey   string
		k8sSafe  string
	}{
		{
			name:     "plain production",
			input:    "production",
			original: "production",
			mapKey:   "production",
			k8sSafe:  "production",
		},
		{
			name:     "wildcard review instance",
			input:    "review/pr-123",
			original: "review/pr-123",
			mapKey:   "review",
			k8sSafe:  "review-pr-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := spec.NormalizeEnv(tt.input)
			assert.Equal(t, tt.original, got.Original)
			assert.Equal(t, tt.mapKey, got.MapKey)
			assert.Equal(t, tt.k8sSafe, got.K8sSafe)
			require.Empty(t, validation.IsValidLabelValue(got.MapKey))
		})
	}
}

func TestNormalizeEnv_TruncatesWithHash(t *testing.T) {
	t.Parallel()

	a := spec.NormalizeEnv(strings.Repeat("a", 60))
	b := spec.NormalizeEnv(strings.Repeat("a", 50) + strings.Repeat("b", 10))
	assert.Equal(t, 53, len(a.K8sSafe))
	assert.Equal(t, 53, len(b.K8sSafe))
	assert.NotEqual(t, a.K8sSafe, b.K8sSafe)
	assert.True(t, strings.HasPrefix(a.K8sSafe, strings.Repeat("a", 48)))
	assert.True(t, strings.HasPrefix(b.K8sSafe, strings.Repeat("a", 48)))
	require.Empty(t, validation.IsValidLabelValue(a.K8sSafe))
	require.Empty(t, validation.IsValidLabelValue(b.K8sSafe))
}
