{
  description = "Deployah - A CLI tool for deploying applications to Kubernetes";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    git-hooks = {
      url = "github:cachix/git-hooks.nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      git-hooks,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
        go = pkgs.go;

        buildGoModule' = pkgs.buildGoModule.override { inherit go; };

        deployahVendorHash = "sha256-cMVETK7theSy8gSB7+ksyQ89qu4qhfRgSGFgqll/ySk=";

        deployah = import ./nix/deployah.nix {
          buildGoModule = buildGoModule';
          deployahVersion = "dev";
          vendorHash = deployahVendorHash;
          src = ./.;
          lib = nixpkgs.lib;
        };

        lib' = import ./nix/lib.nix { inherit pkgs go; };

        pre-commit-check = import ./nix/checks.nix {
          inherit
            pkgs
            go
            git-hooks
            system
            ;
          src = ./.;
        };
      in
      {
        formatter = pkgs.nixfmt-tree;

        packages = {
          default = deployah;
          deployah = deployah;
        };

        checks = {
          pre-commit = pre-commit-check;
        };

        apps = import ./nix/apps {
          inherit
            pkgs
            flake-utils
            deployah
            system
            ;
          lib = lib';
        };

        devShells.default = import ./nix/devshell.nix {
          inherit pkgs go pre-commit-check;
        };
      }
    );
}
