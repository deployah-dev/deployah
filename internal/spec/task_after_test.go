// Copyright 2025 The Deployah Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing the License.

package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssignHookWeights_IndependentShareZero(t *testing.T) {
	t.Parallel()

	weights, err := AssignHookWeights(map[string]Task{
		"migrate":  {On: TaskOnPreDeploy},
		"seed":     {On: TaskOnPreDeploy},
		"smoke":    {On: TaskOnPostDeploy},
		"backfill": {On: TaskOnManual},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, weights["migrate"])
	assert.Equal(t, 0, weights["seed"])
	assert.Equal(t, 0, weights["smoke"])
	_, hasManual := weights["backfill"]
	assert.False(t, hasManual)
}

func TestAssignHookWeights_ChainAndNameOrder(t *testing.T) {
	t.Parallel()

	weights, err := AssignHookWeights(map[string]Task{
		"seed":    {On: TaskOnPreDeploy, After: []string{"migrate"}},
		"migrate": {On: TaskOnPreDeploy},
		"grant":   {On: TaskOnPreDeploy, After: []string{"seed"}},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, weights["migrate"])
	assert.Equal(t, 1, weights["seed"])
	assert.Equal(t, 2, weights["grant"])
}

func TestAssignHookWeights_Cycle(t *testing.T) {
	t.Parallel()

	_, err := AssignHookWeights(map[string]Task{
		"a": {On: TaskOnPreDeploy, After: []string{"b"}},
		"b": {On: TaskOnPreDeploy, After: []string{"a"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
	assert.Contains(t, err.Error(), `"a", "b"`)
}

func TestAssignHookWeights_CycleNamesOnlyStuckTasks(t *testing.T) {
	t.Parallel()

	_, err := AssignHookWeights(map[string]Task{
		"migrate": {On: TaskOnPreDeploy},
		"a":       {On: TaskOnPreDeploy, After: []string{"b"}},
		"b":       {On: TaskOnPreDeploy, After: []string{"a"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"a", "b"`)
	assert.NotContains(t, err.Error(), "migrate")
}
