# GFM parser and consumer verification

The verified parser target is CommonMark **0.31.2** plus all five published GFM
extensions. The retained extension page, retrieved 2026-10-14, is 508,310 bytes,
SHA-256 `b153d814fdfc8624bb6da7449162c1cd707a637f7d1c1b636eb44b9cf63fa220`.
Its `0.29-gfm` label does not identify the selected core. The
[compatibility contract](gfm-compatibility.md) retains explicit modern-core and
extension composition interpretations; optional comparison programs supply no
expectations.

All **652** unchanged CommonMark examples pass normalized and strict gates.
All **2,117** approved GFM units pass: 677 published inputs (649 base and 28
extension inputs) under three scopes, plus 43 supplemental inputs under two.
There are zero failed, errored, pending, filtered or omitted approved units.
Original base comparisons select the reviewed core target before execution;
original extension comparisons retain their section target. Immutable historical
raw comparisons remain 2,115 pass and two explicit failures, original 649/650,
whose old HTML comment interpretation differs from CommonMark 0.31.2.
`--require-complete` still demands raw historical equality and exits 1;
`--require-approved-profile` demands the entire modern target and exits 0.
Integrity-only `--check` truthfully reports NOT-RUN.

```sh
python3 -B tools/commonmark_conformance.py --require-complete
python3 -B tools/commonmark_conformance.py --strict --require-complete
python3 -B tools/gfm_corpus.py --check
python3 -B tools/gfm_profile.py --check
python3 -B tools/gfm_profile.py --run --require-approved-profile
python3 -B -m unittest discover -s tools -p 'test_gfm*.py' -v
go test ./...
go vet ./...
go test -race ./...
go test -tags mdchk_conformance ./internal/conformance/... -count=1
go test ./internal/parser -run '^TestGFMVerified' -count=1 -v
```

`gfm_verification_test.go` directly parses independently authored mixed literals.
It checks ordered ancestors, original task X/tab spelling and remaining space,
raw/short/excess table cells, escaped pipe and alignment, nested Strike/emphasis,
link origins and source versus synthetic destinations, first reference binding,
affected HTML angles, and exact opaque/comment bytes including excess cells.
LF/CRLF, Unicode and tabs are paired; retained extension tests additionally cover
NUL normalization, partial tabs, discontinuous container mappings, malformed
boundaries, odd/even escape precedence and every link origin. Mutated returned
handles, properties and source copies cannot alter stored facts. Equal-byte
foreign sources, foreign operations, constructed models and incomplete parses
cannot grant source-consumer authority. Handles are detached shallow copies;
these checks do not require recursively cloned trees.

Excluded footnote spelling includes a legitimate ordinary reference definition
and link. Unresolved brackets, alerts in quotes, emoji/dollar notation and
YAML/TOML-looking headers receive positive ordinary core interpretations in both
profiles, without uncertainty. All seven closed scopes run with synchronized
concurrent starts and independent/shared finite operations; public core remains
identical to its pre-profile baseline. A tagged CLI test runs real production
execution before and after every closed scope: default/explicit GFM are admitted;
invented partial profiles/extension keys and enabled frontmatter remain rejected.

Mixed resource tests measure accounting boundaries at used-1/used/used+1, carry
real protection charges and core/integrated reparses through one operation, and
check sticky work/total/time exhaustion. Known depth-six chains count table,
row, cell, quote/list/item and Strike/Link; task/filter metadata adds no depth.
Deterministic context checkpoints sweep cancellation/deadline through publication.
Finite doubled table/delimiter/URL/HTML families record actual work/construction
and Complete versus named limit outcomes. Existing adapter tests additionally
charge actual rendering/export work; manual charges are not adapter execution.
These are finite tests, not universal complexity, preemption or memory proofs.

`FuzzGFMProfile` seeds retained extension inputs and interacting/malformed source.
It checks source ownership, immutable input, deterministic fresh-operation
outcomes, actual table/task/Strike/link/filter metadata and explicit exhaustion.
A constrained generated layout has independently known separator/marker facts,
so a consistently wrong parse can fail beyond determinism or panic detection.
Inputs over 64 KiB are explicitly skipped; each arbitrary parse has finite
2,000,000 work, 30,000 document-construction and 100,000 cumulative-construction
allowances. Native fuzz smoke is 10 seconds, two workers, timeout two minutes;
CI also schedules five-minute campaigns. The six existing core targets remain.
Race results cover executed concurrent paths only. See [contributor commands](../contributing.md#verify-the-private-integrated-gfm-parser)
and the [measured baseline](parser-performance.md#integrated-gfm-initial-baseline).

Offline reproduction requires Go 1.27.2+, Python 3.9+ and already installed module
and toolchain caches. Corpus/profile generators use retained normative bytes;
no expected output is generated from parser observations. Repeated generation
with outputs absent reproduces exact bytes/counts; successful and rejected check
modes preserve bytes and mtimes. A clean nonignored-file export including new
tests reproduces full reports and independent checks from unrelated CWD under
process-level network denial, with local toolchain and proxy/sumdb/workspace off.
This does not claim an empty dependency cache can build without acquisition.

Public `Parse`/`ParseWithBudget` remain core-only. The closed tagged bridge is
verification infrastructure. `ParseGFMWithBudget` exposes only the whole profile
within the internal package boundary and shares the existing parse lifecycle.
Default/explicit GFM now drive actual lint, inspect and fix, including candidate
and saved analysis, with the same immutable plan and residual operation budget.

Permanent consumer fixtures assert independently specified LF/CRLF/UTF-8 source
facts, exact diagnostic coordinates, genuine versus opaque directives, protected
edits, foreign/partial authority refusal and controlled preservation/idempotence.
Inspection projects original table cells/counts/separators/padding, task tokens,
strike markers, destination provenance and filtered HTML facts; selected views
retain their original ownership. Actual producer schema/semantic controls reject
corrupted extension facts. Compiled MD047 tests cover preview, supported native
saved bytes and ordinary unsupported-platform refusal.

Named production rules (#236), frontmatter, MCP and production rendering
remain separate. Full catalog defaults still yield incomplete/15 for missing
checks; explicitly enabling an unavailable rule fails configuration/12. Reduced
MD047 configurations can complete with either supported flavor. These checks do
not claim historical 0.29 equality, GitHub website/sanitization parity or whole
product readiness.
