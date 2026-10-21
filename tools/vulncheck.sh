#!/bin/sh
# Install outside the root module; never select a tool from PATH.
set -eu

case "${1:-}" in
  ''|--self-test) ;;
  *) echo 'usage: tools/vulncheck.sh [--self-test]' >&2; exit 2 ;;
esac

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/mdchk-vulncheck.XXXXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM

export GOTOOLCHAIN=local GOWORK=off GOFLAGS=-mod=readonly
# The version-qualified install uses the tool's upstream module graph.
GOBIN="$scratch" GOFLAGS= go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
cd "$root"
case "${1:-}" in
  '') "$scratch/govulncheck" -format=text -scan=symbol -test -show=traces,version ./... ;;
  --self-test) python3 -B tools/test_vulncheck.py "$scratch/govulncheck" ;;
esac
