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

package view

import (
	"bytes"

	_ "embed"
)

//go:embed schema/semantic_plan.v1.json
var schemaV1 []byte

//go:embed schema/semantic_plan.v2.json
var schemaV2 []byte

// SchemaV1ID is the canonical $id of the historical Draft 2020-12 v1
// schema. [WriteJSON] no longer emits this identifier.
const SchemaV1ID = "https://deployah.dev/schemas/plan/v1/schema.json"

// SchemaV2ID is the canonical $id of the Draft 2020-12 schema for
// [WriteJSON] documents. WriteJSON stores it in the instance schema
// field.
const SchemaV2ID = "https://deployah.dev/schemas/plan/v2/schema.json"

// SchemaV1 returns the historical Draft 2020-12 JSON Schema for
// documents written before chartCRDs existed.
func SchemaV1() []byte {
	return bytes.Clone(schemaV1)
}

// SchemaV2 returns the Draft 2020-12 JSON Schema that describes documents
// written by [WriteJSON]. WriteJSON does not validate against this
// schema.
func SchemaV2() []byte {
	return bytes.Clone(schemaV2)
}
