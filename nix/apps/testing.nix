# Test runner apps: test-unit, test-integration
{ lib }:

{
  test-unit = lib.mkTaggedRaceTest {
    name = "test-unit";
    description = "Run unit tests with race detector; write coverage-unit.out (build tag !integration)";
    tags = "!integration";
    coverProfile = "coverage-unit.out";
  };

  test-integration = lib.mkTaggedRaceTest {
    name = "test-integration";
    description = "Run integration tests with race detector; write coverage-integration.out (build tag integration)";
    tags = "integration";
    coverProfile = "coverage-integration.out";
    testPackages = "./internal/testing";
  };
}
