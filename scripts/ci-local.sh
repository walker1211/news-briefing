#!/usr/bin/env bash
set -euo pipefail

mode=${1:-clean}
script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/.." && pwd)

run_secret_scan() {
  local dir=$1

  printf '==> secret scan\n'
  "$repo_root/scripts/secret-scan.sh" --current --current-root "$dir" --history
}

run_checks() {
  local dir=$1

  printf '==> gofmt\n'
  unformatted=$(gofmt -l "$dir")
  if [[ -n "$unformatted" ]]; then
    printf '%s\n' "$unformatted"
    exit 1
  fi

  printf '==> go vet\n'
  go -C "$dir" vet ./...

  printf '==> go test\n'
  TZ=UTC go -C "$dir" test ./...
}

case "$mode" in
  clean)
    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT
    git -C "$repo_root" ls-files -z | tar -C "$repo_root" --null -T - -cf - | tar -x -C "$tmpdir"
    run_secret_scan "$tmpdir"
    run_checks "$tmpdir"
    ;;
  worktree)
    run_secret_scan "$repo_root"
    run_checks "$repo_root"
    ;;
  *)
    printf 'usage: %s [clean|worktree]\n' "$0" >&2
    exit 2
    ;;
esac
