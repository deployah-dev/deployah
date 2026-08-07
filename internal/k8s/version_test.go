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

package k8s

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes/fake"

	fakediscovery "k8s.io/client-go/discovery/fake"
)

func fakeClientWithVersion(major, minor string) *fake.Clientset {
	cs := fake.NewSimpleClientset()
	fd, ok := cs.Discovery().(*fakediscovery.FakeDiscovery)
	if !ok {
		panic("expected *fakediscovery.FakeDiscovery")
	}
	fd.FakedServerVersion = &version.Info{Major: major, Minor: minor}
	return cs
}

func TestCheckMinimumVersion_Pass(t *testing.T) {
	t.Parallel()
	err := CheckMinimumVersion(fakeClientWithVersion("1", "32"), 1, 32, "kind: stateful")
	assert.NoError(t, err)
}

func TestCheckMinimumVersion_HigherPass(t *testing.T) {
	t.Parallel()
	err := CheckMinimumVersion(fakeClientWithVersion("1", "33+"), 1, 32, "kind: stateful")
	assert.NoError(t, err)
}

func TestCheckMinimumVersion_Fail(t *testing.T) {
	t.Parallel()
	err := CheckMinimumVersion(fakeClientWithVersion("1", "31"), 1, 32, "kind: stateful requires Kubernetes 1.32+")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "below required 1.32")
	assert.Contains(t, err.Error(), "kind: stateful")
}

func TestCheckMinimumVersion_ParseError(t *testing.T) {
	t.Parallel()
	err := CheckMinimumVersion(fakeClientWithVersion("x", "32"), 1, 32, "reason")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse cluster major version")
}

func TestParseVersionPart(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want int
	}{
		{"1", 1},
		{"32", 32},
		{"32+", 32},
		{"32.0", 32},
	}
	for _, tt := range tests {
		got, err := parseVersionPart(tt.in)
		require.NoError(t, err, tt.in)
		assert.Equal(t, tt.want, got, tt.in)
	}
}

func TestParseVersionPart_Empty(t *testing.T) {
	t.Parallel()
	_, err := parseVersionPart("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty version part")
}

func TestParseVersionPart_NonNumeric(t *testing.T) {
	t.Parallel()
	_, err := parseVersionPart("abc")
	require.Error(t, err)
}

func TestCheckMinimumVersion_MinorParseError(t *testing.T) {
	t.Parallel()
	err := CheckMinimumVersion(fakeClientWithVersion("1", "abc"), 1, 32, "reason")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse cluster minor version")
}
