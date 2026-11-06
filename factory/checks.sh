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
#
# The result contract checks at the end go beyond that job, which doesn't run
# them, so a gate can catch contract drift before review.
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

# Validate the result contract, then the JSON producers emit against it. A fresh
# environment and -count=1 are deliberate: the tests reach the schema through
# Python, so Go's test cache doesn't see a contract change. The compiled tests
# are limited to one set per operation, because every acceptance test starts the
# validator and the whole of ./cmd/mdchk passes the 10-minute test timeout.
validator_env=$(mktemp -d)
trap 'rm -rf "$validator_env"' EXIT
python3 -m venv "$validator_env"
"$validator_env/bin/pip" install --quiet --disable-pip-version-check -r docs/contracts/results/requirements.txt
"$validator_env/bin/python" -B docs/contracts/results/validate.py
export MDCHK_RESULT_VALIDATOR_PYTHON="$validator_env/bin/python"
go run ./tools/testrun ./internal/cli ./internal/config ./internal/engine ./internal/inspect ./internal/resource -count=1
go run ./tools/testrun ./cmd/mdchk -run '^(TestConfiguration|TestCompiledInspection|TestCompiledMetadata|TestCompiledCLIReadiness|TestCLIReadiness|TestCompiledFix)' -count=1
