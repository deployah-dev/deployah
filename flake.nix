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
        # nixpkgs' default `go` may lag; pin the toolchain Nabat requires.
        go = pkgs.go_1_27;

        buildGoModule' = pkgs.buildGoModule.override { inherit go; };

        deployahVendorHash = "sha256-CT9xvdQvEU6shdz12Yu30R/uvbV36oA6Nok0LZ9f2mE=";

        golangci-lint = import ./nix/golangci-lint.nix {
          buildGoModule = buildGoModule';
          inherit (pkgs)
            fetchFromGitHub
            installShellFiles
            lib
            stdenv
            buildPackages
            ;
        };

        # nixpkgs gopls is built with default `go` (still 1.26). gopls must be
        # compiled with at least the toolchain it analyzes.
        gopls = pkgs.gopls.override {
          buildGoLatestModule = buildGoModule';
        };

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
