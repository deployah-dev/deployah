# Code quality apps: fmt, lint, lint-md, tidy
{
  pkgs,
  lib,
  go,
}:

{
  # Skip until golangci-lint supports Go 1.27:
  # https://github.com/golangci/golangci-lint/issues/6643
  # nixpkgs' binary is built with Go 1.26 and rejects go.mod 1.27.
  fmt = lib.mkApp {
    name = "fmt";
    description = "Format Go files (skipped until Go 1.27 support)";
    script = ''
      echo "skipping golangci-lint fmt: Go 1.27 not supported yet"
      echo "see https://github.com/golangci/golangci-lint/issues/6643"
      exit 0
    '';
  };

  lint = lib.mkApp {
    name = "lint";
    description = "Run golangci-lint (skipped until Go 1.27 support)";
    script = ''
      echo "skipping golangci-lint: Go 1.27 not supported yet"
      echo "see https://github.com/golangci/golangci-lint/issues/6643"
      exit 0
    '';
  };

  lint-md = lib.mkApp {
    name = "lint-md";
    description = "Lint Markdown files with markdownlint";
    script = ''
      exec ${pkgs.markdownlint-cli}/bin/markdownlint '**/*.md'
    '';
  };

  tidy = lib.mkApp {
    name = "tidy";
    description = "Run go mod tidy for the module";
    script = ''
      exec ${go}/bin/go mod tidy
    '';
  };

  gen-docs = lib.mkApp {
    name = "gen-docs";
    description = "Generate the CLI reference under docs/cli from the command tree";
    script = ''
      exec ${go}/bin/go run ./internal/tools/gendocs
    '';
  };
}
