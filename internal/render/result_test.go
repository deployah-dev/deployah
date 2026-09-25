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

package render_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/render"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

func TestHookIntents_NilEntryAndSliceCopy(t *testing.T) {
	t.Parallel()

	events := []v1.HookEvent{v1.HookPreUpgrade}
	policies := []v1.HookDeletePolicy{v1.HookBeforeHookCreation}
	logs := []v1.HookOutputLogPolicy{v1.HookOutputOnSucceeded}
	hooks := []*v1.Hook{
		nil,
		{
			Name:              "migrate",
			Events:            events,
			DeletePolicies:    policies,
			OutputLogPolicies: logs,
		},
	}

	got := render.HookIntents(hooks)
	require.Len(t, got, 2)
	assert.Nil(t, got[0])
	require.NotNil(t, got[1])

	events[0] = v1.HookPostUpgrade
	policies[0] = v1.HookFailed
	logs[0] = v1.HookOutputOnFailed
	assert.Equal(t, []v1.HookEvent{v1.HookPreUpgrade}, got[1].Events)
	assert.Equal(t, []v1.HookDeletePolicy{v1.HookBeforeHookCreation}, got[1].DeletePolicies)
	assert.Equal(t, []v1.HookOutputLogPolicy{v1.HookOutputOnSucceeded}, got[1].OutputLogPolicies)
	assert.Nil(t, render.HookIntents(nil))
	assert.Nil(t, (*render.HookIntent)(nil).Hook())

	hook := got[1].Hook()
	require.NotNil(t, hook)
	got[1].Events[0] = v1.HookPostDelete
	assert.Equal(t, []v1.HookEvent{v1.HookPreUpgrade}, hook.Events)
}

func TestHookIntent_RoundTrip(t *testing.T) {
	t.Parallel()

	ran := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	src := &v1.Hook{
		Name:     "migrate",
		Kind:     "Job",
		Path:     "templates/migrate.yaml",
		Manifest: "kind: Job\nmetadata:\n  name: migrate\n",
		Events:   []v1.HookEvent{v1.HookPreUpgrade, v1.HookPostUpgrade},
		LastRun: v1.HookExecution{
			StartedAt:   ran,
			CompletedAt: ran,
			Phase:       v1.HookPhaseSucceeded,
		},
		Weight:            3,
		DeletePolicies:    []v1.HookDeletePolicy{v1.HookBeforeHookCreation, v1.HookSucceeded},
		OutputLogPolicies: []v1.HookOutputLogPolicy{v1.HookOutputOnSucceeded, v1.HookOutputOnFailed},
	}

	got := render.HookIntents([]*v1.Hook{src})
	require.Len(t, got, 1)
	require.NotNil(t, got[0])

	want := *src
	want.LastRun = v1.HookExecution{}
	assert.Equal(t, &want, got[0].Hook())
}
