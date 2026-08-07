# golangci-lint built with the project Go toolchain.
#
# nixpkgs pins buildGo126Module, so the stock binary rejects go.mod 1.27.
# Pin a commit from the draft Go 1.27 PR until a release lands:
#   https://github.com/golangci/golangci-lint/pull/6642
#   https://github.com/golangci/golangci-lint/issues/6643
# When that ships, drop this file and use pkgs.golangci-lint (or bump rev/hash
# here). A force-push of the PR branch will break the src hash; re-pin then.
{
  buildGoModule,
  fetchFromGitHub,
  installShellFiles,
  lib,
  stdenv,
  buildPackages,
}:

buildGoModule (finalAttrs: {
  pname = "golangci-lint";
  version = "2.12.2-go1.27-pr6642";

  src = fetchFromGitHub {
    owner = "golangci";
    repo = "golangci-lint";
    rev = "c4815f06852754c8daa088b684d71fd88589b175";
    hash = "sha256-oxhyDh+vvej2hjVOeLimzukE3PXmR72Zo+4Wv2UDDqo=";
  };

  vendorHash = "sha256-NNnrRtdH950rEODVPaGkMbVZ1pSl9XDFNkoSKBTrMfQ=";

  subPackages = [ "cmd/golangci-lint" ];

  nativeBuildInputs = [ installShellFiles ];

  ldflags = [
    "-s"
    "-w"
    "-X main.version=${finalAttrs.version}"
    "-X main.commit=${finalAttrs.src.rev}"
    "-X main.date=1970-01-01T00:00:00Z"
  ];

  postInstall =
    let
      golangcilintBin =
        if stdenv.buildPlatform.canExecute stdenv.hostPlatform then
          "$out"
        else
          lib.getBin buildPackages.golangci-lint;
    in
    ''
      installShellCompletion --cmd golangci-lint \
        --bash <(${golangcilintBin}/bin/golangci-lint completion bash) \
        --fish <(${golangcilintBin}/bin/golangci-lint completion fish) \
        --zsh <(${golangcilintBin}/bin/golangci-lint completion zsh)
    '';

  meta = {
    description = "Fast linters Runner for Go (Go 1.27-capable build)";
    homepage = "https://golangci-lint.run/";
    mainProgram = "golangci-lint";
    license = lib.licenses.gpl3Plus;
  };
})
