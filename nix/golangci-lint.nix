# golangci-lint built with the project Go toolchain.
#
# nixpkgs-unstable still pins buildGo126Module (v2.12.2), so the stock binary
# rejects go.mod 1.27. Pin the Go 1.27 release until unstable catches up, then
# drop this file and use pkgs.golangci-lint.
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
  version = "2.13.1";

  src = fetchFromGitHub {
    owner = "golangci";
    repo = "golangci-lint";
    tag = "v${finalAttrs.version}";
    hash = "sha256-8nWHSMAwIILfKMPfxWKMimxWt9N+kUsZEAaoAOPbRBE=";
  };

  vendorHash = "sha256-yZRqfht5rY2yyoZNtYttE57sB7EYjk71yrKw8dLYzNk=";

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
