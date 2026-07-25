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

package extras

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

// PostRenderer appends literal extra manifests to Helm's rendered stream.
// Extra YAML never passes through the template engine, so `{{ }}` is preserved.
type PostRenderer struct {
	Manifests []Object
}

// Run implements helm.sh/helm/v4/pkg/postrenderer.PostRenderer.
func (p *PostRenderer) Run(renderedManifests *bytes.Buffer) (*bytes.Buffer, error) {
	if p == nil || len(p.Manifests) == 0 {
		return renderedManifests, nil
	}

	generated, err := parseIdentities(renderedManifests.Bytes())
	if err != nil {
		return nil, fmt.Errorf("parse rendered manifests: %w", err)
	}

	for i := range p.Manifests {
		id := p.Manifests[i].Identity()
		if path, ok := generated[id.Key()]; ok {
			return nil, fmt.Errorf("extra manifest %s collides with generated object %s (from %s)", p.Manifests[i].Path, id, path)
		}
		generated[id.Key()] = p.Manifests[i].Path
	}

	out := new(bytes.Buffer)
	if renderedManifests.Len() > 0 {
		out.Write(renderedManifests.Bytes())
		if !bytes.HasSuffix(renderedManifests.Bytes(), []byte("\n")) {
			out.WriteByte('\n')
		}
	}
	for i := range p.Manifests {
		raw := bytes.TrimSpace(p.Manifests[i].Raw)
		if len(raw) == 0 {
			continue
		}
		if out.Len() > 0 {
			out.WriteString("---\n")
		}
		out.Write(raw)
		if !bytes.HasSuffix(raw, []byte("\n")) {
			out.WriteByte('\n')
		}
	}
	return out, nil
}

func parseIdentities(data []byte) (map[string]string, error) {
	out := make(map[string]string)
	if len(bytes.TrimSpace(data)) == 0 {
		return out, nil
	}
	decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	idx := 0
	for {
		var obj unstructured.Unstructured
		if err := decoder.Decode(&obj); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		idx++
		if len(obj.Object) == 0 {
			continue
		}
		id := Identity{
			APIVersion: obj.GetAPIVersion(),
			Kind:       obj.GetKind(),
			Namespace:  obj.GetNamespace(),
			Name:       obj.GetName(),
		}
		if id.APIVersion == "" || id.Kind == "" || id.Name == "" {
			continue
		}
		out[id.Key()] = fmt.Sprintf("rendered#%d", idx)
	}
	return out, nil
}
