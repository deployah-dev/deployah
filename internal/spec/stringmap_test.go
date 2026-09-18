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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestStringMap_StringifiesScalars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want StringMap
	}{
		{name: "string", raw: "LOG_LEVEL: debug", want: StringMap{"LOG_LEVEL": "debug"}},
		{name: "bool true", raw: "DEBUG: true", want: StringMap{"DEBUG": "true"}},
		{name: "bool false", raw: "DEBUG: false", want: StringMap{"DEBUG": "false"}},
		{name: "int", raw: "MAX_RETRIES: 5", want: StringMap{"MAX_RETRIES": "5"}},
		{name: "float", raw: "TIMEOUT: 30.5", want: StringMap{"TIMEOUT": "30.5"}},
		{name: "large integer", raw: "BIG_ID: 9007199254740993", want: StringMap{"BIG_ID": "9007199254740993"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got struct {
				Env StringMap `yaml:"env"`
			}
			require.NoError(t, yaml.Unmarshal([]byte("env:\n  "+tt.raw+"\n"), &got))
			assert.Equal(t, tt.want, got.Env)
		})
	}
}

func TestStringMap_JSONPreservesLargeInteger(t *testing.T) {
	t.Parallel()

	var got StringMap
	require.NoError(t, json.Unmarshal([]byte(`{"BIG_ID":9007199254740993}`), &got))
	assert.Equal(t, "9007199254740993", got["BIG_ID"])
}

func TestStringMap_RejectsNonScalars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{name: "nested map", raw: "NESTED:\n    FOO: bar"},
		{name: "list", raw: "ITEMS:\n    - a\n    - b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got struct {
				Env StringMap `yaml:"env"`
			}
			err := yaml.Unmarshal([]byte("env:\n  "+tt.raw+"\n"), &got)
			require.Error(t, err)
			assert.ErrorContains(t, err, "expected string, number, or boolean")
		})
	}
}

func TestStringMap_YAML11ReservedKeys(t *testing.T) {
	t.Parallel()

	t.Run("quoted keys survive", func(t *testing.T) {
		t.Parallel()
		var got struct {
			Env StringMap `yaml:"env"`
		}
		raw := `env:
  "ON": enabled
  "OFF": disabled
  "YES": yes-value
  "NO": no-value
  "TRUE": true-value
  "FALSE": false-value
  "NULL": empty
`
		require.NoError(t, yaml.Unmarshal([]byte(raw), &got))
		assert.Equal(t, StringMap{
			"ON":    "enabled",
			"OFF":   "disabled",
			"YES":   "yes-value",
			"NO":    "no-value",
			"TRUE":  "true-value",
			"FALSE": "false-value",
			"NULL":  "empty",
		}, got.Env)
	})

	t.Run("unquoted YAML 1.1 keys are rewritten", func(t *testing.T) {
		t.Parallel()
		var got struct {
			Env StringMap `yaml:"env"`
		}
		require.NoError(t, yaml.Unmarshal([]byte("env:\n  ON: enabled\n"), &got))
		_, hasON := got.Env["ON"]
		assert.False(t, hasON, "unquoted ON must not survive YAML 1.1 as ON; quote it in the spec")
		require.Len(t, got.Env, 1)
		for key, val := range got.Env {
			assert.Equal(t, "enabled", val)
			assert.NotEqual(t, "ON", key)
		}
	})
}

func TestSubstituteVariables_Sources(t *testing.T) {
	tests := []struct {
		name      string
		process   string
		dotenv    string
		variables StringMap
		want      string
	}{
		{
			name:      "ignores dotenv",
			dotenv:    "DPY_VAR_IMAGE=from-file\n",
			variables: StringMap{"IMAGE": "from-variables"},
			want:      "image: from-variables\n",
		},
		{
			name:      "process overrides variables",
			process:   "from-process",
			variables: StringMap{"IMAGE": "from-variables"},
			want:      "image: from-process\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.process != "" {
				t.Setenv("DPY_VAR_IMAGE", tt.process)
			}
			var envFile string
			if tt.dotenv != "" {
				dir := t.TempDir()
				requireWrite(t, dir, ".env", tt.dotenv)
				envFile = dir + "/.env"
			}
			got, err := SubstituteVariables([]byte("image: ${IMAGE}\n"), &Environment{
				EnvFile:   envFile,
				Variables: tt.variables,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
}
