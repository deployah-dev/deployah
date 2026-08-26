# Test runner apps: test-unit, test-integration, test-e2e
{ lib }:

{
  test-unit = lib.mkTaggedRaceTest {
    name = "test-unit";
    description = "Run unit tests with race detector; write coverage-unit.out and junit-unit.xml (build tag !integration)";
    tags = "!integration";
    coverProfile = "coverage-unit.out";
    junitFile = "junit-unit.xml";
  };

  test-integration = lib.mkTaggedRaceTest {
    name = "test-integration";
    description = "Run integration tests with race detector; write coverage-integration.out and junit-integration.xml (build tag integration)";
    tags = "integration";
    coverProfile = "coverage-integration.out";
    junitFile = "junit-integration.xml";
    testPackages = "./internal/testing";
  };

  test-e2e = lib.mkTaggedRaceTest {
    name = "test-e2e";
    description = "Run e2e tests against a live Kind cluster; write coverage-e2e.out and junit-e2e.xml";
    tags = "e2e";
    coverProfile = "coverage-e2e.out";
    junitFile = "junit-e2e.xml";
    testPackages = "./internal/e2e";
    timeout = "30m";
    extraArgs = "-parallel=4";
    race = false; # the work is a live cluster, not concurrent Go
  };
}
