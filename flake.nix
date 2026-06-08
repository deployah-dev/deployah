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
        lib = nixpkgs.lib;
        packageName = "deployah";
        deployahVersion = "dev";
        go = pkgs.go;

        buildGoModule' = pkgs.buildGoModule.override { inherit go; };

        deployah = buildGoModule' {
          pname = packageName;
          version = deployahVersion;
          src = ./.;

          vendorHash = "sha256-2v/ic+v0yLDH1yhnYpG6y0w6pjg+Pn8kEZpsUdPd4Lk=";

          ldflags = [
            "-s"
            "-w"
            "-X deployah.dev/deployah/internal/cmd.version=${deployahVersion}"
          ];

          doCheck = false;

          env.CGO_ENABLED = "0";
          GOWORK = "off";

          meta = {
            mainProgram = "deployah";
            description = "Deployah - A CLI tool for deploying applications to Kubernetes";
            homepage = "https://github.com/deployah-dev/deployah";
            license = lib.licenses.asl20;
          };
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
