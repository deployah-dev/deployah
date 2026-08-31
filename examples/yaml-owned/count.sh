#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")" && pwd)

count() {
  local dir=$1
  find "$dir" -type f \( -name '*.yaml' -o -name '*.yml' -o -name '*.tpl' \) -print0 \
    | xargs -0 grep -hve '^[[:space:]]*$' \
    | wc -l
}

printf 'deployah    %s\n' "$(count "$root/deployah")"
printf 'kubernetes  %s\n' "$(count "$root/kubernetes")"
printf 'kustomize   %s\n' "$(count "$root/kustomize")"
printf 'helm        %s\n' "$(count "$root/helm")"
