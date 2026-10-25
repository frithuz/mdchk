#!/bin/sh
# Python owns installation, snapshots, process groups, deadlines and evidence.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
exec python3 -B "$root/tools/mutation.py" "$@"
