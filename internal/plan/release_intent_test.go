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
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/testing/testpath"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

const releaseIntentFixtures = "testdata/release_intent"

// TestReleaseIntentChanged runs each testdata/release_intent directory.
// result.golden holds "changed" or "unchanged"; error.golden holds the
// expected error text.
func TestReleaseIntentChanged(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(releaseIntentFixtures)
	require.NoError(t, err)
	var ran int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		ran++
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(releaseIntentFixtures, name)
			got, cmpErr := releaseIntentChanged(
				testpath.ReadFile(t, dir, "previous.yaml"),
				readHookFixtures(t, filepath.Join(dir, "previous.hooks")),
				testpath.ReadFile(t, dir, "desired.yaml"),
				readHookFixtures(t, filepath.Join(dir, "desired.hooks")),
			)
			_, hasResult := readOptionalFixture(t, dir, "result.golden")
			wantErr, hasError := readOptionalFixture(t, dir, "error.golden")
			require.NotEqualf(t, hasResult, hasError, "%s must contain exactly one of result.golden and error.golden", name)
			if hasError {
				require.Errorf(t, cmpErr, "releaseIntentChanged(%s) error = nil, want %q", name, wantErr)
				assert.ErrorContains(t, cmpErr, strings.TrimSpace(wantErr), "releaseIntentChanged(%s)", name)
				return
			}
			require.NoErrorf(t, cmpErr, "releaseIntentChanged(%s)", name)
			want := strings.TrimSpace(testpath.ReadFile(t, dir, "result.golden"))
			require.Containsf(t, []string{"changed", "unchanged"}, want, "%s/result.golden", name)
			assert.Equalf(t, want == "changed", got, "releaseIntentChanged(%s) = %v, want %s", name, got, want)
		})
	}
	require.NotZero(t, ran, "no fixtures under %s", releaseIntentFixtures)
}

func TestReleaseIntentChanged_NilHook(t *testing.T) {
	t.Parallel()
	hook := testHook("Job", "migrate", "migrate", "busybox", 1)
	tests := []struct {
		name         string
		prevHooks    []*v1.Hook
		desiredHooks []*v1.Hook
		want         string
	}{
		{name: "nil previous hook", prevHooks: []*v1.Hook{hook, nil}, want: "previous hook 2 is nil"},
		{name: "nil desired hook", desiredHooks: []*v1.Hook{nil}, want: "desired hook 1 is nil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := releaseIntentChanged("", tt.prevHooks, "", tt.desiredHooks)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func readOptionalFixture(tb testing.TB, dir, name string) (string, bool) {
	tb.Helper()
	_, err := os.Stat(filepath.Join(dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false
	}
	require.NoError(tb, err)
	return testpath.ReadFile(tb, dir, name), true
}

// readHookFixtures loads *.yaml files from dir in lexical order, so a
// numeric prefix such as "01-" sets hook order. A missing dir means no
// hooks.
func readHookFixtures(tb testing.TB, dir string) []*v1.Hook {
	tb.Helper()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	require.NoError(tb, err)
	var hooks []*v1.Hook
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		hooks = append(hooks, &v1.Hook{
			Name:     hookFixtureName(entry.Name()),
			Manifest: testpath.ReadFile(tb, dir, entry.Name()),
		})
	}
	return hooks
}

func hookFixtureName(file string) string {
	base := strings.TrimSuffix(file, filepath.Ext(file))
	prefix, rest, ok := strings.Cut(base, "-")
	if ok && prefix != "" && strings.Trim(prefix, "0123456789") == "" {
		return rest
	}
	return base
}
