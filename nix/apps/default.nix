# Aggregates all flake apps
{
  pkgs,
  lib,
  flake-utils,
  deployah,
  system,
  go,
}:

let
  quality = import ./quality.nix { inherit pkgs lib go; };
  testing = import ./testing.nix { inherit lib; };
  vendor = import ./vendor.nix { inherit pkgs system; };
  demo = import ./demo.nix { inherit pkgs lib deployah; };
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
// demo
