# Development shell for deployah
{
  pkgs,
  go,
  pre-commit-check,
}:

let
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
    jq
    yq-go
  ];
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
    export PATH="${go}/bin:$GOPATH/bin:$PATH"
    echo "Deployah dev shell — $(go version)"
  '';
}
