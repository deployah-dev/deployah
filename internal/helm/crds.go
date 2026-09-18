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

package helm

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"deployah.dev/deployah/internal/extras"
)

const chartCRDsDir = "crds"

func copyChartWithCRDs(backing string, crds []extras.Object) (string, error) {
	copyDir, err := createChartCopy(backing)
	if err != nil {
		return "", err
	}
	err = materializeChartCRDs(copyDir, crds)
	if err != nil {
		if removeErr := os.RemoveAll(copyDir); removeErr != nil {
			return "", fmt.Errorf("write chart crds: %w", errors.Join(err, removeErr))
		}
		return "", err
	}
	return copyDir, nil
}

func materializeChartCRDs(chartDir string, crds []extras.Object) error {
	if len(crds) == 0 {
		return nil
	}

	type group struct {
		path string
		raws [][]byte
	}
	order := make([]string, 0)
	groups := make(map[string]*group)
	for i := range crds {
		path := crds[i].Path
		g, ok := groups[path]
		if !ok {
			g = &group{path: path}
			groups[path] = g
			order = append(order, path)
		}
		if len(bytes.TrimSpace(crds[i].Raw)) == 0 {
			return fmt.Errorf("chart crd %s: empty document", path)
		}
		g.raws = append(g.raws, crds[i].Raw)
	}

	dests := make(map[string]string, len(groups))
	files := make(map[string][]byte, len(groups))
	for _, path := range order {
		name, err := crdFileName(path)
		if err != nil {
			return err
		}
		if prev, ok := dests[name]; ok {
			return fmt.Errorf("chart crd %s: destination %s collides with %s", path, name, prev)
		}
		dests[name] = path
		files[name] = joinCRDDocuments(groups[path].raws)
	}

	crdsDir := filepath.Join(chartDir, chartCRDsDir)
	if err := os.MkdirAll(crdsDir, 0o750); err != nil {
		return fmt.Errorf("create chart crds directory: %w", err)
	}

	for _, name := range slices.Sorted(maps.Keys(files)) {
		destPath := filepath.Join(crdsDir, name)
		if filepath.Dir(destPath) != crdsDir {
			return fmt.Errorf("chart crd %s: destination escapes %s", dests[name], crdsDir)
		}
		if err := os.WriteFile(destPath, files[name], 0o600); err != nil {
			return fmt.Errorf("write chart crd %s: %w", destPath, err)
		}
	}
	return nil
}

func crdFileName(path string) (string, error) {
	name := filepath.Base(path)
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("chart crd %q: unsafe file name", path)
	}
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\\') {
		return "", fmt.Errorf("chart crd %q: unsafe file name", path)
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".yaml" && ext != ".yml" {
		return "", fmt.Errorf("chart crd %q: file name must end in .yaml or .yml", path)
	}
	return name, nil
}

func joinCRDDocuments(raws [][]byte) []byte {
	var b bytes.Buffer
	for i, raw := range raws {
		if i > 0 {
			b.WriteString("---\n")
		}
		b.Write(raw)
		if !bytes.HasSuffix(raw, []byte("\n")) {
			b.WriteByte('\n')
		}
	}
	return b.Bytes()
}
