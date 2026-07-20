//go:build integration

package testing

import (
	"os"
	"testing"
)

// TestPlanScenarios runs every scenarios/plan-* scenario through the plan
// engine and checks the result against plan-config.yaml and any golden files.
func TestPlanScenarios(t *testing.T) {
	if _, err := os.Stat(TestScenariosDir); err != nil {
		t.Fatalf("test scenarios directory %s is required: %v", TestScenariosDir, err)
	}

	scenarios, err := DiscoverPlanScenarios(TestScenariosDir)
	if err != nil {
		t.Fatalf("Failed to discover plan scenarios: %v", err)
	}

	if len(scenarios) == 0 {
		t.Fatalf("no plan scenarios found under %s; tracked plan-* fixtures are required", TestScenariosDir)
	}

	t.Logf("Found %d plan scenarios", len(scenarios))

	for _, scenario := range scenarios {
		RunPlanScenarioTest(t, scenario)
	}
}
