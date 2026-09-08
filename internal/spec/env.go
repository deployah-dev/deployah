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

package spec

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// parseEnvFile parses .env files.
//
// If explicitlySet is true, a missing file returns [*ResolutionError] with
// [ErrCodeEnvFileNotFound]. If explicitlySet is false, a missing file is
// ignored. A file that exists but cannot be read returns [*ResolutionError]
// with [ErrCodeEnvFileReadError].
func parseEnvFile(path string, explicitlySet bool) (map[string]string, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- env file path from spec or CLI
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if explicitlySet {
				return nil, &ResolutionError{
					Code:    ErrCodeEnvFileNotFound,
					Message: fmt.Sprintf("failed to read %s file: %v", path, err),
				}
			}
			return map[string]string{}, nil
		}
		return nil, &ResolutionError{
			Code:    ErrCodeEnvFileReadError,
			Message: fmt.Sprintf("failed to read %s file: %v", path, err),
		}
	}

	vars := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if before, after, ok := strings.Cut(line, "="); ok {
			key := strings.TrimSpace(before)
			val := strings.TrimSpace(after)
			vars[key] = val
		}
	}

	if err = scanner.Err(); err != nil {
		return nil, &ResolutionError{
			Code:    ErrCodeEnvFileReadError,
			Message: fmt.Sprintf("error reading %s: %v", path, err),
		}
	}

	return vars, nil
}

// parseOSVariables parses the current OS environment variables from
// [os.Environ].
func parseOSVariables() (map[string]string, error) {
	env := os.Environ()
	vars := make(map[string]string)
	for _, e := range env {
		if before, after, ok := strings.Cut(e, "="); ok {
			key := before
			val := after
			vars[key] = val
		}
	}
	return vars, nil
}

// filterVariables separates variables into two maps based on their prefix.
// - deployahVars: variables prefixed with DPY_VAR_ used for spec substitution
// - envVars: remaining keys (unused by substitution)
//
// The DPY_VAR_ prefix is stripped from the keys in the returned deployahVars map.
func filterVariables(vars map[string]string) (map[string]string, map[string]string) {
	deployahVars := make(map[string]string)
	envVars := make(map[string]string)

	for key, value := range vars {
		if trimmed, found := strings.CutPrefix(key, EnvVarPrefix); found {
			deployahVars[trimmed] = value
		} else {
			envVars[key] = value
		}
	}

	return deployahVars, envVars
}
