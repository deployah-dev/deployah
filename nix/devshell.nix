# Development shell for deployah
{
  pkgs,
  go,
  gopls,
  pre-commit-check,
  golangci-lint,
}:

let
  # Prefer flake-pinned Go tools over a user GOPATH install.
  devTools = [
    go
    gopls
    golangci-lint
  ]
  ++ (with pkgs; [
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
    # gotools on PATH leaks GOTOOLDIR to its own bin; go must use GOROOT's tools.
    unset GOTOOLDIR
    # Real ELF shims (not bash). vscode-go runs these as `go env` / gopls LSP.
    mkdir -p .direnv/bin
    ln -sfn "${go}/bin/go" .direnv/bin/go
    ln -sfn "${gopls}/bin/gopls" .direnv/bin/gopls
    ln -sfn "${golangci-lint}/bin/golangci-lint" .direnv/bin/golangci-lint
    # .direnv/bin first so shell and editor resolve the same binaries.
    # GOPATH/bin last so local installs do not shadow the pinned toolchain.
    export PATH="$PWD/.direnv/bin:$PATH:$GOPATH/bin"
    echo "Deployah dev shell — $(go version)" >&2
  '';
}
