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

package extras

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"

	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	sigsyaml "sigs.k8s.io/yaml"
)

const crdKind = "CustomResourceDefinition"

// crdPresentation is the only YAML shape the CRD source contract reads.
// Spec, status, namespace, and other fields are ignored on purpose.
type crdPresentation struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name string `json:"name"`
	} `json:"metadata"`
}

// parseCRDDocuments extracts presentation identity from each non-empty YAML
// document in raw. It does not rewrite raw; callers keep those bytes for Helm.
func parseCRDDocuments(path string, raw []byte) ([]CRDDoc, error) {
	reader := yamlutil.NewYAMLReader(bufio.NewReader(bytes.NewReader(raw)))
	var docs []CRDDoc
	docNum := 0
	for {
		body, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: parse YAML: %w", path, err)
		}
		docNum++
		if len(bytes.TrimSpace(body)) == 0 {
			continue
		}
		var ident crdPresentation
		if unmarshalErr := sigsyaml.Unmarshal(body, &ident); unmarshalErr != nil {
			return nil, fmt.Errorf("%s: parse YAML: %w", path, unmarshalErr)
		}
		if ident.Kind == "" {
			return nil, fmt.Errorf("%s: document %d missing required kind", path, docNum)
		}
		if ident.Kind != crdKind {
			return nil, fmt.Errorf("%s: document %d: only CustomResourceDefinition documents belong in .deployah/crds/; found %s", path, docNum, ident.Kind)
		}
		if ident.Metadata.Name == "" {
			return nil, fmt.Errorf("%s: document %d missing required metadata.name", path, docNum)
		}
		docs = append(docs, CRDDoc{
			Path:       path,
			Index:      len(docs),
			Kind:       ident.Kind,
			Name:       ident.Metadata.Name,
			APIVersion: ident.APIVersion,
			YAML:       bytes.Clone(body),
		})
	}
	return docs, nil
}
