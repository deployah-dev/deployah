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

package yamlowned_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNonBlankLineCounts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		dir  string
		want int
	}{
		{dir: "deployah", want: 17},
		{dir: "kubernetes", want: 135},
		{dir: "kustomize", want: 145},
		{dir: "helm", want: 340},
	}

	for _, tc := range cases {
		t.Run(tc.dir, func(t *testing.T) {
			t.Parallel()
			got, err := countNonBlank(tc.dir)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got, "non-blank lines in %s (run examples/yaml-owned/count.sh; update README and deployah.dev if this changes)", tc.dir)
		})
	}
}

func countNonBlank(root string) (int, error) {
	total := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".yaml", ".yml", ".tpl":
		default:
			return nil
		}
		n, countErr := nonBlankLines(path)
		if countErr != nil {
			return countErr
		}
		total += n
		return nil
	})
	return total, err
}

func nonBlankLines(path string) (int, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is under examples/yaml-owned
	if err != nil {
		return 0, err
	}
	n := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n, nil
}
