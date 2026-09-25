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
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/spec"
	"deployah.dev/deployah/internal/testing/testpath"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

func TestDeriveHelmAction(t *testing.T) {
	t.Parallel()
	same := configMapManifest("app", "same")
	changed := configMapManifest("app", "new")
	hookA := testHook("Job", "migrate", "migrate", "busybox", 1)
	hookB := testHook("Job", "migrate", "migrate", "alpine", 1)
	hookC := testHook("Job", "smoke", "smoke", "busybox", 2)
	upgrade := func(manifest string, hooks []*v1.Hook) helm.ReleasePrep {
		return helm.ReleasePrep{
			Operation: helm.OperationUpgrade,
			Current:   &v1.Release{Manifest: manifest, Hooks: hooks},
		}
	}
	tests := []struct {
		name    string
		prep    helm.ReleasePrep
		desired string
		hooks   []*v1.Hook
		want    semantic.HelmAction
	}{
		{name: "install", prep: helm.ReleasePrep{Operation: helm.OperationInstall}, desired: changed, want: semantic.HelmInstall},
		{
			name: "install ignores previous release",
			prep: helm.ReleasePrep{
				Operation: helm.OperationInstall,
				Current:   &v1.Release{Manifest: same, Hooks: []*v1.Hook{hookA}},
			},
			desired: changed,
			hooks:   []*v1.Hook{hookB},
			want:    semantic.HelmInstall,
		},
		{name: "unchanged", prep: upgrade(same, []*v1.Hook{hookA}), desired: same, hooks: []*v1.Hook{hookA}, want: semantic.HelmNone},
		{name: "manifest change", prep: upgrade(same, []*v1.Hook{hookA}), desired: changed, hooks: []*v1.Hook{hookA}, want: semantic.HelmUpgrade},
		{name: "hook change", prep: upgrade(same, []*v1.Hook{hookA}), desired: same, hooks: []*v1.Hook{hookB}, want: semantic.HelmUpgrade},
		{name: "hook added", prep: upgrade(same, nil), desired: same, hooks: []*v1.Hook{hookA}, want: semantic.HelmUpgrade},
		{name: "hook removed", prep: upgrade(same, []*v1.Hook{hookA, hookC}), desired: same, hooks: []*v1.Hook{hookA}, want: semantic.HelmUpgrade},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := deriveHelmAction(tt.prep, tt.desired, tt.hooks)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDeriveHelmAction_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		prep helm.ReleasePrep
		want string
	}{
		{name: "invalid operation", prep: helm.ReleasePrep{}, want: "invalid helm operation"},
		{name: "nil current", prep: helm.ReleasePrep{Operation: helm.OperationUpgrade}, want: "upgrade prep requires a current release"},
		{name: "unparseable manifest", prep: helm.ReleasePrep{Operation: helm.OperationUpgrade, Current: &v1.Release{Manifest: ":\n  - ["}}, want: "previous manifest:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := deriveHelmAction(tt.prep, "", nil)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestHelmActionFromRender(t *testing.T) {
	t.Parallel()
	same := configMapManifest("app", "same")
	changed := configMapManifest("app", "new")
	hookA := testHook("Job", "migrate", "migrate", "busybox", 1)
	hookB := testHook("Job", "migrate", "migrate", "alpine", 1)
	argsDir := filepath.Join(releaseIntentFixtures, "args-order")
	hookDir := filepath.Join(releaseIntentFixtures, "hook-formatting")
	argsPrev := testpath.ReadFile(t, argsDir, "previous.yaml")
	argsNext := testpath.ReadFile(t, argsDir, "desired.yaml")
	fmtPrev := testpath.ReadFile(t, hookDir, "previous.yaml")
	fmtNext := testpath.ReadFile(t, hookDir, "desired.yaml")
	fmtPrevHooks := readHookFixtures(t, filepath.Join(hookDir, "previous.hooks"))
	fmtNextHooks := readHookFixtures(t, filepath.Join(hookDir, "desired.hooks"))

	tests := []struct {
		name   string
		result *render.RenderResult
		want   semantic.HelmAction
	}{
		{name: "install", result: &render.RenderResult{Manifest: changed}, want: semantic.HelmInstall},
		{name: "unchanged", result: upgradeRender(same, []*v1.Hook{hookA}, same, []*v1.Hook{hookA}), want: semantic.HelmNone},
		{name: "manifest change", result: upgradeRender(same, []*v1.Hook{hookA}, changed, []*v1.Hook{hookA}), want: semantic.HelmUpgrade},
		{name: "hook change", result: upgradeRender(same, []*v1.Hook{hookA}, same, []*v1.Hook{hookB}), want: semantic.HelmUpgrade},
		{name: "args reorder", result: upgradeRender(argsPrev, nil, argsNext, nil), want: semantic.HelmUpgrade},
		{name: "hook formatting", result: upgradeRender(fmtPrev, fmtPrevHooks, fmtNext, fmtNextHooks), want: semantic.HelmNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := HelmActionFromRender(tt.result)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHelmActionFromRender_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		result *render.RenderResult
		want   string
	}{
		{name: "nil result", want: "render result is required"},
		{name: "upgrade without previous", result: &render.RenderResult{IsUpgrade: true}, want: "upgrade render requires a previous release intent"},
		{
			name: "nil previous hook",
			result: &render.RenderResult{
				IsUpgrade: true,
				Previous:  &render.ReleaseIntent{Hooks: []*render.HookIntent{nil}},
			},
			want: "previous hook 1 is nil",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := HelmActionFromRender(tt.result)
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func upgradeRender(previous string, previousHooks []*v1.Hook, desired string, desiredHooks []*v1.Hook) *render.RenderResult {
	return &render.RenderResult{
		IsUpgrade: true,
		Manifest:  desired,
		Hooks:     desiredHooks,
		Previous: &render.ReleaseIntent{
			Manifest: previous,
			Hooks:    render.HookIntents(previousHooks),
		},
	}
}

func configMapManifest(name, data string) string {
	return "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: " + name + "\n  namespace: prod\ndata:\n  key: " + data + "\n"
}

func TestApplyHelmWillRun_UnchangedHook(t *testing.T) {
	t.Parallel()
	unchanged := []semantic.TaskPlan{{
		Name:   "seed",
		Phase:  semantic.TaskPreDeploy,
		Action: semantic.TaskUnchanged,
	}}

	tests := []struct {
		name    string
		action  semantic.HelmAction
		wantRun bool
	}{
		{name: "none leaves idle", action: semantic.HelmNone},
		{name: "upgrade will run", action: semantic.HelmUpgrade, wantRun: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tasks := append([]semantic.TaskPlan(nil), unchanged...)
			applyHelmWillRun(tasks, tt.action)
			assert.Equal(t, tt.wantRun, tasks[0].WillRun)
		})
	}
}

func TestAssembleTasks_DoesNotStampWillRun(t *testing.T) {
	t.Parallel()
	resolved := resolvedWithTasks(map[string]spec.ResolvedTask{
		"migrate": hookResolved(spec.TaskOnPreDeploy, 1),
	})
	tasks, err := assembleTasks(resolved, helm.ReleasePrep{Operation: helm.OperationInstall, NextRevision: 1}, []*v1.Hook{
		testHook("Job", "migrate", "migrate", "v1", 1),
	}, nil)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.False(t, tasks[0].WillRun)
	applyHelmWillRun(tasks, semantic.HelmInstall)
	assert.True(t, tasks[0].WillRun)
}
