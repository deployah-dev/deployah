{
  buildGoModule,
  lib,
  deployahVersion,
  src,
  vendorHash,
}:

buildGoModule {
  pname = "deployah";
  version = deployahVersion;
  inherit src vendorHash;

  goSum = builtins.readFile "${src}/go.sum";

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
}
