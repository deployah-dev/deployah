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
        helmOverlay = final: prev: {
          kubernetes-helm = prev.kubernetes-helm.overrideAttrs (_old: rec {
            version = "4.2.0";
            src = prev.fetchFromGitHub {
              owner = "helm";
              repo = "helm";
              rev = "v${version}";
              sha256 = "sha256-Wyihzf7KpnVuIdp5lmjhB7uLAGgtmI0TXYl29uaVC5Y=";
            };
            vendorHash = "sha256-QTDC0v0BPE3FoK9AAq1n2jWxOE9gB9OsoY2wnpcCDUQ=";
            ldflags = [
              "-w"
              "-s"
              "-X helm.sh/helm/v4/internal/version.version=v${version}"
              "-X helm.sh/helm/v4/internal/version.gitCommit=v${version}"
            ];
            preBuild = ''
              K8S_MODULES_VER="$(go list -f '{{.Version}}' -m k8s.io/client-go)"
              K8S_MODULES_MAJOR_VER="$(($(cut -d. -f1 <<<"$K8S_MODULES_VER") + 1))"
              K8S_MODULES_MINOR_VER="$(cut -d. -f2 <<<"$K8S_MODULES_VER")"
              old_ldflags="''${ldflags}"
              ldflags="''${ldflags} -X helm.sh/helm/v4/pkg/lint/rules.k8sVersionMajor=''${K8S_MODULES_MAJOR_VER}"
              ldflags="''${ldflags} -X helm.sh/helm/v4/pkg/lint/rules.k8sVersionMinor=''${K8S_MODULES_MINOR_VER}"
              ldflags="''${ldflags} -X helm.sh/helm/v4/pkg/chartutil.k8sVersionMajor=''${K8S_MODULES_MAJOR_VER}"
              ldflags="''${ldflags} -X helm.sh/helm/v4/pkg/chartutil.k8sVersionMinor=''${K8S_MODULES_MINOR_VER}"
            '';
            doCheck = false;
          });
        };

        pkgs = import nixpkgs {
          inherit system;
          overlays = [ helmOverlay ];
        };
        packageName = "deployah";
        deployahVersion = "dev";
        go = pkgs.go;

        buildGoModule' = pkgs.buildGoModule.override { inherit go; };

        deployahVendorHash = "sha256-dqeijkQqkumMH4Z8/79WCUBMsDo1VH+hJW0WR+SZyx8=";

        deployah = import ./nix/deployah.nix {
          buildGoModule = buildGoModule';
          inherit deployahVersion;
          vendorHash = deployahVendorHash;
          src = ./.;
          lib = nixpkgs.lib;
        };

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

        devTools = with pkgs; [
          go
          gopls
          gotools
          golangci-lint
          markdownlint-cli
          delve
          git
          kind
          kubectl
          kubecolor
          kubernetes-helm
          just
          jq
          yq-go
        ];

        mkApp =
          {
            name,
            description,
            script,
            runtimeInputs ? [ ],
          }:
          let
            shellApp = pkgs.writeShellApplication {
              inherit name runtimeInputs;
              text = script;
            };
          in
          {
            type = "app";
            program = "${shellApp}/bin/${name}";
            meta = {
              mainProgram = name;
              inherit description;
            };
          };

        mkTaggedRaceTest =
          {
            name,
            description,
            tags,
            coverProfile,
            testPackages ? "./...",
          }:
          mkApp {
            inherit name description;
            runtimeInputs = [
              go
              pkgs.stdenv.cc
            ];
            script = ''
              export GOTOOLCHAIN=local
              export CGO_ENABLED=1
              go="${go}"
              export GOROOT="''${go}/share/go"
              export PATH="''${go}/bin:$PATH"
              mapfile -t testpkgs < <("$go/bin/go" list -tags=${tags} ${testPackages} || true)
              if [ ''${#testpkgs[@]} -eq 0 ]; then
                echo "go list: no test packages after filters (tags=${tags})" >&2
                exit 1
              fi
              exec "$go/bin/go" test -tags=${tags} -race -shuffle=on -covermode=atomic \
                -coverpkg=./... -coverprofile=${coverProfile} -timeout 10m "''${testpkgs[@]}"
            '';
          };

        pre-commit-check = git-hooks.lib.${system}.run {
          src = ./.;
          hooks = {
            golangci-lint = {
              enable = true;
              extraPackages = [ go ];
            };
            markdownlint = {
              enable = true;
              excludes = [ "node_modules" ];
              settings.configuration = builtins.fromJSON (builtins.readFile ./.markdownlint.json);
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
        };
      in
      {
        formatter = pkgs.nixfmt-tree;

        packages = {
          default = deployah;
          ${packageName} = deployah;
        };

        checks = {
          pre-commit = pre-commit-check;
        };

        apps = {
          default = flake-utils.lib.mkApp {
            drv = deployah;
            exePath = "/bin/deployah";
          };

          fmt = mkApp {
            name = "fmt";
            description = "Format Go files (gofumpt + gci via golangci-lint)";
            script = ''
              exec ${pkgs.golangci-lint}/bin/golangci-lint fmt ./...
            '';
          };

          tidy = mkApp {
            name = "tidy";
            description = "Run go mod tidy for the module";
            script = ''
              exec ${go}/bin/go mod tidy
            '';
          };

          update-vendor-hash = {
            type = "app";
            program = "${updateVendorHash}/bin/update-vendor-hash";
            meta = {
              mainProgram = "update-vendor-hash";
              description = "Recompute deployahVendorHash in flake.nix via nix-prefetch goModules";
            };
          };

          lint = mkApp {
            name = "lint";
            description = "Run golangci-lint";
            script = ''
              exec ${pkgs.golangci-lint}/bin/golangci-lint run ./...
            '';
          };

          lint-md = mkApp {
            name = "lint-md";
            description = "Lint Markdown files with markdownlint";
            script = ''
              exec ${pkgs.markdownlint-cli}/bin/markdownlint '**/*.md'
            '';
          };

          test-unit = mkTaggedRaceTest {
            name = "test-unit";
            description = "Run unit tests with race detector; write coverage-unit.out (build tag !integration)";
            tags = "!integration";
            coverProfile = "coverage-unit.out";
          };

          test-integration = mkTaggedRaceTest {
            name = "test-integration";
            description = "Run integration tests with race detector; write coverage-integration.out (build tag integration)";
            tags = "integration";
            coverProfile = "coverage-integration.out";
            testPackages = "./internal/testing";
          };
        };

        devShells.default = pkgs.mkShell {
          name = packageName;
          packages = devTools ++ pre-commit-check.enabledPackages;
          env = {
            GO111MODULE = "on";
            CGO_ENABLED = "1";
          };
          shellHook = ''
            ${pre-commit-check.shellHook}
            export GOROOT="${go}/share/go"
            export GOPATH="''${GOPATH:-$HOME/go}"
            export PATH="${go}/bin:$GOPATH/bin:$PATH"
            echo "Deployah dev shell — $(go version)"
          '';
        };
      }
    );
}
