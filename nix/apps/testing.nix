# Test runner apps: test-unit, test-integration, test-e2e
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

  test-e2e = lib.mkTaggedRaceTest {
    name = "test-e2e";
    description = "Run e2e tests against a live Kind cluster; write coverage-e2e.out";
    tags = "e2e";
    coverProfile = "coverage-e2e.out";
    testPackages = "./internal/e2e";
    timeout = "15m";
    race = false; # the work is a live cluster, not concurrent Go
  };
}
