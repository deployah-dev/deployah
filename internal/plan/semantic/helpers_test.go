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

package semantic_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/plan/semantic"
)

func objectString(tb testing.TB, obj map[string]any, keys ...string) string {
	tb.Helper()
	var cur any = obj
	for _, key := range keys {
		m, ok := cur.(map[string]any)
		require.True(tb, ok)
		cur, ok = m[key]
		require.True(tb, ok)
	}
	s, ok := cur.(string)
	require.True(tb, ok)
	return s
}

func setObjectString(tb testing.TB, obj map[string]any, value string, keys ...string) {
	tb.Helper()
	require.NotEmpty(tb, keys)
	cur := obj
	for _, key := range keys[:len(keys)-1] {
		next, ok := cur[key].(map[string]any)
		require.True(tb, ok)
		cur = next
	}
	cur[keys[len(keys)-1]] = value
}

func cm(name, value string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      name,
			"namespace": "prod",
		},
		"data": map[string]any{"key": value},
	}
}

func ref(kind, name string) semantic.ResourceRef {
	return semantic.ResourceRef{
		APIVersion: "v1",
		Kind:       kind,
		Namespace:  "prod",
		Name:       name,
	}
}

func mustFields(before, after map[string]any) []semantic.FieldChange {
	fields, err := semantic.DiffFields(before, after)
	if err != nil {
		panic(err)
	}
	return fields
}

func createChange(name, value string) semantic.ResourceChange {
	return semantic.ResourceChange{
		Resource: ref("ConfigMap", name),
		Action:   semantic.Create,
		After:    &semantic.ResourceSnapshot{Object: cm(name, value)},
	}
}

func updateChange(name, before, after string) semantic.ResourceChange {
	beforeObj := cm(name, before)
	afterObj := cm(name, after)
	return semantic.ResourceChange{
		Resource: ref("ConfigMap", name),
		Action:   semantic.Update,
		Before:   &semantic.ResourceSnapshot{Object: beforeObj},
		After:    &semantic.ResourceSnapshot{Object: afterObj},
		Fields:   mustFields(beforeObj, afterObj),
	}
}

func deleteChange(name, value string) semantic.ResourceChange {
	return semantic.ResourceChange{
		Resource: ref("ConfigMap", name),
		Action:   semantic.Delete,
		Before:   &semantic.ResourceSnapshot{Object: cm(name, value)},
	}
}
