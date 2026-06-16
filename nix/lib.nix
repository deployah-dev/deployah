# Shared helper functions for flake apps
{ pkgs, go }:

rec {
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
}
