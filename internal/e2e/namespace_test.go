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
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/e2e-framework/klient/wait"

	"deployah.dev/deployah/internal/testing/e2e"

	inttest "deployah.dev/deployah/internal/testing"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// TestDeployCreatesNamespace checks that deployah deploy creates a namespace
// that the suite did not create first.
func (s *E2ESuite) TestDeployCreatesNamespace() {
	t := s.T()
	src := filepath.Join(s.scenariosDir, "basic-web-service")
	require.DirExists(t, src)
	dir, err := inttest.NewScenarioWorkspace(t, src)
	require.NoError(t, err)

	var buf [8]byte
	_, err = rand.Read(buf[:])
	require.NoError(t, err)
	ns := "e2e-ns-" + hex.EncodeToString(buf[:])
	_, getErr := s.namespaceView(t.Context(), ns)
	require.ErrorIs(t, getErr, e2e.ErrNotFound)

	var uid string
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), namespaceWaitTimeout)
		defer cancel()
		if cleanupErr := s.cleanupCreatedNamespace(ctx, t, dir, ns, uid, "basic-web-service", "dev"); cleanupErr != nil {
			t.Errorf("cleanup: %v", cleanupErr)
		}
	})

	_, stderr, deployErr := runInErr(t, dir, "deploy", "dev",
		"--context", kindContext, "--namespace", ns)
	view, viewErr := s.namespaceView(context.Background(), ns)
	if viewErr == nil && view != nil {
		uid = view.UID
		markCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		marked := &corev1.Namespace{}
		res, markErr := s.newResources("")
		if markErr == nil {
			markErr = res.Get(markCtx, ns, "", marked)
		}
		if markErr == nil && (marked.Name != ns || string(marked.UID) != uid) {
			markErr = fmt.Errorf("namespace %s: identity changed before marking", ns)
		}
		if markErr == nil {
			if marked.Annotations == nil {
				marked.Annotations = map[string]string{}
			}
			marked.Annotations[e2e.RunAnnotation] = "deploy-creates-namespace"
			markErr = res.Update(markCtx, marked)
		}
		cancel()
		if markErr != nil {
			t.Errorf("mark namespace: %v", markErr)
		}
	}
	require.NoError(t, deployErr, stderr)
	require.NotEmpty(t, uid)
	assertReleaseExists(t, dir, ns, "basic-web-service", "dev")

	require.NoError(t, wait.For(func(ctx context.Context) (bool, error) {
		objects, obsErr := s.observe(ctx, e2e.ResourceSelect{
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
			"status": map[string]any{"readyReplicas": int64(1)},
		}, objects[0])
		return len(diffs) == 0, nil
	}, wait.WithTimeout(3*time.Minute), wait.WithInterval(resourcePollInterval),
		wait.WithContext(t.Context()), wait.WithImmediate()))
}

func (s *E2ESuite) cleanupCreatedNamespace(ctx context.Context, t *testing.T, dir, name, uid, project, env string) error {
	t.Helper()
	if uid == "" {
		view, err := s.namespaceView(ctx, name)
		if errors.Is(err, e2e.ErrNotFound) || apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("namespace %s: ownership uncertain: %w", name, err)
		}
		if view == nil {
			return fmt.Errorf("namespace %s: ownership uncertain, not deleting", name)
		}
		return fmt.Errorf("namespace %s exists without a recorded uid, not deleting", name)
	}
	phases := []e2e.CleanupPhase{
		{Name: "delete release", Run: func(phaseCtx context.Context) error {
			if err := s.confirmCreated(phaseCtx, name, uid); err != nil {
				if errors.Is(err, e2e.ErrNamespaceGone) {
					return nil
				}
				return err
			}
			return s.deleteRelease(t, dir, name, releaseTarget{Project: project, Env: env})(phaseCtx)
		}},
		{Name: "workloads", Run: func(phaseCtx context.Context) error {
			if err := s.confirmCreated(phaseCtx, name, uid); err != nil {
				if errors.Is(err, e2e.ErrNamespaceGone) {
					return nil
				}
				return err
			}
			return s.deleteWorkloads(phaseCtx, name)
		}},
		{Name: "pvcs", Run: func(phaseCtx context.Context) error {
			if err := s.confirmCreated(phaseCtx, name, uid); err != nil {
				if errors.Is(err, e2e.ErrNamespaceGone) {
					return nil
				}
				return err
			}
			return s.deletePVCs(phaseCtx, name)
		}},
		{Name: "namespace", Run: func(phaseCtx context.Context) error {
			if err := s.confirmCreated(phaseCtx, name, uid); err != nil {
				if errors.Is(err, e2e.ErrNamespaceGone) {
					return nil
				}
				return err
			}
			return s.deleteNamespaceObject(phaseCtx, e2e.Claim{Name: name, UID: uid, RunID: "recorded"})
		}},
	}
	return e2e.RunCleanup(ctx, phases, e2e.WithPhaseTimeout(namespaceWaitTimeout))
}

func (s *E2ESuite) confirmCreated(ctx context.Context, name, uid string) error {
	view, err := s.namespaceView(ctx, name)
	if errors.Is(err, e2e.ErrNotFound) {
		return e2e.ErrNamespaceGone
	}
	if err != nil {
		return fmt.Errorf("namespace %s: %w", name, err)
	}
	if view == nil || view.Name != name || view.UID != uid {
		return fmt.Errorf("namespace %s: %w", name, e2e.ErrNotOwner)
	}
	return nil
}
