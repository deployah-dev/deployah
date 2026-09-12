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
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

var rootKeyOrder = []string{"apiVersion", "kind", "metadata"}

func marshalOrderedYAML(obj map[string]any) (string, error) {
	if obj == nil {
		obj = map[string]any{}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(mappingNode(obj, rootKeyOrder)); err != nil {
		return "", fmt.Errorf("encode yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("close yaml encoder: %w", err)
	}
	return buf.String(), nil
}

func metadataKeyOrder(obj map[string]any) []string {
	var order []string
	if _, hasName := obj["name"]; hasName {
		order = append(order, "name")
	} else if _, hasGenerateName := obj["generateName"]; hasGenerateName {
		order = append(order, "generateName")
	}
	if _, hasNamespace := obj["namespace"]; hasNamespace {
		order = append(order, "namespace")
	}
	return order
}

func mappingNode(obj map[string]any, first []string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	seen := make(map[string]struct{}, len(first))
	for _, key := range first {
		if _, ok := obj[key]; !ok {
			continue
		}
		n.Content = append(n.Content, yamlKey(key), yamlValue(key, obj[key]))
		seen[key] = struct{}{}
	}
	rest := make([]string, 0, len(obj))
	for key := range obj {
		if _, ok := seen[key]; !ok {
			rest = append(rest, key)
		}
	}
	slices.Sort(rest)
	for _, key := range rest {
		n.Content = append(n.Content, yamlKey(key), yamlValue(key, obj[key]))
	}
	return n
}

func yamlValue(key string, v any) *yaml.Node {
	switch val := v.(type) {
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	case map[string]any:
		if key == "metadata" {
			return mappingNode(val, metadataKeyOrder(val))
		}
		return mappingNode(val, nil)
	case map[string]string:
		out := make(map[string]any, len(val))
		for k, x := range val {
			out[k] = x
		}
		if key == "metadata" {
			return mappingNode(out, metadataKeyOrder(out))
		}
		return mappingNode(out, nil)
	case []any:
		seq := &yaml.Node{Kind: yaml.SequenceNode}
		for _, item := range val {
			seq.Content = append(seq.Content, yamlValue("", item))
		}
		return seq
	case []string:
		seq := &yaml.Node{Kind: yaml.SequenceNode}
		for _, item := range val {
			seq.Content = append(seq.Content, yamlValue("", item))
		}
		return seq
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: val}
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(val)}
	case json.Number:
		return yamlNumber(string(val))
	case int:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(val)}
	case int32:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(int64(val), 10)}
	case int64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(val, 10)}
	case float32:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(float64(val), 'g', -1, 32)}
	case float64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(val, 'g', -1, 64)}
	default:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: fmt.Sprint(val)}
	}
}

func yamlNumber(s string) *yaml.Node {
	tag := "!!int"
	if strings.ContainsAny(s, ".eE") {
		tag = "!!float"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: s}
}

func yamlKey(k string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}
}
