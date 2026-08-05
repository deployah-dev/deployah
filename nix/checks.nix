# Pre-commit hooks and git-hooks configuration
{
  pkgs,
  go,
  git-hooks,
  system,
  src,
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
    # Use the pinned Go toolchain's gofmt; git-hooks' default wrapper
    # still ships Go 1.26 and rejects go.mod 1.27.
    gofmt = {
      enable = true;
      entry = "${go}/bin/gofmt -l -w";
      files = "\\.go$";
    };
    # Disabled while go.mod is 1.27: nixpkgs golangci-lint is built with
    # Go 1.26 and fails config load. Re-enable when
    # https://github.com/golangci/golangci-lint/issues/6643 lands.
    golangci-lint = {
      enable = false;
      extraPackages = [ go ];
    };
    markdownlint = {
      enable = true;
      excludes = [ "node_modules" ];
      settings.configuration = builtins.fromJSON (builtins.readFile ../.markdownlint.json);
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
