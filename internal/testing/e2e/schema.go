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

package e2e

import (
	"bytes"
	"fmt"
	"sync"

	_ "embed"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed e2e.schema.json
var e2eSchemaJSON []byte

// E2ESchemaJSON returns a copy of the embedded Draft 2020-12 schema for
// scenarios/*/e2e.yaml. Editors still read the source file on disk.
func E2ESchemaJSON() []byte {
	return bytes.Clone(e2eSchemaJSON)
}

var compileE2ESchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(e2eSchemaJSON))
	if err != nil {
		return nil, fmt.Errorf("parse embedded e2e schema: %w", err)
	}
	const id = "e2e.schema.json"
	if err = compiler.AddResource(id, doc); err != nil {
		return nil, fmt.Errorf("load embedded e2e schema: %w", err)
	}
	compiled, err := compiler.Compile(id)
	if err != nil {
		return nil, fmt.Errorf("compile embedded e2e schema: %w", err)
	}
	return compiled, nil
})

// ValidateE2EDocument checks doc against the embedded e2e JSON Schema.
// doc is the generic JSON value, not a decoded fixture struct.
func ValidateE2EDocument(doc any) error {
	compiled, err := compileE2ESchema()
	if err != nil {
		return err
	}
	if err = compiled.Validate(doc); err != nil {
		return fmt.Errorf("e2e schema: %w", err)
	}
	return nil
}
