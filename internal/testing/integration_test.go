//go:build integration

package testing

import (
	"errors"
	"io/fs"
	"os"
	"testing"
)

func TestDeployahIntegration(t *testing.T) {
	// Skip if scenarios directory doesn't exist
	if _, err := os.Stat(TestScenariosDir); errors.Is(err, fs.ErrNotExist) {
		t.Skipf("Test scenarios directory %s does not exist. Skipping integration tests.", TestScenariosDir)
		return
	}

	suite := NewIntegrationTestSuite(t)

	// Auto-discover scenarios from directory structure
	scenarios, err := DiscoverScenarios(TestScenariosDir)
	if err != nil {
		t.Fatalf("Failed to discover scenarios: %v", err)
	}

	if len(scenarios) == 0 {
		t.Skip("No scenarios found in test scenarios directory")
		return
	}

	t.Logf("Found %d scenarios", len(scenarios))

	// Run each scenario
	for _, scenario := range scenarios {
		suite.RunScenarioTest(t, scenario)
	}
}
