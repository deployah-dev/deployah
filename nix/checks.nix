# Pre-commit hooks and git-hooks configuration
{
  pkgs,
  go,
  git-hooks,
  system,
  src,
  golangci-lint,
}:

let
  vendorHashCheck = pkgs.writeShellApplication {
    name = "vendor-hash-check";
    runtimeInputs = with pkgs; [
      git
      gnused
    ];
    text = ''
      if git diff --cached --quiet -- go.sum; then
        exit 0
      fi
      STAGED=$(git show :flake.nix 2>/dev/null | grep 'deployahVendorHash' | sed -n 's/.*"\(sha256-[^"]*\)".*/\1/p')
      HEAD=$(git show HEAD:flake.nix 2>/dev/null | grep 'deployahVendorHash' | sed -n 's/.*"\(sha256-[^"]*\)".*/\1/p')
      if [ "$STAGED" != "$HEAD" ]; then
        exit 0
      fi
      echo "go.sum changed but deployahVendorHash in flake.nix is unchanged" >&2
      echo "Run: nix run .#update-vendor-hash" >&2
      exit 1
    '';
  };
in
git-hooks.lib.${system}.run {
  inherit src;
  hooks = {
    # git-hooks' default gofmt package is pkgs.go (still 1.26).
    gofmt = {
      enable = true;
      package = go;
    };
    golangci-lint = {
      enable = true;
      package = golangci-lint;
      extraPackages = [ go ];
    };
    markdownlint = {
      enable = true;
      excludes = [ "node_modules" ];
      settings.configuration = builtins.fromJSON (builtins.readFile ../.markdownlint.json);
    };
    # markdownlint only validates heading anchors inside a single file, so a
    # broken cross-file link or anchor (docs/platform.md#profiles) passes it.
    # Offline mode skips network URLs, which keeps the hook fast and stable.
    markdown-links = {
      enable = true;
      name = "markdown-links";
      entry = "${pkgs.lychee}/bin/lychee --offline --include-fragments --no-progress '**/*.md'";
      files = "\\.md$";
      language = "system";
      pass_filenames = false;
    };
    go-mod-tidy = {
      enable = true;
      name = "go-mod-tidy";
      entry = "${go}/bin/go mod tidy";
      files = "(\\.go|go\\.mod|go\\.sum)$";
      pass_filenames = false;
    };
    # Regenerate the CLI reference (docs/cli) from the command tree. If a command
    # or flag changed without regenerating, this rewrites the files and the
    # commit is blocked until they are staged. Run: nix run .#gen-docs
    gen-docs = {
      enable = true;
      name = "gen-docs";
      entry = "${go}/bin/go run ./internal/tools/gendocs";
      files = "(\\.go|go\\.mod|go\\.sum)$";
      pass_filenames = false;
    };
    vendor-hash-check = {
      enable = true;
      name = "vendor-hash-check";
      entry = "${vendorHashCheck}/bin/vendor-hash-check";
      files = "^go\\.(mod|sum)$";
      language = "system";
      pass_filenames = false;
    };
    nixfmt.enable = true;
  };
}
