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
