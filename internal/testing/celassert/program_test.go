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

package celassert_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"deployah.dev/deployah/internal/testing/celassert"

	k8sjson "k8s.io/apimachinery/pkg/util/json"
)

// plantedSecret is an unused input value. Error text must not contain it.
const plantedSecret = "super-secret-value"

// Fixed fixture UIDs. They are not read from a Kubernetes client.
const (
	migrateUID = "11111111-1111-1111-1111-111111111111"
	seedUID    = "22222222-2222-2222-2222-222222222222"
)

func TestCompile_RejectsInvalidExpressions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		expression string
		opts       []celassert.Option
		contains   string
	}{
		{
			name:       "empty",
			expression: "   ",
			contains:   "empty",
		},
		{
			name:       "syntax",
			expression: "object..status",
			opts:       []celassert.Option{celassert.WithVariable("object")},
			contains:   ":1:",
		},
		{
			name:       "unknown root",
			expression: "missing.size() == 0",
			opts:       []celassert.Option{celassert.WithVariable("plan")},
			contains:   "undeclared reference",
		},
		{
			name:       "static double equals int",
			expression: "1.0 == 1",
			contains:   "no matching overload",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := celassert.Compile(tt.expression, tt.opts...)
			require.Error(t, err)
			assert.NotErrorIs(t, err, celassert.ErrNotBool)
			assert.ErrorContains(t, err, tt.contains)
		})
	}
}

func TestCompile_NonBool(t *testing.T) {
	t.Parallel()

	_, err := celassert.Compile("1 + 1")
	require.ErrorIs(t, err, celassert.ErrNotBool)
}

func TestCompile_RejectsBadOptions(t *testing.T) {
	t.Parallel()

	duplicate := []celassert.Option{
		celassert.WithVariable("plan"),
		celassert.WithVariable("plan"),
	}
	tests := []struct {
		name     string
		opts     []celassert.Option
		contains string
	}{
		{
			name:     "zero cost",
			opts:     []celassert.Option{celassert.WithCostLimit(0)},
			contains: "cost limit",
		},
		{
			name:     "duplicate variable",
			opts:     duplicate,
			contains: "duplicate variable",
		},
		{
			name:     "empty variable",
			opts:     []celassert.Option{celassert.WithVariable("")},
			contains: "variable name is empty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := celassert.Compile("true", tt.opts...)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.contains)
		})
	}
}

func TestEval_UnstructuredValues(t *testing.T) {
	t.Parallel()

	pod := map[string]any{"apiVersion": "v1", "kind": "Pod"}
	obj := unstructured.Unstructured{Object: pod}
	list := unstructured.UnstructuredList{Items: []unstructured.Unstructured{obj}}
	objectExpr := `object.kind == "Pod"`
	itemsExpr := `items[0].kind == "Pod"`
	tests := []struct {
		name  string
		expr  string
		key   string
		value any
	}{
		{name: "value", expr: objectExpr, key: "object", value: obj},
		{name: "list", expr: itemsExpr, key: "items", value: list},
		{name: "list pointer", expr: itemsExpr, key: "items", value: &list},
		{name: "slice", expr: itemsExpr, key: "items", value: []unstructured.Unstructured{obj}},
		{
			name:  "pointer slice",
			expr:  itemsExpr,
			key:   "items",
			value: []*unstructured.Unstructured{&obj},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := mustCompile(t, tt.expr, celassert.WithVariable(tt.key))
			require.NoError(t, p.Eval(t.Context(), map[string]any{tt.key: tt.value}))
		})
	}
}

func TestEval_NilUnstructured(t *testing.T) {
	t.Parallel()

	p := mustCompile(t, `true`, celassert.WithVariable("object"))
	tests := []struct {
		name  string
		value any
	}{
		{name: "nil pointer", value: (*unstructured.Unstructured)(nil)},
		{name: "nil object", value: unstructured.Unstructured{}},
		{name: "nil list pointer", value: (*unstructured.UnstructuredList)(nil)},
		{
			name:  "nil item object",
			value: unstructured.UnstructuredList{Items: []unstructured.Unstructured{{}}},
		},
		{name: "nil pointer in slice", value: []*unstructured.Unstructured{nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := p.Eval(t.Context(), map[string]any{
				"object": tt.value,
				"note":   plantedSecret,
			})
			require.Error(t, err)
			assert.ErrorContains(t, err, "unstructured object is nil")
			assert.NotContains(t, err.Error(), plantedSecret)
		})
	}
}

func TestEval_TypedCollections(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		expr  string
		value interface{}
	}{
		{
			name:  "numbers",
			expr:  `n[0] == 7`,
			value: []json.Number{json.Number("7")},
		},
		{
			name: "maps",
			expr: `n[0].kind == "Pod"`,
			value: []map[string]interface{}{{
				"kind": "Pod",
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := mustCompile(t, tt.expr, celassert.WithVariable("n"))
			require.NoError(t, p.Eval(t.Context(), map[string]interface{}{"n": tt.value}))
		})
	}
}

func TestEval_EmptyPlan(t *testing.T) {
	t.Parallel()

	// Source: internal/plan/view/testdata/golden/json_empty.golden
	plan := decodeDocument(t, `{
	  "helmAction": "none",
	  "changes": [],
	  "drift": [],
	  "summary": {"create": 0, "update": 0, "delete": 0, "total": 0}
	}`)
	p := mustCompile(t,
		`plan.changes.size() == 0 && plan.drift.size() == 0 && plan.helmAction == "none"`,
		celassert.WithVariable("plan"),
	)
	vars := map[string]interface{}{"plan": plan}
	require.NoError(t, p.Eval(t.Context(), vars))
	require.NoError(t, p.Eval(t.Context(), vars))
}

func TestEval_NonEmptyPlan(t *testing.T) {
	t.Parallel()

	// Source: internal/plan/view/testdata/golden/json_update.golden
	plan := decodeDocument(t, `{
	  "helmAction": "upgrade",
	  "changes": [{
	    "resource": {"apiVersion": "v1", "kind": "ConfigMap", "namespace": "prod", "name": "app"},
	    "action": "update",
	    "fields": [
	      {"path": "/a", "op": "replace", "before": "1", "after": "2"},
	      {"path": "/z", "op": "replace", "before": "1", "after": "2"}
	    ]
	  }],
	  "drift": [],
	  "summary": {"create": 0, "update": 1, "delete": 0, "total": 1}
	}`)
	p := mustCompile(t,
		`plan.changes.size() == 1 && plan.changes[0].action == "update" && plan.summary.update == 1`,
		celassert.WithVariable("plan"),
	)
	require.NoError(t, p.Eval(t.Context(), map[string]interface{}{"plan": plan}))
}

func TestEval_DriftCounts(t *testing.T) {
	t.Parallel()

	// Shapes from sampleDriftPlan in internal/plan/view/schema_test.go.
	plan := decodeDocument(t, `{
	  "drift": [
	    {
	      "action": "modified",
	      "resource": {"apiVersion": "v1", "kind": "ConfigMap", "name": "app"},
	      "fields": [{"path": "/data/key", "op": "replace", "before": "old", "after": "new"}]
	    },
	    {
	      "action": "missing",
	      "resource": {"apiVersion": "v1", "kind": "ConfigMap", "name": "other"}
	    },
	    {
	      "action": "unexpected",
	      "resource": {"apiVersion": "v1", "kind": "ConfigMap", "name": "extra"}
	    }
	  ]
	}`)
	p := mustCompile(t, `
		plan.drift.filter(d, d.action == "modified").size() == 1
		&& plan.drift.filter(d, d.action == "missing").size() == 1
		&& plan.drift.filter(d, d.action == "unexpected").size() == 1
		&& plan.drift.filter(d, d.action == "modified" && d.resource.kind == "ConfigMap" && d.resource.name == "app").size() == 1
	`, celassert.WithVariable("plan"))
	require.NoError(t, p.Eval(t.Context(), map[string]interface{}{"plan": plan}))
}

func TestEval_AbsentAndNull(t *testing.T) {
	t.Parallel()

	// Source: scenarios/task-schedule/e2e.yaml
	present := decodeDocument(t, `
apiVersion: batch/v1
kind: CronJob
metadata:
  name: taskcron-dev-cleanup
  labels:
    deployah.dev/project: taskcron
    deployah.dev/component: cleanup
  annotations:
    helm.sh/hook: null
spec:
  schedule: "@every 1h"
  startingDeadlineSeconds: null
`)
	absent := decodeDocument(t, `
apiVersion: batch/v1
kind: CronJob
metadata:
  name: taskcron-dev-cleanup
spec:
  schedule: "@every 1h"
`)

	nullExpr := mustCompile(t, `
		has(object.spec.startingDeadlineSeconds)
		&& object.spec.startingDeadlineSeconds == null
		&& object.metadata.annotations["helm.sh/hook"] == null
	`, celassert.WithVariable("object"))
	require.NoError(t, nullExpr.Eval(t.Context(), map[string]interface{}{"object": present}))

	absentExpr := mustCompile(t, `
		!has(object.spec.startingDeadlineSeconds)
		&& !has(object.status)
		&& object.?status.orValue({}).size() == 0
	`, celassert.WithVariable("object"))
	require.NoError(t, absentExpr.Eval(t.Context(), map[string]interface{}{"object": absent}))
	err := absentExpr.Eval(t.Context(), map[string]interface{}{"object": present})
	require.ErrorIs(t, err, celassert.ErrFalse)

	readMissing := mustCompile(t,
		`object.spec.startingDeadlineSeconds == null`,
		celassert.WithVariable("object"),
	)
	err = readMissing.Eval(t.Context(), map[string]interface{}{"object": absent})
	require.Error(t, err)
	assert.NotErrorIs(t, err, celassert.ErrFalse)
	assert.ErrorContains(t, err, "no such key")
}

func TestEval_OmittedBinding(t *testing.T) {
	t.Parallel()

	p := mustCompile(t, `plan.changes.size() == 0`, celassert.WithVariable("plan"))
	err := p.Eval(t.Context(), map[string]interface{}{})
	require.Error(t, err)
	assert.NotErrorIs(t, err, celassert.ErrFalse)
}

func TestEval_NonBoolValue(t *testing.T) {
	t.Parallel()

	p := mustCompile(t, `object.metadata.name`, celassert.WithVariable("object"))
	err := p.Eval(t.Context(), map[string]interface{}{
		"object": map[string]interface{}{
			"metadata": map[string]interface{}{
				"name": "basic-web-service-dev",
				"note": plantedSecret,
			},
		},
	})
	require.ErrorIs(t, err, celassert.ErrNotBool)
	assert.ErrorContains(t, err, "string")
	assert.NotContains(t, err.Error(), plantedSecret)
}

func TestEval_JSONDocuments(t *testing.T) {
	t.Parallel()

	p := mustCompile(t, `object.kind == "Pod"`, celassert.WithVariable("object"))
	valid := []struct {
		name string
		raw  string
	}{
		{name: "object", raw: `{"kind":"Pod"}`},
		{name: "whitespace", raw: " \n\t{\"kind\":\"Pod\"}\n "},
	}
	for _, tt := range valid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := p.Eval(t.Context(), map[string]interface{}{
				"object": json.RawMessage(tt.raw),
			})
			require.NoError(t, err)
		})
	}

	items := mustCompile(t, `items.size() == 2`, celassert.WithVariable("items"))
	require.NoError(t, items.Eval(t.Context(), map[string]interface{}{
		"items": json.RawMessage(`[1, 2]`),
	}))

	invalid := []struct {
		name string
		raw  string
	}{
		{name: "two objects", raw: `{"kind":"Pod"}{"kind":"Service"}`},
		{name: "second value", raw: `{"kind":"Pod"} true`},
		{name: "trailing junk", raw: `{"kind":"Pod"} leftover`},
		{name: "incomplete", raw: `{`},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := p.Eval(t.Context(), map[string]interface{}{
				"object": json.RawMessage(tt.raw),
				"note":   plantedSecret,
			})
			require.Error(t, err)
			assert.ErrorContains(t, err, "decoding JSON input")
			assert.NotErrorIs(t, err, celassert.ErrFalse)
			assert.NotContains(t, err.Error(), plantedSecret)
			assert.NotContains(t, err.Error(), "Service")
		})
	}
}

func TestEval_DeploymentReadiness(t *testing.T) {
	t.Parallel()

	// Source: scenarios/basic-web-service/e2e.yaml
	obj := decodeDocument(t, deploymentYAML)
	p := mustCompile(t, deploymentExpr, celassert.WithVariable("object"))
	require.NoError(t, p.Eval(t.Context(), map[string]interface{}{"object": obj}))
	require.NoError(t, p.Eval(t.Context(), map[string]interface{}{
		"object": &unstructured.Unstructured{Object: obj},
	}))
}

func TestEval_PartialMatch(t *testing.T) {
	t.Parallel()

	obj := decodeDocument(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: basic-web-service-dev
spec:
  replicas: 1
  selector:
    matchLabels:
      app: web
  template:
    spec:
      containers:
        - name: web
          image: docker.io/library/nginx:latest
          ports:
            - name: http
              containerPort: 80
              protocol: TCP
            - name: metrics
              containerPort: 9090
status:
  readyReplicas: 1
`)
	p := mustCompile(t, deploymentExpr, celassert.WithVariable("object"))
	require.NoError(t, p.Eval(t.Context(), map[string]interface{}{"object": obj}))
}

func TestEval_ExtraObjects(t *testing.T) {
	t.Parallel()

	deployment := decodeDocument(t, deploymentYAML)
	service := decodeDocument(t, `
apiVersion: v1
kind: Service
metadata:
  name: basic-web-service-dev
spec:
  ports:
    - name: http
      port: 80
      targetPort: http
`)
	p := mustCompile(t,
		`resources.filter(r, r.kind == "Deployment" && r.status.readyReplicas == 1).size() == 1`,
		celassert.WithVariable("resources"),
	)
	require.NoError(t, p.Eval(t.Context(), map[string]interface{}{
		"resources": []interface{}{service, deployment},
	}))
}

func TestEval_JobConfigMapOwnership(t *testing.T) {
	t.Parallel()

	// Name link from internal/helm/generate_env_test.go.
	// Owner link from internal/e2e/e2e_wildcard_test.go.
	// UIDs are fixed literals, not client-generated values.
	migrateCM := "shop-dev-migrate-env"
	resources := []interface{}{
		job("shop-dev-migrate", migrateUID, migrateCM),
		configMap(migrateCM, "shop-dev-migrate", migrateUID),
		job("shop-dev-seed", seedUID, migrateCM),
	}
	p := mustCompile(t, ownershipExpr("shop-dev-migrate", migrateCM), celassert.WithVariable("resources"))
	require.NoError(t, p.Eval(t.Context(), map[string]interface{}{"resources": resources}))

	miss := mustCompile(t, ownershipExpr("shop-dev-seed", migrateCM), celassert.WithVariable("resources"))
	err := miss.Eval(t.Context(), map[string]interface{}{"resources": resources})
	require.ErrorIs(t, err, celassert.ErrFalse)
}

func TestEval_DynamicNumbers(t *testing.T) {
	t.Parallel()

	p := mustCompile(t, `n == 1`, celassert.WithVariable("n"))
	tests := []struct {
		name  string
		value interface{}
	}{
		{name: "int64", value: int64(1)},
		{name: "float64", value: float64(1)},
		{name: "json.Number", value: json.Number("1")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, p.Eval(t.Context(), map[string]interface{}{"n": tt.value}))
		})
	}
}

func TestEval_JSONNumberExact(t *testing.T) {
	t.Parallel()

	// 9007199254740993 is 2^53+1. float64 rounds it to 2^53.
	rounded, err := json.Number("9007199254740993e0").Float64()
	require.NoError(t, err)
	boundary, err := json.Number("9007199254740992").Float64()
	require.NoError(t, err)
	require.Equal(t, boundary, rounded)

	tests := []struct {
		name  string
		expr  string
		value interface{}
	}{
		{
			name:  "within int64",
			expr:  `n == 7`,
			value: json.Number("7"),
		},
		{
			name:  "beyond 2^53",
			expr:  `n == 9007199254740992 && n != 9007199254740993`,
			value: json.Number("9007199254740992"),
		},
		{
			name:  "scientific beyond 2^53",
			expr:  `n == 9007199254740993 && n != 9007199254740992`,
			value: json.Number("9007199254740993e0"),
		},
		{
			name:  "1e2",
			expr:  `n == 100`,
			value: json.Number("1e2"),
		},
		{
			name:  "1.5e2",
			expr:  `n == 150`,
			value: json.Number("1.5e2"),
		},
		{
			name:  "fraction",
			expr:  `n == 1.5`,
			value: json.Number("1.5"),
		},
		{
			name: "nested scientific",
			expr: `n.nums[0] == 9007199254740993`,
			value: map[string]interface{}{
				"nums": []interface{}{json.Number("9007199254740993e0")},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := mustCompile(t, tt.expr, celassert.WithVariable("n"))
			require.NoError(t, p.Eval(t.Context(), map[string]interface{}{"n": tt.value}))
		})
	}
}

func TestEval_JSONNumberOutsideInt64(t *testing.T) {
	t.Parallel()

	// These two integers are distinct, but float64 rounds both to 2^63.
	low, lowErr := json.Number("9223372036854775809e0").Float64()
	require.NoError(t, lowErr)
	high, highErr := json.Number("9223372036854775810e0").Float64()
	require.NoError(t, highErr)
	require.Equal(t, low, high)

	p := mustCompile(t, `true`, celassert.WithVariable("n"))
	tests := []struct {
		name  string
		value interface{}
	}{
		{
			name:  "number",
			value: json.Number("9223372036854775809"),
		},
		{
			name:  "other number",
			value: json.Number("9223372036854775810"),
		},
		{
			name:  "scientific",
			value: json.Number("9223372036854775809e0"),
		},
		{
			name:  "other scientific",
			value: json.Number("9223372036854775810e0"),
		},
		{
			name: "map",
			value: map[string]interface{}{
				"value": json.Number("9223372036854775809e0"),
				"note":  plantedSecret,
			},
		},
		{
			name: "array",
			value: []interface{}{
				map[string]interface{}{
					"n":    json.Number("9223372036854775810e0"),
					"note": plantedSecret,
				},
			},
		},
		{
			name:  "slice of numbers",
			value: []json.Number{json.Number("9223372036854775810")},
		},
		{
			name: "slice of maps",
			value: []map[string]interface{}{{
				"n":    json.Number("9223372036854775809"),
				"note": plantedSecret,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := p.Eval(t.Context(), map[string]interface{}{"n": tt.value})
			require.ErrorIs(t, err, celassert.ErrIntegerRange)
			assert.NotContains(t, err.Error(), plantedSecret)
		})
	}

	fraction := mustCompile(t, `n == 1.5`, celassert.WithVariable("n"))
	require.NoError(t, fraction.Eval(t.Context(), map[string]interface{}{
		"n": json.Number("1.5"),
	}))
}

func TestEval_CostLimit(t *testing.T) {
	t.Parallel()

	items := make([]interface{}, 0, 20)
	for i := range 20 {
		items = append(items, i)
	}
	p := mustCompile(t,
		`items.all(i, items.all(j, i == j || i != j))`,
		celassert.WithVariable("items"),
		celassert.WithCostLimit(1),
	)
	err := p.Eval(t.Context(), map[string]interface{}{"items": items})
	require.Error(t, err)
	assert.ErrorContains(t, err, "actual cost limit exceeded")
	assert.NotErrorIs(t, err, celassert.ErrFalse)
	assert.NotErrorIs(t, err, context.Canceled)

	ok := mustCompile(t, `true`)
	require.NoError(t, ok.Eval(t.Context(), nil))
}

func TestEval_ContextAlreadyDone(t *testing.T) {
	t.Parallel()

	p := mustCompile(t, `true`)
	tests := []struct {
		name   string
		newCtx func() (context.Context, context.CancelFunc)
		want   error
	}{
		{
			name: "canceled",
			newCtx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			want: context.Canceled,
		},
		{
			name: "deadline exceeded",
			newCtx: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Unix(0, 0))
			},
			want: context.DeadlineExceeded,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := tt.newCtx()
			defer cancel()
			err := p.Eval(ctx, map[string]interface{}{"unused": plantedSecret})
			require.ErrorIs(t, err, tt.want)
			assert.NotErrorIs(t, err, celassert.ErrFalse)
			assert.NotContains(t, err.Error(), plantedSecret)
			assert.Contains(t, err.Error(), "true")
		})
	}

	require.NoError(t, p.Eval(t.Context(), map[string]interface{}{"unused": plantedSecret}))
}

func TestEval_NilContext(t *testing.T) {
	t.Parallel()

	p := mustCompile(t, `true`)
	//nolint:staticcheck // nil context is the case under test
	err := p.Eval(nil, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "context is nil")
	assert.NotErrorIs(t, err, context.Canceled)
}

func TestEval_FalseDoesNotDumpInput(t *testing.T) {
	t.Parallel()

	obj := decodeDocument(t, deploymentYAML)
	meta, ok := obj["metadata"].(map[string]interface{})
	require.True(t, ok)
	meta["annotations"] = map[string]interface{}{
		"secret": plantedSecret,
	}
	expression := `object.status.readyReplicas == 2`
	p := mustCompile(t, expression, celassert.WithVariable("object"))
	err := p.Eval(t.Context(), map[string]interface{}{"object": obj})
	require.ErrorIs(t, err, celassert.ErrFalse)
	assert.ErrorContains(t, err, expression)
	assert.NotContains(t, err.Error(), plantedSecret)
}

func TestEval_MissingFieldDoesNotDumpInput(t *testing.T) {
	t.Parallel()

	obj := decodeDocument(t, deploymentYAML)
	status, ok := obj["status"].(map[string]interface{})
	require.True(t, ok)
	delete(status, "readyReplicas")
	meta, ok := obj["metadata"].(map[string]interface{})
	require.True(t, ok)
	meta["annotations"] = map[string]interface{}{
		"secret": plantedSecret,
	}
	p := mustCompile(t, `object.status.readyReplicas == 1`, celassert.WithVariable("object"))
	err := p.Eval(t.Context(), map[string]interface{}{"object": obj})
	require.Error(t, err)
	assert.NotErrorIs(t, err, celassert.ErrFalse)
	assert.ErrorContains(t, err, "no such key")
	assert.NotContains(t, err.Error(), plantedSecret)
}

func TestEval_ReusesProgram(t *testing.T) {
	t.Parallel()

	p := mustCompile(t, `object.status.readyReplicas == 1`, celassert.WithVariable("object"))
	ready := map[string]interface{}{
		"object": map[string]interface{}{
			"status": map[string]interface{}{"readyReplicas": 1},
		},
	}
	notReady := map[string]interface{}{
		"object": map[string]interface{}{
			"status": map[string]interface{}{"readyReplicas": 2},
		},
	}
	require.NoError(t, p.Eval(t.Context(), ready))
	require.ErrorIs(t, p.Eval(t.Context(), notReady), celassert.ErrFalse)
}

func TestEval_UnsupportedValue(t *testing.T) {
	t.Parallel()

	p := mustCompile(t, `bad == null`, celassert.WithVariable("bad"))
	err := p.Eval(t.Context(), map[string]interface{}{"bad": func() {}})
	require.Error(t, err)
	assert.NotErrorIs(t, err, celassert.ErrFalse)
}

func mustCompile(t *testing.T, expression string, opts ...celassert.Option) *celassert.Program {
	t.Helper()
	p, err := celassert.Compile(expression, opts...)
	require.NoError(t, err)
	return p
}

func decodeDocument(t *testing.T, src string) map[string]interface{} {
	t.Helper()
	raw, err := yaml.YAMLToJSON([]byte(src))
	require.NoError(t, err)
	var out map[string]interface{}
	require.NoError(t, k8sjson.Unmarshal(raw, &out))
	return out
}

func job(name, uid, cmName string) map[string]interface{} {
	return map[string]interface{}{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]interface{}{
			"name": name,
			"uid":  uid,
		},
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name": "migrate",
							"envFrom": []interface{}{
								map[string]interface{}{
									"configMapRef": map[string]interface{}{
										"name": cmName,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func configMap(name, ownerName, ownerUID string) map[string]interface{} {
	return map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name": name,
			"ownerReferences": []interface{}{
				map[string]interface{}{
					"apiVersion": "batch/v1",
					"kind":       "Job",
					"name":       ownerName,
					"uid":        ownerUID,
				},
			},
		},
	}
}

func ownershipExpr(jobName, cmName string) string {
	return fmt.Sprintf(
		`resources.filter(r, r.kind == "Job" && r.metadata.name == %q).exists(j, `+
			`resources.filter(c, c.kind == "ConfigMap" && c.metadata.name == %q).exists(cm, `+
			`j.spec.template.spec.containers[0].envFrom[0].configMapRef.name == cm.metadata.name `+
			`&& cm.metadata.ownerReferences[0].name == j.metadata.name `+
			`&& cm.metadata.ownerReferences[0].uid == j.metadata.uid))`,
		jobName, cmName,
	)
}

const deploymentExpr = `object.kind == "Deployment"
&& object.metadata.name == "basic-web-service-dev"
&& object.spec.replicas == 1
&& object.spec.template.spec.containers.exists(c, c.name == "web" && c.image == "docker.io/library/nginx:latest")
&& object.status.readyReplicas == 1`

const deploymentYAML = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: basic-web-service-dev
  labels:
    deployah.dev/project: basic-web-service
    deployah.dev/environment: dev
    deployah.dev/component: web
spec:
  replicas: 1
  template:
    spec:
      containers:
        - name: web
          image: docker.io/library/nginx:latest
          ports:
            - name: http
              containerPort: 80
status:
  readyReplicas: 1
`
