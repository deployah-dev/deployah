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

//go:build integration

package testing

import (
	"os"
	"testing"
)

func TestDeployahIntegration(t *testing.T) {
	if _, err := os.Stat(TestScenariosDir); err != nil {
		t.Fatalf("test scenarios directory %s is required: %v", TestScenariosDir, err)
	}

	suite := NewIntegrationTestSuite(t)

	scenarios, err := DiscoverScenarios(TestScenariosDir)
	if err != nil {
		t.Fatalf("Failed to discover scenarios: %v", err)
	}

	if len(scenarios) == 0 {
		t.Fatalf("no scenarios found under %s; tracked deployah.yaml fixtures are required", TestScenariosDir)
	}

	t.Logf("Found %d scenarios", len(scenarios))

	for _, scenario := range scenarios {
		suite.RunScenarioTest(t, scenario)
	}
}
