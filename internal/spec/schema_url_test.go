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

package spec_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/spec"
	"deployah.dev/deployah/internal/spec/schema"
)

const jsonSchemaDialect = "https://json-schema.org/draft/2020-12/schema"

func TestSpecSchemaURL(t *testing.T) {
	t.Parallel()
	const prefix = "https://deployah.dev/schemas/spec/v1-alpha.5/"
	assert.Equal(t, "https://deployah.dev/schemas/spec/v1-alpha.5/schema.json", spec.SpecSchemaURL())

	root, err := schema.GetManifestSchema(spec.CurrentManifestVersion)
	require.NoError(t, err)
	id, dialect := mustSchemaMeta(t, root)
	assert.Equal(t, spec.SpecSchemaURL(), id)
	assert.Equal(t, jsonSchemaDialect, dialect)

	env, err := schema.GetEnvironmentsSchema(spec.CurrentManifestVersion)
	require.NoError(t, err)
	envID, envDialect := mustSchemaMeta(t, env)
	assert.True(t, strings.HasPrefix(envID, prefix), envID)
	assert.Equal(t, jsonSchemaDialect, envDialect)
}

func TestPlatformSchemaURL(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "https://deployah.dev/schemas/platform/v1-alpha.3/schema.json", spec.PlatformSchemaURL())

	version := strings.TrimPrefix(spec.CurrentPlatformVersion, "platform/")
	raw, err := schema.GetPlatformSchema(version)
	require.NoError(t, err)
	id, dialect := mustSchemaMeta(t, raw)
	assert.Equal(t, spec.PlatformSchemaURL(), id)
	assert.Equal(t, jsonSchemaDialect, dialect)
}

func mustSchemaMeta(t *testing.T, raw []byte) (id, dialect string) {
	t.Helper()
	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	id, ok := doc["$id"].(string)
	require.True(t, ok, "$id must be a string")
	dialect, ok = doc["$schema"].(string)
	require.True(t, ok, "$schema must be a string")
	return id, dialect
}
