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
	"slices"

	yamlv3 "go.yaml.in/yaml/v3"
)

// noiseAnnotations lists metadata.annotations keys that Helm/kubectl add
// but no Deployah chart template sets. They only appear when diffing
// against a live cluster object (--drift), not two rendered manifests, so
// stripping them keeps normalizeResource correct for both cases.
var noiseAnnotations = []string{
	"meta.helm.sh/release-name",
	"meta.helm.sh/release-namespace",
	"kubectl.kubernetes.io/last-applied-configuration",
}

// noiseMetadataFields lists metadata.* keys the API server populates that
// are never meaningful to a spec-driven change: they either change on
// every apply (resourceVersion, generation) or identify the object rather
// than its desired state (uid, creationTimestamp, managedFields).
var noiseMetadataFields = []string{
	"resourceVersion",
	"uid",
	"generation",
	"creationTimestamp",
	"managedFields",
}

// normalizeResource strips noise fields from a parsed Kubernetes resource
// document in place, so [diffResource] never reports a "change" caused by
// server-populated bookkeeping. node must be a document node as produced by
// [parseResources] (Kind == yamlv3.DocumentNode, Content[0] the mapping node).
func normalizeResource(node *yamlv3.Node) {
	if node == nil || node.Kind != yamlv3.DocumentNode || len(node.Content) != 1 {
		return
	}
	root := node.Content[0]
	if root.Kind != yamlv3.MappingNode {
		return
	}

	deleteMapKey(root, "status")

	metadata, metaOK := mapValue(root, "metadata")
	if !metaOK || metadata.Kind != yamlv3.MappingNode {
		return
	}

	for _, key := range noiseMetadataFields {
		deleteMapKey(metadata, key)
	}

	if annotations, annOK := mapValue(metadata, "annotations"); annOK && annotations.Kind == yamlv3.MappingNode {
		for _, key := range noiseAnnotations {
			deleteMapKey(annotations, key)
		}
		if len(annotations.Content) == 0 {
			deleteMapKey(metadata, "annotations")
		}
	}
}

// mapValue returns the value node for key in a YAML mapping node, following
// aliases on both the key and value the same way dyff's own comparator does.
func mapValue(mapping *yamlv3.Node, key string) (*yamlv3.Node, bool) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		k := followAlias(mapping.Content[i])
		if k.Value == key {
			return followAlias(mapping.Content[i+1]), true
		}
	}
	return nil, false
}

// deleteMapKey removes key (and its value) from a YAML mapping node's
// Content in place. It reports whether the key was found.
func deleteMapKey(mapping *yamlv3.Node, key string) bool {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = slices.Delete(mapping.Content, i, i+2)
			return true
		}
	}
	return false
}

// followAlias resolves a YAML alias node to the node it points to, mirroring
// dyff's own unexported helper of the same name since normalizeResource runs
// before resources reach dyff.
func followAlias(node *yamlv3.Node) *yamlv3.Node {
	if node != nil && node.Kind == yamlv3.AliasNode && node.Alias != nil {
		return followAlias(node.Alias)
	}
	return node
}
