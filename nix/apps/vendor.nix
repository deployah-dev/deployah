# Vendor hash management app
{ pkgs, system }:

let
  updateVendorHash = pkgs.writeShellApplication {
    name = "update-vendor-hash";
    runtimeInputs = with pkgs; [
      gnused
      gnugrep
    ];
    text = ''
      set -euo pipefail

      if [ ! -f flake.nix ]; then
        echo "run from repo root" >&2
        exit 1
      fi

      # A deliberately wrong hash so the build fails and Nix prints the real one.
      fake="sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

      echo "Computing goModules hash via nix build..."
      # Override the goModules output hash and build it. Nix reports the correct
      # hash in the mismatch error. This uses plain `nix build` instead of
      # nix-prefetch, whose deep re-evaluation trips a nixpkgs fetcher bug
      # (getRevWithTag) on the pinned nixpkgs. Nix evaluates this lazily, so it
      # never touches the broken fetcher path.
      build_log=$(
        nix build --no-link --impure --expr \
          "(builtins.getFlake (toString ./.)).packages.${system}.default.goModules.overrideAttrs (_: { outputHash = \"$fake\"; outputHashAlgo = \"sha256\"; })" 2>&1 || true
      )

      NEW_HASH=$(
        printf '%s\n' "$build_log" \
          | sed -n 's/.*got:[[:space:]]*\(sha256-[A-Za-z0-9+/=]*\).*/\1/p' \
          | head -n1
      )

      if [ -z "$NEW_HASH" ]; then
        echo "Could not determine the vendor hash. Build output:" >&2
        printf '%s\n' "$build_log" >&2
        exit 1
      fi

      OLD_HASH=$(
        grep 'deployahVendorHash' flake.nix \
          | sed -n 's/.*"\(sha256-[^"]*\)".*/\1/p'
      )

      if [ "$OLD_HASH" = "$NEW_HASH" ]; then
        echo "deployahVendorHash already correct: $NEW_HASH"
        exit 0
      fi

      sed -i "s|deployahVendorHash = \"sha256-[^\"]*\";|deployahVendorHash = \"$NEW_HASH\";|" flake.nix
      echo "Updated deployahVendorHash:"
      echo "  old: $OLD_HASH"
      echo "  new: $NEW_HASH"
      echo "Commit flake.nix."
    '';
  };
in
{
  update-vendor-hash = {
    type = "app";
    program = "${updateVendorHash}/bin/update-vendor-hash";
    meta = {
      mainProgram = "update-vendor-hash";
      description = "Recompute deployahVendorHash in flake.nix via nix build";
    };
  };
}
