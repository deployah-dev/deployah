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
        # nixpkgs' default `go` is still 1.26; pin 1.27 for the module.
        go = pkgs.go_1_27;

        buildGoModule' = pkgs.buildGoModule.override { inherit go; };

        deployahVendorHash = "sha256-zltKCgHj7sC3FSTbuAFxBjS03XaU/d6Vz5BKH8wgkbc=";

        inherit (pkgs) golangci-lint gopls;

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
            golangci-lint
            ;
          src = ./.;
        };
      in
      {
        formatter = pkgs.nixfmt-tree;

        packages = {
          default = deployah;
          deployah = deployah;
          golangci-lint = golangci-lint;
          gopls = gopls;
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
            go
            golangci-lint
            ;
          lib = lib';
        };

        devShells.default = import ./nix/devshell.nix {
          inherit
            pkgs
            go
            gopls
            pre-commit-check
            golangci-lint
            ;
        };
      }
    );
}
