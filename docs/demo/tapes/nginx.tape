# README demo using examples/nginx
# Render with: nix run .#demo
# Requires Docker or Podman.
#
# Wait on the shell prompt after long commands: deployah's Status UI (Bubble Tea)
# can leave "Deployed" invisible to VHS screen scrapes even after a successful deploy.
# Paste + xclip shows the curl line with the real gateway port (VHS Up prints ^[[A here).
Output docs/assets/nginx.gif

Require deployah
Require bat
Require jq
Require curl
Require xclip

Source docs/demo/tapes/_common.tape

# --- off camera: workdir + local cluster ---
# Delete any prior release so the on-camera deploy is a real install.
# Unique PS1 so Wait can detect command completion reliably.
Hide
Type "cd examples/nginx"
Enter
Sleep 300ms
Type `export PS1='demo> '`
Enter
Sleep 200ms
Type `alias cat='bat --paging=never --style=numbers,grid --theme=Dracula --language=yaml'`
Enter
Sleep 200ms
Type "deployah cluster up"
Enter
Wait /demo>/
Type "deployah delete nginx local --yes >/dev/null 2>&1 || true"
Enter
Wait /demo>/
Type "clear"
Enter
Sleep 300ms
Show

# --- on camera ---
Type@70ms "cat deployah.yaml"
Enter
Sleep 2.5s

Type "deployah validate local"
Enter
Wait /demo>/
Sleep 1s

Type "deployah deploy local --yes"
Enter
Wait /demo>/
Sleep 1.5s

# Capture gateway port and put the expanded curl on the X11 clipboard for Paste.
Hide
Type `PORT=$(deployah cluster status -o json | jq -r '.access[0].address|split(":")[1]')`
Enter
Wait /demo>/
Type `printf 'curl -ksI --resolve web.127.0.0.1.nip.io:%s:127.0.0.1 https://web.127.0.0.1.nip.io:%s' "$PORT" "$PORT" | xclip -selection clipboard`
Enter
Wait /demo>/
Type "clear"
Enter
Sleep 300ms
Show

Type "deployah cluster status"
Enter
Wait /demo>/
Sleep 2s

Paste
Sleep 1.5s
Enter
Wait /demo>/
Sleep 6s
