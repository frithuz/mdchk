#!/usr/bin/env bash
# The repo's checks. Exit 0 only when the branch is fit to review.
#
# The gate runs this script from the main checkout with the job's worktree as
# the working directory. Keep it fast, deterministic and non-interactive: no
# watch modes, no prompts, nothing that rewrites files.
#
# These mirror the Linux `test` job in .github/workflows/go.yml. The race,
# fuzz, cross-OS native and vulnerability jobs stay in CI: they are slow or
# depend on the host and on outside data, not only on the branch.
set -euo pipefail

export GOTOOLCHAIN=local GOWORK=off GOFLAGS=-mod=readonly

go mod download

node tools/resource_schema.mjs
go run ./tools/testrun ./...
CGO_ENABLED=0 go build ./...
for target in linux/amd64 linux/arm64 darwin/arm64 darwin/amd64 windows/amd64 windows/arm64 freebsd/amd64; do
  echo "== $target"
  GOOS=${target%/*} GOARCH=${target#*/} CGO_ENABLED=0 go vet ./...
done
go run ./tools/testrun -tags mdchk_conformance ./internal/conformance/... -count=1
go run ./tools/testrun -tags mdchk_conformance ./internal/cli -run '^TestGFMPrivateScopesPreserveProductionAdmission$' -count=1
go vet ./...
sh tools/lint.sh
sh tools/lint.sh --self-test

python3 -B tools/commonmark_corpus.py --check
python3 -B tools/gfm_corpus.py --check
sh tools/unittest.sh discover -s tools -p 'test_gfm_corpus.py'
python3 -B tools/gfm_profile.py --check
sh tools/unittest.sh discover -s tools -p 'test_gfm_profile.py'
sh tools/unittest.sh discover -s tools -p 'test_gfm_conformance.py'
python3 -B tools/gfm_profile.py --run --require-approved-profile
python3 -B tools/unicode_casefold.py --check
python3 -B tools/unicode_idna.py --check
python3 -B tools/html_entities.py --check
sh tools/unittest.sh discover -s tools -p 'test_commonmark_corpus.py'
python3 -B tools/commonmark_conformance.py --require-complete
sh tools/unittest.sh discover -s tools -p 'test_commonmark_conformance.py'
