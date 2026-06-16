# Aggregates all flake apps
{
  pkgs,
  lib,
  flake-utils,
  deployah,
  system,
}:

let
  quality = import ./quality.nix { inherit pkgs lib; };
  testing = import ./testing.nix { inherit lib; };
  vendor = import ./vendor.nix { inherit pkgs system; };
in
{
  default = flake-utils.lib.mkApp {
    drv = deployah;
    exePath = "/bin/deployah";
  };
}
// quality
// testing
// vendor
