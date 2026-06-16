# Vendor hash management app
{ pkgs, system }:

let
  updateVendorHash = pkgs.writeShellApplication {
    name = "update-vendor-hash";
    runtimeInputs = with pkgs; [
      nix-prefetch
      gnused
    ];
    text = ''
      set -euo pipefail

      if [ ! -f flake.nix ]; then
        echo "run from repo root" >&2
        exit 1
      fi

      echo "Prefetching goModules hash..."
      NEW_HASH=$(
        nix-prefetch \
          --option extra-experimental-features 'nix-command flakes' \
          -E "{ sha256 }: (builtins.getFlake \"$(pwd)\").packages.${system}.default.goModules.overrideAttrs (_: { outputHash = sha256; outputHashAlgo = \"sha256\"; })"
      )

      OLD_HASH=$(
        grep 'deployahVendorHash' flake.nix \
          | sed -n 's/.*"\(sha256-[^"]*\)".*/\1/p'
      )

      if [ "$OLD_HASH" = "$NEW_HASH" ]; then
        echo "deployahVendorHash unchanged: $NEW_HASH"
        exit 0
      fi

      sed -i "s|deployahVendorHash = \"sha256-[^\"]*\";|deployahVendorHash = \"$NEW_HASH\";|" flake.nix
      echo "Updated deployahVendorHash:"
      echo "  old: $OLD_HASH"
      echo "  new: $NEW_HASH"
      echo "Commit flake.nix"
    '';
  };
in
{
  update-vendor-hash = {
    type = "app";
    program = "${updateVendorHash}/bin/update-vendor-hash";
    meta = {
      mainProgram = "update-vendor-hash";
      description = "Recompute deployahVendorHash in flake.nix via nix-prefetch goModules";
    };
  };
}
