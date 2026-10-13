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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes/fake"

	fakediscovery "k8s.io/client-go/discovery/fake"
	k8stesting "k8s.io/client-go/testing"
)

func fakeClientWithVersion(tb testing.TB, major, minor string) *fake.Clientset {
	tb.Helper()
	cs := fake.NewSimpleClientset()
	fd, ok := cs.Discovery().(*fakediscovery.FakeDiscovery)
	require.True(tb, ok)
	fd.FakedServerVersion = &version.Info{Major: major, Minor: minor}
	return cs
}

func TestCheckMinimumVersion_Pass(t *testing.T) {
	t.Parallel()
	err := CheckMinimumVersion(fakeClientWithVersion(t, "1", "32"), 1, 32, "kind: stateful")
	assert.NoError(t, err)
}

func TestCheckMinimumVersion_HigherPass(t *testing.T) {
	t.Parallel()
	err := CheckMinimumVersion(fakeClientWithVersion(t, "1", "33+"), 1, 32, "kind: stateful")
	assert.NoError(t, err)
}

func TestCheckMinimumVersion_NewerMajorPasses(t *testing.T) {
	t.Parallel()
	err := CheckMinimumVersion(fakeClientWithVersion(t, "2", "0"), 1, 32, "kind: stateful")
	assert.NoError(t, err)
}

func TestCheckMinimumVersion_Fail(t *testing.T) {
	t.Parallel()
	err := CheckMinimumVersion(fakeClientWithVersion(t, "1", "31"), 1, 32, "kind: stateful requires Kubernetes 1.32+")
	require.Error(t, err)
	assert.ErrorContains(t, err, "below required 1.32")
	assert.ErrorContains(t, err, "kind: stateful")
}

func TestCheckMinimumVersion_OlderMajorFails(t *testing.T) {
	t.Parallel()
	err := CheckMinimumVersion(fakeClientWithVersion(t, "0", "40"), 1, 32, "kind: stateful")
	require.Error(t, err)
	assert.ErrorContains(t, err, "version 0.40 is below required 1.32")
	assert.ErrorContains(t, err, "kind: stateful")
}

func TestCheckMinimumVersion_DiscoveryError(t *testing.T) {
	t.Parallel()
	cs := fake.NewSimpleClientset()
	fd, ok := cs.Discovery().(*fakediscovery.FakeDiscovery)
	require.True(t, ok)
	fd.PrependReactor("get", "version", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("unreachable")
	})
	err := CheckMinimumVersion(cs, 1, 32, "kind: stateful")
	require.Error(t, err)
	assert.ErrorContains(t, err, "discover cluster version")
	assert.ErrorContains(t, err, "unreachable")
}

func TestCheckMinimumVersion_ParseError(t *testing.T) {
	t.Parallel()
	err := CheckMinimumVersion(fakeClientWithVersion(t, "x", "32"), 1, 32, "reason")
	require.Error(t, err)
	assert.ErrorContains(t, err, `parse cluster major version "x"`)
}

func TestParseVersionPart(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want int
	}{
		{name: "major", in: "1", want: 1},
		{name: "minor", in: "32", want: 32},
		{name: "plus suffix", in: "32+", want: 32},
		{name: "dotted", in: "32.0", want: 32},
		{name: "whitespace and plus", in: " 33+ ", want: 33},
		{name: "dash suffix", in: "32-gke.1", want: 32},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseVersionPart(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseVersionPart_Empty(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
	}{
		{name: "empty", in: ""},
		{name: "whitespace", in: "  "},
		{name: "plus only", in: "+"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseVersionPart(tt.in)
			require.Error(t, err)
			assert.ErrorContains(t, err, "empty version part")
		})
	}
}

func TestParseVersionPart_NonNumeric(t *testing.T) {
	t.Parallel()
	_, err := parseVersionPart("abc")
	require.Error(t, err)
}

func TestCheckMinimumVersion_MinorParseError(t *testing.T) {
	t.Parallel()
	err := CheckMinimumVersion(fakeClientWithVersion(t, "1", "abc"), 1, 32, "reason")
	require.Error(t, err)
	assert.ErrorContains(t, err, `parse cluster minor version "abc"`)
}

func TestCheckMinimumVersion_EmptyMinor(t *testing.T) {
	t.Parallel()
	err := CheckMinimumVersion(fakeClientWithVersion(t, "1", ""), 1, 32, "reason")
	require.Error(t, err)
	assert.ErrorContains(t, err, `parse cluster minor version ""`)
	assert.ErrorContains(t, err, "empty version part")
}
