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

package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// StringMap is a string-to-string map. When unmarshaling YAML or JSON,
// string, number, and boolean values are stored as strings so Kubernetes
// and envsubst can use them.
type StringMap map[string]string

// UnmarshalJSON unmarshals a JSON object into a StringMap.
func (m *StringMap) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*m = nil
		return nil
	}
	// Accept string, number, or boolean; UseNumber keeps large ints exact.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return fmt.Errorf("string map: %w", err)
	}
	out := make(StringMap, len(raw))
	for k, v := range raw {
		s, err := stringifyScalar(v)
		if err != nil {
			return fmt.Errorf("key %q: %w", k, err)
		}
		out[k] = s
	}
	*m = out
	return nil
}

func stringifyScalar(v any) (string, error) {
	switch val := v.(type) {
	case nil:
		return "", nil
	case string:
		return val, nil
	case bool:
		return strconv.FormatBool(val), nil
	case float64:
		if !math.IsInf(val, 0) && !math.IsNaN(val) && val == math.Trunc(val) {
			return strconv.FormatInt(int64(val), 10), nil
		}
		return strconv.FormatFloat(val, 'f', -1, 64), nil
	case json.Number:
		return val.String(), nil
	default:
		return "", fmt.Errorf("expected string, number, or boolean, got %T", v)
	}
}
