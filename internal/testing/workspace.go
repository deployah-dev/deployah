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

package testing

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// NewScenarioWorkspace copies sourceDir into a temporary directory
// and returns its absolute path. The directory is removed when tb ends.
func NewScenarioWorkspace(tb testing.TB, sourceDir string) (string, error) {
	tb.Helper()
	if err := validateScenarioSource(sourceDir); err != nil {
		return "", err
	}
	dst := tb.TempDir()
	if err := os.CopyFS(dst, os.DirFS(sourceDir)); err != nil {
		return "", fmt.Errorf("copy scenario %s: %w", sourceDir, err)
	}
	return dst, nil
}

// validateScenarioSource rejects a path that is not a real directory.
func validateScenarioSource(sourceDir string) error {
	// Lstat before WalkDir. WalkDir follows a symlink root, and CopyFS would
	// recreate a link that a later open could follow outside the workspace.
	info, err := os.Lstat(sourceDir)
	if err != nil {
		return fmt.Errorf("stat scenario %s: %w", sourceDir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("scenario %s is a symbolic link", sourceDir)
	}
	if !info.IsDir() {
		return fmt.Errorf("scenario %s is not a directory", sourceDir)
	}

	walkErr := filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		entry, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link %s", path)
		}
		return nil
	})
	if walkErr != nil {
		return fmt.Errorf("validate scenario %s: %w", sourceDir, walkErr)
	}
	return nil
}
