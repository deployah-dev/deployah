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

//go:build e2e

package e2e_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/testing/e2e"

	planCmd "deployah.dev/deployah/internal/cmd/plan"
	inttest "deployah.dev/deployah/internal/testing"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func runPlanCLI(t *testing.T, env *e2eEnv) {
	t.Helper()
	src := filepath.Join(env.scenariosDir, "basic-web-service")
	require.DirExists(t, src)
	dir, err := inttest.NewScenarioWorkspace(t, src)
	require.NoError(t, err)

	ns := fixtureNamespace("plan-cli")
	var claim e2e.Claim
	t.Cleanup(func() {
		if claim.UID == "" {
			return
		}
		if cleanupErr := env.cleanupClaim(t, dir, claim, []releaseTarget{{
			Project: "basic-web-service",
			Env:     "dev",
		}}); cleanupErr != nil {
			t.Errorf("cleanup: %v", cleanupErr)
		}
	})

	stdout, _, err := runInErrContext(t, t.Context(), dir, "plan", "dev", "-o", "json",
		"--context", kindContext, "--namespace", ns)
	require.NoError(t, err)
	var fresh map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &fresh))
	assert.Empty(t, fresh["drift"])
	cs, _ := env.kubeClients(t)
	_, err = cs.CoreV1().Namespaces().Get(t.Context(), ns, metav1.GetOptions{})
	require.True(t, apierrors.IsNotFound(err), "fresh plan must not create namespace %s: %v", ns, err)

	got, err := env.claimNamespace(t.Context(), ns)
	require.NoError(t, err)
	claim = got
	runIn(t, dir, "deploy", "dev", "--context", kindContext, "--namespace", ns)

	stdout, _, err = runInErrContext(t, t.Context(), dir, "plan", "dev", "-o", "json",
		"--context", kindContext, "--namespace", ns)
	require.NoError(t, err)
	var same map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &same))
	assert.Empty(t, same["changes"])
	assert.Empty(t, same["drift"])

	_, _, err = runInErrContext(t, t.Context(), dir, "plan", "dev", "--detailed-exitcode",
		"--context", kindContext, "--namespace", ns)
	require.NoError(t, err)

	dep, err := cs.AppsV1().Deployments(ns).Get(t.Context(), "basic-web-service-dev", metav1.GetOptions{})
	require.NoError(t, err)
	replicas := int32(3)
	dep.Spec.Replicas = &replicas
	_, err = cs.AppsV1().Deployments(ns).Update(t.Context(), dep, metav1.UpdateOptions{})
	require.NoError(t, err)

	stdout, _, err = runInErrContext(t, t.Context(), dir, "plan", "dev", "-o", "json",
		"--context", kindContext, "--namespace", ns)
	require.NoError(t, err)
	var drifted map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &drifted))
	drift, ok := drifted["drift"].([]any)
	require.True(t, ok)
	modified := 0
	for _, item := range drift {
		entry, isMap := item.(map[string]any)
		require.True(t, isMap)
		if entry["action"] == "modified" {
			modified++
		}
	}
	assert.Equal(t, 1, modified)

	_, _, err = runInErrContext(t, t.Context(), dir, "plan", "dev", "--detailed-exitcode",
		"--context", kindContext, "--namespace", ns)
	require.NoError(t, err, "drift alone must not exit 2")

	human, _, err := runInErrContext(t, t.Context(), dir, "plan", "dev",
		"--context", kindContext, "--namespace", ns)
	require.NoError(t, err)
	assert.Contains(t, human, "Drift")

	specPath := filepath.Join(dir, "deployah.yaml")
	raw, err := os.ReadFile(specPath) // #nosec G304 -- path under test-controlled temp dir
	require.NoError(t, err)
	old := []byte("port: 80\n")
	require.Equal(t, 1, bytes.Count(raw, old))
	updated := bytes.Replace(raw, old, []byte("port: 80\n    replicas: 2\n"), 1)
	require.NoError(t, os.WriteFile(specPath, updated, 0o600)) // #nosec G703 -- path under test-controlled temp dir
	stdout, _, err = runInErrContext(t, t.Context(), dir, "plan", "dev", "--detailed-exitcode",
		"--context", kindContext, "--namespace", ns)
	require.ErrorIs(t, err, planCmd.ErrChangesPresent)
	assert.NotEmpty(t, stdout)
}
