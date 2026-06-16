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
    golangci-lint = {
      enable = true;
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
