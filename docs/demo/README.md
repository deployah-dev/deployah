# Demo GIFs

Terminal demos for the README and docs. Tapes live under
[`tapes/`](tapes/). Rendered GIFs go to [`../assets/`](../assets/) and are
published to `https://deployah.dev/demos/`.

Shared VHS settings live in [`tapes/_common.tape`](tapes/_common.tape)
(`Source`d by each demo; files starting with `_` are not rendered on their
own).

Examples under [`examples/`](../../examples/) are general-purpose starters.
Demos reuse them (for example [`examples/nginx`](../../examples/nginx)) so the
recording matches what people run by hand.

## Render locally

Docker or Podman must be running. From the repo root:

```sh
nix run .#demo
```

That runs every `docs/demo/tapes/*.tape` (except `_*.tape`) with
[VHS](https://github.com/charmbracelet/vhs) under `xvfb-run`, and writes
optimized GIFs to `docs/assets/`. Tapes may use
`bat`, `jq`, `curl`, and `xclip` (provided by `nix run .#demo`).

`xvfb-run` supplies a DISPLAY so `xclip` and VHS `Paste` share a clipboard
(used to show the curl line with the real gateway port).

## Publish

Upload `docs/assets/` to the CDN (Cloudflare R2 or any S3-compatible bucket):

```sh
export R2_ACCESS_KEY_ID=...
export R2_SECRET_ACCESS_KEY=...
export R2_ENDPOINT_URL=...
export R2_BUCKET=deployah
export R2_DEST_PATH=demos/
nix run .#publish-demo
```

The README embeds `https://deployah.dev/demos/nginx.gif`. After the first
publish, that URL should resolve.

## Adding a tape

1. Prefer an existing example under `examples/`, or add a new one.
2. Add `docs/demo/tapes/<name>.tape` with `Output docs/assets/<name>.gif` and
   `Source docs/demo/tapes/_common.tape`.
3. Run `nix run .#demo` and check the GIF.
4. Run `nix run .#publish-demo` when you want the CDN updated.
