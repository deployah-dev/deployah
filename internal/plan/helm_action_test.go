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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

func TestDeriveHelmAction(t *testing.T) {
	t.Parallel()
	helmChange := labeledCreate("app", "app")
	crdChange := semantic.ResourceChange{
		Resource: semantic.ResourceRef{
			APIVersion: "apiextensions.k8s.io/v1",
			Kind:       "CustomResourceDefinition",
			Name:       "widgets.example.com",
		},
		Origin: semantic.ResourceOrigin{Kind: semantic.OriginCRD},
		Action: semantic.Create,
	}
	nsChange := semantic.ResourceChange{
		Resource: semantic.ResourceRef{APIVersion: "v1", Kind: "Namespace", Name: "prod"},
		Origin:   semantic.ResourceOrigin{Kind: semantic.OriginNamespace},
		Action:   semantic.Create,
	}
	unchanged := semantic.TaskPlan{Name: "seed", Phase: semantic.TaskPreDeploy, Action: semantic.TaskUnchanged}
	hookCreate := semantic.TaskPlan{Name: "migrate", Phase: semantic.TaskPreDeploy, Action: semantic.TaskCreate}
	hookDelete := semantic.TaskPlan{Name: "old", Phase: semantic.TaskPreDeploy, Action: semantic.TaskDelete}
	schedule := semantic.TaskPlan{Name: "cleanup", Phase: semantic.TaskSchedule, Action: semantic.TaskCreate}
	tests := []struct {
		name    string
		op      helm.Operation
		changes []semantic.ResourceChange
		tasks   []semantic.TaskPlan
		want    semantic.HelmAction
	}{
		{name: "install", op: helm.OperationInstall, want: semantic.HelmInstall},
		{name: "install ignores changes", op: helm.OperationInstall, changes: []semantic.ResourceChange{crdChange}, want: semantic.HelmInstall},
		{name: "upgrade with helm change", op: helm.OperationUpgrade, changes: []semantic.ResourceChange{helmChange}, want: semantic.HelmUpgrade},
		{name: "upgrade with hook create", op: helm.OperationUpgrade, tasks: []semantic.TaskPlan{hookCreate}, want: semantic.HelmUpgrade},
		{name: "upgrade with hook delete", op: helm.OperationUpgrade, tasks: []semantic.TaskPlan{hookDelete}, want: semantic.HelmUpgrade},
		{name: "crd change does not upgrade", op: helm.OperationUpgrade, changes: []semantic.ResourceChange{crdChange}, tasks: []semantic.TaskPlan{unchanged}, want: semantic.HelmNone},
		{name: "namespace change does not upgrade", op: helm.OperationUpgrade, changes: []semantic.ResourceChange{nsChange}, want: semantic.HelmNone},
		{name: "schedule change does not upgrade", op: helm.OperationUpgrade, tasks: []semantic.TaskPlan{schedule}, want: semantic.HelmNone},
		{name: "unchanged hooks none", op: helm.OperationUpgrade, tasks: []semantic.TaskPlan{unchanged}, want: semantic.HelmNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, deriveHelmAction(tt.op, tt.changes, tt.tasks))
		})
	}
}

func TestApplyHelmWillRun_OriginCRDUnchangedHook(t *testing.T) {
	t.Parallel()
	unchanged := []semantic.TaskPlan{{
		Name:   "seed",
		Phase:  semantic.TaskPreDeploy,
		Action: semantic.TaskUnchanged,
	}}
	crd := []semantic.ResourceChange{{
		Resource: semantic.ResourceRef{
			APIVersion: "apiextensions.k8s.io/v1",
			Kind:       "CustomResourceDefinition",
			Name:       "widgets.example.com",
		},
		Origin: semantic.ResourceOrigin{Kind: semantic.OriginCRD},
		Action: semantic.Create,
	}}

	none := deriveHelmAction(helm.OperationUpgrade, crd, unchanged)
	assert.Equal(t, semantic.HelmNone, none)
	idle := append([]semantic.TaskPlan(nil), unchanged...)
	applyHelmWillRun(idle, none)
	assert.False(t, idle[0].WillRun)

	upgrade := append([]semantic.TaskPlan(nil), unchanged...)
	applyHelmWillRun(upgrade, semantic.HelmUpgrade)
	assert.True(t, upgrade[0].WillRun)
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
