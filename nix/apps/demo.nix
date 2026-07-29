# Demo GIF rendering and CDN publish apps
{
  pkgs,
  lib,
  deployah,
}:

{
  demo = lib.mkApp {
    name = "demo";
    description = "Render docs/demo/tapes/*.tape into docs/assets/ (needs Docker or Podman)";
    runtimeInputs = [
      deployah
      pkgs.vhs
      pkgs.gifsicle
      pkgs.bat
      pkgs.jq
      pkgs.curl
      pkgs.xclip
      pkgs.xvfb-run
    ];
    script = ''
      set -euo pipefail

      if [ ! -d docs/demo/tapes ]; then
        echo "run from the repo root (docs/demo/tapes not found)" >&2
        exit 1
      fi

      mkdir -p docs/assets

      shopt -s nullglob
      tapes=(docs/demo/tapes/*.tape)
      if [ ''${#tapes[@]} -eq 0 ]; then
        echo "no tape files in docs/demo/tapes/" >&2
        exit 1
      fi

      for tape in "''${tapes[@]}"; do
        base="$(basename "$tape")"
        # Shared snippets (e.g. _common.tape) are Source'd, not rendered alone.
        case "$base" in
          _*) continue ;;
        esac
        echo "Rendering $tape ..."
        # xvfb gives DISPLAY so xclip + VHS Paste share a clipboard.
        xvfb-run -a vhs "$tape"
      done

      for gif in docs/assets/*.gif; do
        echo "Optimizing $gif ..."
        gifsicle -O3 --lossy=40 --colors 128 "$gif" -o "$gif"
      done

      echo "Done. GIFs are under docs/assets/"
    '';
  };

  publish-demo = lib.mkApp {
    name = "publish-demo";
    description = "Sync docs/assets/ to R2 (requires R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY, R2_ENDPOINT_URL, R2_BUCKET, R2_DEST_PATH)";
    runtimeInputs = [ pkgs.awscli2 ];
    script = ''
      set -euo pipefail

      : "''${R2_ACCESS_KEY_ID:?R2_ACCESS_KEY_ID is required}"
      : "''${R2_SECRET_ACCESS_KEY:?R2_SECRET_ACCESS_KEY is required}"
      : "''${R2_ENDPOINT_URL:?R2_ENDPOINT_URL is required}"
      : "''${R2_BUCKET:?R2_BUCKET is required}"
      : "''${R2_DEST_PATH:?R2_DEST_PATH is required}"

      if [ ! -d docs/assets ]; then
        echo "docs/assets/ missing; run nix run .#demo first" >&2
        exit 1
      fi

      export AWS_ACCESS_KEY_ID="$R2_ACCESS_KEY_ID"
      export AWS_SECRET_ACCESS_KEY="$R2_SECRET_ACCESS_KEY"
      export AWS_DEFAULT_REGION="auto"

      exec aws s3 sync docs/assets/ \
        "s3://$R2_BUCKET/$R2_DEST_PATH" \
        --endpoint-url "$R2_ENDPOINT_URL" \
        --exclude ".gitkeep"
    '';
  };
}
