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
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/e2e-framework/klient/wait"

	"deployah.dev/deployah/internal/testing/e2e"

	inttest "deployah.dev/deployah/internal/testing"
)

// TestPublicImagePull deploys an image that is not special-cased by the
// suite. Kind pulls it from the public registry.
// Failures in this test should point here, not at the suite caller.
//
//nolint:thelper
func runPublicImagePull(t *testing.T, env *e2eEnv) {
	src := filepath.Join(env.scenariosDir, "basic-web-service")
	require.DirExists(t, src)
	dir, err := inttest.NewScenarioWorkspace(t, src)
	require.NoError(t, err)

	specPath := filepath.Join(dir, "deployah.yaml")
	raw, err := os.ReadFile(specPath) // #nosec G304 -- copied scenario workspace
	require.NoError(t, err)
	updated := strings.ReplaceAll(string(raw), "image: nginx:latest", "image: nginx:1.27")
	require.NotEqual(t, string(raw), updated)
	require.NoError(t, os.WriteFile(specPath, []byte(updated), 0o600)) // #nosec G703 -- path under the copied workspace

	ns := fixtureNamespace("public-image")
	claim, err := env.claimNamespace(t.Context(), ns)
	require.NoError(t, err)
	t.Cleanup(func() {
		if cleanupErr := env.cleanupClaim(t, dir, claim, []releaseTarget{{
			Project: "basic-web-service", Env: "dev",
		}}); cleanupErr != nil {
			t.Errorf("cleanup: %v", cleanupErr)
		}
	})

	runIn(t, dir, "deploy", "dev", "--context", kindContext, "--namespace", ns)
	require.NoError(t, wait.For(func(ctx context.Context) (bool, error) {
		objects, obsErr := env.observe(ctx, e2e.ResourceSelect{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       "basic-web-service-dev",
		}, ns)
		if obsErr != nil {
			if errors.Is(obsErr, e2e.ErrObserveRetry) {
				return false, nil
			}
			return false, obsErr
		}
		if len(objects) != 1 {
			return false, nil
		}
		diffs := inttest.DiffSubset("$", map[string]any{
			"spec": map[string]any{
				"template": map[string]any{
					"spec": map[string]any{
						"containers": []any{
							map[string]any{
								"name":  "web",
								"image": "docker.io/library/nginx:1.27",
							},
						},
					},
				},
			},
			"status": map[string]any{"readyReplicas": int64(1)},
		}, objects[0])
		return len(diffs) == 0, nil
	}, wait.WithTimeout(4*time.Minute), wait.WithInterval(resourcePollInterval),
		wait.WithContext(t.Context()), wait.WithImmediate()))
}
