#!/bin/sh
# Install outside the root module; never select a tool from PATH.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/mdchk-lint.XXXXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM

export GOTOOLCHAIN=local GOWORK=off GOFLAGS=-mod=readonly
# Version-qualified install uses the tool's upstream module graph, not mdchk's.
# Clear only build flags for installation; analysis retains readonly module mode.
GOBIN="$scratch" GOFLAGS= go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
cd "$root"
"$scratch/golangci-lint" config verify --config "$root/.golangci.yml"
case "${1:-}" in
  '') "$scratch/golangci-lint" run --config "$root/.golangci.yml" ./... ;;
  --self-test) python3 -B tools/test_lint.py "$scratch/golangci-lint" ;;
  *) echo 'usage: tools/lint.sh [--self-test]' >&2; exit 2 ;;
esac
