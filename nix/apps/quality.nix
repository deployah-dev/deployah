# Code quality apps: fmt, lint, lint-md, tidy
{ pkgs, lib }:

{
  fmt = lib.mkApp {
    name = "fmt";
    description = "Format Go files (gofumpt + gci via golangci-lint)";
    script = ''
      exec ${pkgs.golangci-lint}/bin/golangci-lint fmt ./...
    '';
  };

  lint = lib.mkApp {
    name = "lint";
    description = "Run golangci-lint";
    script = ''
      exec ${pkgs.golangci-lint}/bin/golangci-lint run ./...
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
      exec ${pkgs.go}/bin/go mod tidy
    '';
  };
}
