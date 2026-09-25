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

// Package testpath provides path helpers for test fixtures.
package testpath

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Dir joins elems and requires the resulting path to be a directory.
func Dir(tb testing.TB, elems ...string) string {
	tb.Helper()

	dir := filepath.Join(elems...)
	info, err := os.Stat(dir)
	require.NoErrorf(tb, err, "stat fixture directory %s", dir)
	require.Truef(tb, info.IsDir(), "fixture %s is not a directory", dir)

	return dir
}

// ReadFile joins elems, reads that file, and returns its contents. It
// fails the test if the file cannot be read.
func ReadFile(tb testing.TB, elems ...string) string {
	tb.Helper()

	path := filepath.Clean(filepath.Join(elems...))
	raw, err := os.ReadFile(path) // #nosec G304 -- test fixture path chosen by the caller
	require.NoErrorf(tb, err, "read fixture %s", path)

	return string(raw)
}

// AbsDir returns the absolute path to the fixture directory formed by elems.
func AbsDir(tb testing.TB, elems ...string) string {
	tb.Helper()

	dir := Dir(tb, elems...)
	abs, err := filepath.Abs(dir)
	require.NoErrorf(tb, err, "make fixture directory %s absolute", dir)

	return abs
}
