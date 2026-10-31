#!/usr/bin/env bash
# .go-version is the human-edited toolchain source; Go requires literal module directives.
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
version="$(tr -d '[:space:]' < "${root}/.go-version")"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "Invalid .go-version: $version" >&2; exit 1; }
if [[ -n "${GO_VERSION:-}" && "$GO_VERSION" != "$version" ]]; then
  echo "Go toolchain mismatch: GO_VERSION must match .go-version ($version); edit that file to change the pin." >&2
  exit 1
fi
while IFS= read -r module; do
  [[ -n "$module" ]] || continue
  actual="$(awk '$1 == "go" { print $2; exit }' "${root}/${module}")"
  [[ "$actual" == "$version" ]] || { echo "$module must mirror .go-version ($version), found $actual" >&2; exit 1; }
  toolchain="$(awk '$1 == "toolchain" { print $2; exit }' "${root}/${module}")"
  [[ -z "$toolchain" || "$toolchain" == "go$version" ]] || { echo "$module toolchain must mirror .go-version ($version)" >&2; exit 1; }
done < <(if [[ -e "$root/.git" ]]; then
  git -C "$root" ls-files -- 'go.mod' '**/go.mod' 'go.work' '**/go.work'
else
  printf '%s\n' go.mod
fi)
echo "Go version contract verified from .go-version: $version"
