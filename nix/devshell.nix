# Development shell for deployah
{
  pkgs,
  go,
  pre-commit-check,
  golangci-lint,
}:

let
  # Prefer the flake-pinned golangci-lint over a user GOPATH install.
  devTools = [
    go
    golangci-lint
  ]
  ++ (with pkgs; [
    gopls
    gotools
    markdownlint-cli
    delve
    git
    kind
    kubectl
    kubecolor
    kubernetes-helm
    jq
    yq-go
    vhs
    gifsicle
    bat
    xclip
    xvfb-run
  ]);
in
pkgs.mkShell {
  name = "deployah";
  packages = devTools ++ pre-commit-check.enabledPackages;
  env = {
    GO111MODULE = "on";
    CGO_ENABLED = "1";
  };
  shellHook = ''
    ${pre-commit-check.shellHook}
    export GOROOT="${go}/share/go"
    export GOPATH="''${GOPATH:-$HOME/go}"
    # Flake Go + golangci-lint first; GOPATH/bin last so local installs do not
    # shadow the pinned toolchain (Go 1.27-capable golangci-lint).
    export PATH="${golangci-lint}/bin:${go}/bin:$PATH:$GOPATH/bin"
    echo "Deployah dev shell — $(go version)"
  '';
}
