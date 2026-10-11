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

//go:build e2e

package e2e_test

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	inttest "deployah.dev/deployah/internal/testing"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	netutil "k8s.io/apimachinery/pkg/util/net"
)

const (
	resourcePollInterval = 2 * time.Second
	namespaceWaitTimeout = 2 * time.Minute
	e2eSchemaModeLine    = "# $schema: ../../internal/testing/e2e.schema.json"
)

// isRetryableAPIError reports whether resource polling should retry err.
// It returns true for NotFound, conflict, timeout, and transport failures.
// REST mapping misses, forbidden requests, and canceled contexts fail
// immediately.
func isRetryableAPIError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	switch {
	case apierrors.IsNotFound(err),
		apierrors.IsConflict(err),
		apierrors.IsServerTimeout(err),
		apierrors.IsTooManyRequests(err),
		apierrors.IsServiceUnavailable(err),
		apierrors.IsTimeout(err),
		apierrors.IsInternalError(err):
		return true
	}
	return netutil.IsProbableEOF(err) ||
		netutil.IsConnectionReset(err) ||
		netutil.IsConnectionRefused(err) ||
		netutil.IsTimeout(err)
}

func (s *E2ESuite) scaffoldSimple(t *testing.T, dir, ns string, fx inttest.E2EFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	kinds := []schema.GroupVersionKind{
		{Group: "apps", Version: "v1", Kind: "Deployment"},
		{Group: "apps", Version: "v1", Kind: "StatefulSet"},
		{Group: "", Version: "v1", Kind: "Service"},
		{Group: "", Version: "v1", Kind: "PersistentVolumeClaim"},
		{Group: "batch", Version: "v1", Kind: "Job"},
		{Group: "batch", Version: "v1", Kind: "CronJob"},
	}
	var assertions []map[string]any
	for _, gvk := range kinds {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group: gvk.Group, Version: gvk.Version, Kind: gvk.Kind + "List",
		})
		if listErr := s.listKindInNamespace(ctx, ns, list); listErr != nil {
			t.Logf("scaffold list %s: %v", gvk.Kind, listErr)
			continue
		}
		for i := range list.Items {
			assertions = append(assertions, scaffoldAssertion(list.Items[i].Object))
		}
	}
	doc := map[string]any{
		"env": fx.Env,
		"steps": []any{
			map[string]any{"deploy": map[string]any{}},
			map[string]any{"assert": assertions},
		},
	}
	body, err := yaml.Marshal(doc)
	require.NoError(t, err)
	out := e2eSchemaModeLine + "\n" + string(body)

	path := filepath.Join(dir, inttest.E2EFixtureFile)
	if _, statErr := os.Stat(path); statErr == nil {
		t.Logf("-e2e.scaffold: %s already exists; skeleton follows\n%s", path, out)
		return
	}
	require.NoError(t, os.WriteFile(path, []byte(out), 0o600))
	t.Logf("wrote scaffold %s", path)
}

func (s *E2ESuite) listKindInNamespace(ctx context.Context, ns string, list *unstructured.UnstructuredList) error {
	res, err := s.newResources(ns)
	if err != nil {
		return err
	}
	return res.List(ctx, list)
}

func scaffoldAssertion(obj map[string]any) map[string]any {
	kind := stringField(obj, "kind")
	sel := map[string]any{
		"apiVersion": obj["apiVersion"],
		"kind":       kind,
	}
	expect := map[string]any{}
	if metaMap := mapField(obj, "metadata"); metaMap != nil {
		if name, hasName := metaMap["name"].(string); hasName && name != "" {
			sel["name"] = name
		}
		if rawLabels := mapField(metaMap, "labels"); rawLabels != nil {
			cleaned := maps.Clone(rawLabels)
			delete(cleaned, "helm.sh/chart")
			if len(cleaned) > 0 {
				sel["labels"] = cleaned
			}
		}
	}
	if specMap := mapField(obj, "spec"); specMap != nil {
		expect["spec"] = stripGeneratedSpec(deepCopyMap(specMap))
	}
	if st := mapField(obj, "status"); st != nil {
		ready := map[string]any{}
		switch kind {
		case "Deployment", "StatefulSet":
			if v, hasReady := st["readyReplicas"]; hasReady {
				ready["readyReplicas"] = v
			}
		case "Job":
			if v, hasSucceeded := st["succeeded"]; hasSucceeded {
				ready["succeeded"] = v
			}
		case "PersistentVolumeClaim":
			if v, hasPhase := st["phase"]; hasPhase {
				ready["phase"] = v
			}
		}
		if len(ready) > 0 {
			expect["status"] = ready
		}
	}
	item := map[string]any{"select": sel}
	if len(expect) > 0 {
		item["expect"] = expect
	}
	return item
}

func stripGeneratedSpec(spec map[string]any) map[string]any {
	delete(spec, "clusterIP")
	delete(spec, "clusterIPs")
	ports, isPorts := spec["ports"].([]any)
	if !isPorts {
		return spec
	}
	for i := range ports {
		pm, isMap := ports[i].(map[string]any)
		if !isMap {
			continue
		}
		delete(pm, "nodePort")
	}
	return spec
}

func deepCopyMap(in map[string]any) map[string]any {
	out := maps.Clone(in)
	for k, v := range out {
		switch n := v.(type) {
		case map[string]any:
			out[k] = deepCopyMap(n)
		case []any:
			cp := make([]any, 0, len(n))
			for _, item := range n {
				m, isMap := item.(map[string]any)
				if isMap {
					cp = append(cp, deepCopyMap(m))
					continue
				}
				cp = append(cp, item)
			}
			out[k] = cp
		}
	}
	return out
}

func stringField(m map[string]any, key string) string {
	s, ok := m[key].(string)
	if !ok {
		return ""
	}
	return s
}

func mapField(m map[string]any, key string) map[string]any {
	child, ok := m[key].(map[string]any)
	if !ok {
		return nil
	}
	return child
}
