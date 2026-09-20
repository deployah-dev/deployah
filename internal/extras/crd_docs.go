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

	yamlv3 "go.yaml.in/yaml/v3"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	sigsyaml "sigs.k8s.io/yaml"
)

const crdKind = "CustomResourceDefinition"

// crdPresentation is the only YAML shape the CRD source contract reads.
// Spec, status, namespace, apiVersion, and other fields are ignored.
type crdPresentation struct {
	Kind     string `json:"kind"`
	Metadata struct {
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
		empty, emptyErr := yamlDocumentEmpty(body)
		if emptyErr != nil {
			return nil, fmt.Errorf("%s: parse YAML: %w", path, emptyErr)
		}
		if empty {
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
			Path:  path,
			Index: len(docs),
			Kind:  ident.Kind,
			Name:  ident.Metadata.Name,
			YAML:  bytes.Clone(body),
		})
	}
	return docs, nil
}

// yamlDocumentEmpty reports whether body has no YAML value: comments,
// whitespace, a bare document separator, or a !!null scalar with an
// empty value. The tokens null and ~, {}, and other values are not
// empty.
func yamlDocumentEmpty(body []byte) (bool, error) {
	var node yamlv3.Node
	if err := yamlv3.Unmarshal(body, &node); err != nil {
		return false, err
	}
	return yamlValueEmpty(&node), nil
}

func yamlValueEmpty(n *yamlv3.Node) bool {
	if n == nil {
		return true
	}
	switch n.Kind {
	case 0:
		return true
	case yamlv3.DocumentNode:
		if len(n.Content) == 0 {
			return true
		}
		for _, c := range n.Content {
			if !yamlValueEmpty(c) {
				return false
			}
		}
		return true
	case yamlv3.ScalarNode:
		return n.Tag == "!!null" && n.Value == ""
	default:
		return false
	}
}
