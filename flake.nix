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
      packageName = "deployah";

      clusterName = "deployah";
      kubeConfigPath = "./.kubeconfig";
      kindContext = "kind-${clusterName}";
      go = pkgs.go_1_23;
    in {
      formatter = pkgs.alejandra;
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
        '';
      };
    });
}
