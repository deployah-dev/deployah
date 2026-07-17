//go:build integration

package testing

import (
	"errors"
	"io/fs"
	"os"
	"testing"
)

// TestPlanScenarios runs every scenarios/plan-* scenario through the plan
// engine and checks the result against plan-config.yaml and any golden files.
func TestPlanScenarios(t *testing.T) {
	if _, err := os.Stat(TestScenariosDir); errors.Is(err, fs.ErrNotExist) {
		t.Skipf("Test scenarios directory %s does not exist. Skipping plan scenario tests.", TestScenariosDir)
		return
	}

	scenarios, err := DiscoverPlanScenarios(TestScenariosDir)
	if err != nil {
		t.Fatalf("Failed to discover plan scenarios: %v", err)
	}

	if len(scenarios) == 0 {
		t.Skip("No plan scenarios found in test scenarios directory")
		return
	}

	t.Logf("Found %d plan scenarios", len(scenarios))

	for _, scenario := range scenarios {
		RunPlanScenarioTest(t, scenario)
	}
}
