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
	"fmt"

	"dario.cat/mergo"
	"github.com/fluxcd/pkg/envsubst"
)

// SubstituteVariables substitutes variables in spec data using the
// provided environment. Variable precedence is lowest to highest:
// environment definition, env file, then OS environment variables.
// Substitution uses the envsubst syntax.
func SubstituteVariables(data []byte, env *Environment) ([]byte, error) {
	variables := make(map[string]string)

	// Merge variables from the environment definition (lowest priority)
	err := mergo.Merge(&variables, env.Variables)
	if err != nil {
		return nil, fmt.Errorf("failed to merge environment definition variables: %w", err)
	}

	// Load env file (medium priority), if explicitly set
	varsFromFile, err := parseEnvFile(env.EnvFile, env.EnvFile != "")
	if err != nil {
		return nil, fmt.Errorf("failed to parse environment file: %w", err)
	}

	// Filter variables, deployahVars (medium priority)
	deployahVarsFromFile, _ := filterVariables(varsFromFile)
	err = mergo.Merge(&variables, deployahVarsFromFile, mergo.WithOverride)
	if err != nil {
		return nil, fmt.Errorf("failed to merge env file variables: %w", err)
	}

	// Load OS environment variables (highest priority)
	osEnvVars, err := parseOSVariables()
	if err != nil {
		return nil, fmt.Errorf("failed to parse OS environment variables: %w", err)
	}

	// Filter variables, deployahVars (highest priority)
	deployahVarsFromOS, _ := filterVariables(osEnvVars)

	// Merge variables (highest priority)
	err = mergo.Merge(&variables, deployahVarsFromOS, mergo.WithOverride)
	if err != nil {
		return nil, fmt.Errorf("failed to merge OS environment variables: %w", err)
	}

	content, err := envsubst.Eval(string(data), func(s string) (string, bool) {
		if v, ok := variables[s]; ok {
			return v, true
		}
		return "", false
	})
	if err != nil {
		return nil, fmt.Errorf("failed to substitute variables: %w", err)
	}

	return []byte(content), nil
}
