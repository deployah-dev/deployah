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

package plan

import (
	"fmt"

	"github.com/gonvenience/ytbx"

	yamlv3 "go.yaml.in/yaml/v3"
)

// resourceKey identifies a Kubernetes resource across two manifests so
// resources can be matched independently of their position in the rendered
// YAML stream. Namespace is empty for cluster-scoped resources.
type resourceKey struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
}

// resourceDoc pairs a resourceKey with the parsed YAML document node so
// [diffResource] can run dyff on it.
type resourceDoc struct {
	key  resourceKey
	node *yamlv3.Node // Kind == yamlv3.DocumentNode
}

// resourceIdentity mirrors the fields dyff's own Kubernetes entity detector
// reads (apiVersion, kind, metadata.name, metadata.namespace), decoded as
// Go-typed fields rather than split back out of a slash-joined string --
// apiVersion values like "apps/v1" already contain a slash.
type resourceIdentity struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"metadata"`
}

// parseResources splits a multi-document Kubernetes manifest (as rendered by
// Helm, "---"-separated) into one [resourceDoc] per non-empty document, in
// manifest order. Documents missing apiVersion, kind, or metadata.name are
// rejected: a rendered chart is expected to only ever produce well-formed
// Kubernetes objects.
func parseResources(manifest string) ([]resourceDoc, error) {
	docs, err := ytbx.LoadYAMLDocuments([]byte(manifest))
	if err != nil {
		return nil, fmt.Errorf("parsing manifest YAML: %w", err)
	}

	out := make([]resourceDoc, 0, len(docs))
	for _, doc := range docs {
		if isEmptyDocument(doc) {
			continue
		}
		if doc.Kind != yamlv3.DocumentNode || len(doc.Content) != 1 {
			return nil, fmt.Errorf("unexpected YAML document shape in rendered manifest")
		}

		var id resourceIdentity
		if decodeErr := doc.Content[0].Decode(&id); decodeErr != nil {
			return nil, fmt.Errorf("decoding resource identity: %w", decodeErr)
		}
		if id.APIVersion == "" || id.Kind == "" || id.Metadata.Name == "" {
			return nil, fmt.Errorf("rendered resource missing apiVersion, kind, or metadata.name")
		}

		out = append(out, resourceDoc{
			key: resourceKey{
				APIVersion: id.APIVersion,
				Kind:       id.Kind,
				Namespace:  id.Metadata.Namespace,
				Name:       id.Metadata.Name,
			},
			node: doc,
		})
	}
	return out, nil
}

// CountResources reports how many Kubernetes resources a rendered manifest
// contains, using the same parsing [ComputeDiff] uses. `deployah plan
// --offline` has no prior release to diff against, so it reports this count
// instead of a per-resource change list.
func CountResources(manifest string) (int, error) {
	docs, err := parseResources(manifest)
	if err != nil {
		return 0, err
	}
	return len(docs), nil
}

// ResourceYAML is one Kubernetes resource extracted from a rendered
// manifest, re-encoded as a standalone single-document YAML string.
type ResourceYAML struct {
	// Label is "Kind/name" (or "Kind/namespace/name" for namespaced
	// resources), the same compact identifier [ComputeDiff] uses to key
	// resources internally.
	Label string
	// YAML is the resource's own manifest, on its own (no "---" separator
	// or sibling documents).
	YAML string
}

// SplitResources splits a rendered manifest into one [ResourceYAML] per
// contained Kubernetes resource, using the same parsing [ComputeDiff] uses.
// It backs drift detection (deployah.dev/deployah/internal/drift), which
// runs a server-side apply dry-run against each resource individually.
func SplitResources(manifest string) ([]ResourceYAML, error) {
	docs, err := parseResources(manifest)
	if err != nil {
		return nil, err
	}
	out := make([]ResourceYAML, 0, len(docs))
	for _, d := range docs {
		b, marshalErr := yamlv3.Marshal(d.node.Content[0])
		if marshalErr != nil {
			return nil, fmt.Errorf("re-encode resource %s: %w", d.key, marshalErr)
		}
		out = append(out, ResourceYAML{Label: d.key.String(), YAML: string(b)})
	}
	return out, nil
}

// isEmptyDocument reports whether node is a YAML document containing only a
// null scalar, which happens when a template renders to nothing (e.g. an
// "if" block that evaluates false) between two "---" separators.
func isEmptyDocument(node *yamlv3.Node) bool {
	if node.Kind != yamlv3.DocumentNode {
		return false
	}
	if len(node.Content) != 1 {
		return false
	}
	c := node.Content[0]
	return c.Kind == yamlv3.ScalarNode && c.Tag == "!!null"
}

// indexResources builds a lookup map from resourceKey to resourceDoc. It
// assumes keys are unique within docs, which holds for well-formed
// Kubernetes manifests (the API server itself rejects duplicate names).
func indexResources(docs []resourceDoc) map[resourceKey]resourceDoc {
	m := make(map[resourceKey]resourceDoc, len(docs))
	for _, d := range docs {
		m[d.key] = d
	}
	return m
}

// String renders a resourceKey as "Kind/name" (or "Kind/namespace/name" for
// namespaced resources), the compact resource label used throughout plan
// output.
func (k resourceKey) String() string {
	if k.Namespace == "" {
		return fmt.Sprintf("%s/%s", k.Kind, k.Name)
	}
	return fmt.Sprintf("%s/%s/%s", k.Kind, k.Namespace, k.Name)
}
