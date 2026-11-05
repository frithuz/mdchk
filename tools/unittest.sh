#!/bin/sh
# Run Python unittest quietly: buffered output and only the totals, unless
# DEBUG is set to a non-empty value other than 0.
set -eu

case "${DEBUG:-0}" in
  0) exec python3 -B -m unittest "$@" -q -b ;;
  *) exec python3 -B -m unittest "$@" -v ;;
esac
