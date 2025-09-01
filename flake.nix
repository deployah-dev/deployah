{
  description = "Deployah - A CLI tool for deploying applications to Kubernetes";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = {
    self,
    nixpkgs,
    flake-utils,
  }:
    flake-utils.lib.eachDefaultSystem (system: let
      pkgs = nixpkgs.legacyPackages.${system};
      lib = nixpkgs.lib;
      packageName = "deployah";

      clusterName = "deployah";
      kubeConfigPath = "./.kubeconfig";
      kindContext = "kind-${clusterName}";
      go = pkgs.go_1_25;
      
      # Ensure buildGoModule (including its go-modules fetcher) uses Go 1.25
      buildGoModule125 = pkgs.buildGoModule.override { go = go; };
      
      deployah = buildGoModule125 {
        pname = packageName;
        version = "0.1.0";
        src = ./.;
        
        vendorHash = "sha256-ZIVqNqjqQFpSYwAHIKTgf3MEea/0ggdF/eQ2sMSm0i4=";

        subPackages = [ "cmd/deployah" ];
        
        ldflags = [
          "-s"
          "-w"
          "-X main.version=${"0.1.0"}"
        ];

        # Disable tests during Nix build if they are not hermetic
        doCheck = false;

        # Improve reproducibility and avoid local workspace interference
        env.CGO_ENABLED = "0";
        GOWORK = "off";

        meta = {
          mainProgram = "deployah";
          description = "Deployah - A CLI tool for deploying applications to Kubernetes";
          homepage = "https://github.com/deployah-dev/deployah";
          license = lib.licenses.asl20;
        };
      };
    in {
      formatter = pkgs.alejandra;
      
      packages = {
        default = deployah;
        ${packageName} = deployah;
      };
      
      apps = {
        default = flake-utils.lib.mkApp {
          drv = deployah;
          exePath = "/bin/deployah";
        };
      };
      
      devShells.default = pkgs.mkShell {
        name = packageName;
        buildInputs = with pkgs; [
          go
          kind
          revive
          kubectl
          kubecolor
          kubernetes-helm
          golangci-lint
          gopls
          just
          jq
          yq-go
        ];

        shellHook = ''
          set -euo pipefail
          echo "Welcome to ${packageName} development environment!"

          alias kubectl="kubecolor"

          # Check and create kind cluster if not exists
          if ! kind get clusters | grep -q ${clusterName}; then
            kind create cluster --name ${clusterName} --wait 60s --kubeconfig ${kubeConfigPath}
          fi

          export KUBECONFIG=${kubeConfigPath}
          export HELM_KUBECONTEXT=${kindContext}

          # Only prompt/cleanup if interactive
          if [ -t 0 ]; then
            cleanup() {
              echo "Cleaning up ${packageName} development environment..."

              read -p "Do you want to delete the kind cluster? (y/n): " choice
              case $choice in
                [Yy]*)
                  kind delete cluster --name ${clusterName} --kubeconfig ${kubeConfigPath}
                  rm -rf ${kubeConfigPath}
                  ;;
              esac
            }
            trap cleanup EXIT
          fi
        '';
      };
    });
}
