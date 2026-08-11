# Code quality apps: fmt, lint, lint-md, lint-links, tidy
{
  pkgs,
  lib,
  go,
  golangci-lint,
}:

let
  withGo =
    script:
    ''
      export GOTOOLCHAIN=local
      export GOROOT="${go}/share/go"
      export PATH="${go}/bin:$PATH"
    ''
    + script;
in
{
  fmt = lib.mkApp {
    name = "fmt";
    description = "Format Go files (gofumpt + gci via golangci-lint)";
    runtimeInputs = [
      go
      golangci-lint
    ];
    script = withGo ''
      exec golangci-lint fmt ./...
    '';
  };

  lint = lib.mkApp {
    name = "lint";
    description = "Run golangci-lint";
    runtimeInputs = [
      go
      golangci-lint
    ];
    script = withGo ''
      exec golangci-lint run ./...
    '';
  };

  lint-md = lib.mkApp {
    name = "lint-md";
    description = "Lint Markdown files with markdownlint";
    script = ''
      exec ${pkgs.markdownlint-cli}/bin/markdownlint '**/*.md'
    '';
  };

  lint-links = lib.mkApp {
    name = "lint-links";
    description = "Check relative Markdown links and heading anchors with lychee";
    script = ''
      exec ${pkgs.lychee}/bin/lychee --offline --include-fragments --no-progress '**/*.md'
    '';
  };

  tidy = lib.mkApp {
    name = "tidy";
    description = "Run go mod tidy for the module";
    runtimeInputs = [ go ];
    script = withGo ''
      exec go mod tidy
    '';
  };

  gen-docs = lib.mkApp {
    name = "gen-docs";
    description = "Generate the CLI reference under docs/cli from the command tree";
    runtimeInputs = [ go ];
    script = withGo ''
      exec go run ./internal/tools/gendocs
    '';
  };
}
