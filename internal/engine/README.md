# Read-only engine contract

`engine` imports the source-preserving parser and the shared `result` model.
It has no filesystem, configuration-decoding, reporting, exit-code or write
adapter. `NewInput` copies original bytes and resolved rule settings; logical
paths identify targets without opening them. `Lint` validates the whole batch
before running any rules. Adapters own selection and intentional skips; runtime
failures, unavailable implementations and interruption never become skips.

`NewRegistry` freezes catalog metadata and a separate executable registration
map. Each canonical, nonreserved MDxxx ID has one metadata entry and at most
one executable implementation. Adding a rule changes registration, without
changing command dispatch. Metadata can describe unavailable implementations;
it does not claim the complete catalog is delivered. The complete built-in
catalog provider is #215. Options admit bool, int, string and copied string-list
domains, validated defaults, integer bounds and string choices. Unknown options
and invalid values fail even on disabled rules. Explicitly enabling an unavailable
implementation fails preflight. Default-enabled unavailable rules produce coverage
rather than clean results. Disabled and affirmatively inapplicable rules do not
produce rule coverage deficits. Applicability predicates and callbacks are trusted,
pure program code; they must not infer inapplicability from unknown context.
Configuration loading and the full configuration contract remain #124 and later.

Rules receive immutable `parser.Source`, `parser.LineIndex` and parser `Document`
objects. Returned option values are copies. All findings reference the exact
original source owner. The engine rejects foreign/absent spans, UTF-8 continuation
boundaries and CRLF interiors. Byte ranges are zero-based and half-open; public
positions are one-based lines and Unicode-scalar columns with exclusive ends.
Tabs and combining scalars each count as one column. Zero-width EOF is valid.
No coordinate is inferred from decoded or normalized text. Each uncached endpoint
conversion reserves its worst-case original-byte prefix scan against the shared
execution meter; a bounded 256-entry cache avoids repeated endpoint conversions.
The engine checks interruption before each finding and endpoint. It does not treat
result projection as unaccounted work.

Strict UTF-8 validation precedes Markdown and rule execution. Its first failing
byte produces exactly one `io` / `invalid-utf8` error with the original byte
index, `LineIndex.FailurePosition`, snapshot identity and a decoding coverage
record. MD094 enablement controls only error attribution. No normal check runs
for that file, including MD094; literal U+FFFD is valid. Other valid inputs can
continue. Acquired files entering validation, including decoding failures, appear
in `checked_files`; the per-file outcomes remain authoritative.

The current parser provider supports core CommonMark only. Every run requests
that parser context. Parser incompleteness, uncertainty or an unsupported
requested flavor produces explicit coverage. Syntax-dependent callbacks run
only with affirmative complete context. Source-only callbacks cannot access an
uncertain tree. A rule that lacks another prerequisite returns an incomplete
reason, optionally with findings whose conclusions remain justified. The richer
structured coverage interface is #216; it extends this shared model.

One `parser.OperationBudget` covers all documents, parser work and rule/context
providers. Supply the same budget when other operation work has already begun;
omitting it creates the finite default profile. Rule loops and external context
providers must call `RuleContext.Checkpoint` and honor its returned error.
Known oversized inputs fail preflight; runtime budget exhaustion, cancellation
and deadlines produce separate interruption records and unassessed coverage.
No further callbacks or targets start after interruption. Reliable findings
returned before stopping remain in the partial result when they fit the reserved
reporting allowance. After execution interrupts, one operation-wide reserve uses
the default finite work profile and the accepted additional two-second reporting
time; it never resets per rule or target. If projection cannot finish in that
reserve, the result retains projected whole records and an explicit `/diagnostics`
omission with the exact unreturned count. Already observed findings are never
unconditionally discarded merely because their callback exhausted execution.
Known unvisited targets
have `analysis=not-started`, rather than invented successful empty checks.

The result is the read-only subset of the accepted
[shared v1 schema](../../docs/contracts/results/result.schema.json). All required
top-level arrays are present, including reserved empty fix/inspection arrays.
Each acquired file and diagnostic identifies original bytes by SHA-256 and byte
count. Canonical sorting precedes deterministic record-ID assignment and cause
linking. Path/rule/reason indexes link records in time proportional to records and
emitted references, without Cartesian scans over files, errors and coverage.
File, diagnostic, error, coverage and interruption state is retained
independently; adapters select exits and presentation. This engine implements
no suppression, resource discovery, correction proposals, output delivery or CLI
integration (#127). A fix metadata declaration alone never makes a finding
eligible: `fix_available` also requires an implemented correction and a rule's
positive context-specific safety/permission assessment. Lint creates no edits.

## Verification

Run `go test ./...`, `go vet ./...` and `sh tools/lint.sh`. Contract tests cover
registration/schema rejection, metadata and input isolation, exact coordinates,
UTF-8 gating, interrupted partial results, unsupported parser coverage and
independence from input/registration order. To validate actual clean, violation,
invalid-UTF-8, interrupted and omitted-diagnostic producer envelopes against the normative schema
and its cross-record semantic validator:

```sh
python3 -m venv /tmp/mdchk-result-schema
/tmp/mdchk-result-schema/bin/pip install -r docs/contracts/results/requirements.txt
MDCHK_RESULT_VALIDATOR_PYTHON=/tmp/mdchk-result-schema/bin/python \
  go test -count=1 -run TestProducerSchema -v ./internal/engine
```

The Python validator is verification tooling, not an engine runtime dependency.
