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

package plan

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/wI2L/jsondiff"

	v1 "helm.sh/helm/v4/pkg/release/v1"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	sigsyaml "sigs.k8s.io/yaml"
)

// releaseIntentChanged reports whether the Desired manifest or hooks differ
// from Previous. Order matters. Bad input (a nil hook, invalid YAML, a
// duplicate key) is an error even when the manifests already differ.
func releaseIntentChanged(previousManifest string, previousHooks []*v1.Hook, desiredManifest string, desiredHooks []*v1.Hook) (bool, error) {
	previousDocs, err := manifestJSON(previousManifest)
	if err != nil {
		return false, fmt.Errorf("previous manifest: %w", err)
	}
	desiredDocs, err := manifestJSON(desiredManifest)
	if err != nil {
		return false, fmt.Errorf("desired manifest: %w", err)
	}
	previousHookDocs, err := hooksJSON("previous", previousHooks)
	if err != nil {
		return false, err
	}
	desiredHookDocs, err := hooksJSON("desired", desiredHooks)
	if err != nil {
		return false, err
	}

	same, err := jsonStructurallyEqual(previousDocs, desiredDocs)
	if err != nil {
		return false, fmt.Errorf("compare manifests: %w", err)
	}
	if !same {
		return true, nil
	}
	same, err = jsonStructurallyEqual(previousHookDocs, desiredHookDocs)
	if err != nil {
		return false, fmt.Errorf("compare hooks: %w", err)
	}
	return !same, nil
}

func manifestJSON(manifest string) ([]json.RawMessage, error) {
	reader := yamlutil.NewYAMLReader(bufio.NewReader(strings.NewReader(manifest)))
	var docs []json.RawMessage
	docNum := 0
	for {
		body, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("document %d: %w", docNum+1, err)
		}
		docNum++
		raw, convErr := sigsyaml.YAMLToJSONStrict(body)
		if convErr != nil {
			return nil, fmt.Errorf("document %d: %w", docNum, convErr)
		}
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			continue
		}
		docs = append(docs, json.RawMessage(raw))
	}
	return docs, nil
}

func hooksJSON(side string, hooks []*v1.Hook) ([]json.RawMessage, error) {
	var docs []json.RawMessage
	for i, h := range hooks {
		if h == nil {
			return nil, fmt.Errorf("%s hook %d is nil", side, i+1)
		}
		hookDocs, err := manifestJSON(h.Manifest)
		if err != nil {
			return nil, fmt.Errorf("%s hook %s: %w", side, hookLabel(h, i), err)
		}
		docs = append(docs, hookDocs...)
	}
	return docs, nil
}

func hookLabel(h *v1.Hook, index int) string {
	if name := strings.TrimSpace(h.Name); name != "" {
		return name
	}
	return strconv.Itoa(index + 1)
}

func jsonStructurallyEqual(previous, desired []json.RawMessage) (bool, error) {
	prev, err := json.Marshal(previous)
	if err != nil {
		return false, err
	}
	next, err := json.Marshal(desired)
	if err != nil {
		return false, err
	}
	patch, err := jsondiff.CompareJSON(prev, next, jsondiff.UnmarshalFunc(unmarshalUseNumber))
	if err != nil {
		return false, err
	}
	return len(patch) == 0, nil
}

func unmarshalUseNumber(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(v)
}
