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
`Catalog` supplies all 89 known rules, preserving reserved gaps and the 59
enabled / 30 disabled defaults. `NewCatalogRegistry` combines those contracts
with only the supplied callbacks; planned corrections remain metadata. Tests and
other trusted consumers may still provide a smaller metadata set to `NewRegistry`.
Options admit bool, int, finite number, string and copied string-list domains,
validated defaults, bounds, choices and named semantic domains. Unknown options
and invalid values fail even on disabled rules.

`ResolveSettings` returns owned defaults/options and records `Explicit` from
rule-map membership, independently of `Enabled`, `AllowDirectives` (default true),
and `Available`. An explicit empty mapping preserves the individual default:
MD001 remains enabled and MD013 remains disabled. `SettingFromMapping` checks
shared boolean fields and rejects null rule mappings. Syntax adapters must first
reject duplicate keys; the catalog boundary does not decode YAML. The config
package aliases the engine's `RuleSetting` to preserve this shared contract.

`SelectRules` retains the same validated option results and explicit membership
in an immutable `RuleSelection`, bound to its registry. Its `Plan` applies
admitted directive denials without synthesizing explicit entries. Plan-created
inputs retain that selection, and `ExecutionPlan.Lint` delegates to the same
registry execution and finalization; semantic providers are not re-run during
Markdown analysis. Inputs from another plan or registry fail before analysis.
Configuration adapters retain the complete admitted policy/context snapshot;
mutable resolution views never become execution authority.

The catalog proves table rules MD055/056/058/060/075 inapplicable to CommonMark,
and footnote rules MD066/067/068 inapplicable to CommonMark/GFM. Unknown or absent
dialect context stays conservative. Resource/integration context absence does
not imply inapplicability: callbacks report unmet prerequisites as coverage.

MD073 validates complete single-line comment markers, distinctness and ordered
heading bounds. MD095 `allowed_hosts` carries the pinned Unicode 17.0.0 semantic
domain. A nonempty list requires `ValidationProviders.ValidateHostname` with the
complete accepted nontransitional UTS #46 profile; no such provider is supplied
by this catalog implementation. Without it resolution fails with
`ErrHostnameValidationUnavailable`, including on disabled rules. Empty/default
lists need no hostname processing. The catalog neither normalizes input nor
substitutes a structural approximation for semantic validation.

An explicitly configured, effectively enabled unavailable implementation fails
lint preflight. Unmentioned default-enabled unavailable rules produce coverage
rather than clean results. Disabled and affirmatively inapplicable rules do not
produce rule coverage deficits. Applicability predicates and callbacks are trusted,
pure program code; they must not infer inapplicability from unknown context.
Operation-wide capability admission and decoding belong to the config adapters.

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
reason, optionally with findings whose conclusions remain justified. Coverage uses the shared `result.Coverage` record with required reason/message
and optional catalog rule, path, public range, exact snapshot and cause links.
Unavailable build-wide work needs no fabricated source attribution. Extension-like
text in CommonMark follows ordinary CommonMark interpretation.

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
An already-cancelled external context is observed before a target enters
validation, hashing or UTF-8 decoding. It creates no checked-file entry or
snapshot for that target. Known unvisited targets
have `analysis=not-started`, rather than invented successful empty checks.

The result is the read-only subset of the accepted
[shared v1 schema](../../docs/contracts/results/result.schema.json). All required
top-level arrays are present, including reserved empty fix/inspection arrays.
Each acquired file and diagnostic identifies original bytes by SHA-256 and byte
count. Error/interruption IDs are assigned canonically before cause linking. Coverage
then sorts by absent-first path/start, rule and reason, with remaining semantic
fields as tie-breaks. `Result.CanonicalizeCoverage` treats cause references as a
set and collapses identical observations independent of generated IDs, retaining
distinct ranges, snapshots and causes. Coverage IDs and per-file links are
assigned only after deduplication. Path/rule/reason indexes link records in time proportional to records and
emitted references, without Cartesian scans over files, errors and coverage.
`FinalizeLintResult` is the authoritative finalizer for engine observations
combined with adapter acquisition records. Adapters may remap paths and append
records, then call it again to regenerate canonical IDs, cause links, per-file
references and outcome. Explicit cause IDs must be unique across the combined
errors and interruptions; new unreferenced records may have empty IDs. Checked
paths, file outcomes and intentional skips also receive canonical ordering.
File, diagnostic, error, coverage and interruption state is retained
independently; adapters select exits and presentation. The shared
`Result.FinalizeLintFixOutcome` applies operational error > incomplete >
violations > clean precedence only to lint/fix. Coverage, interruptions or
omissions force `complete=false`; observations remain available with errors.
Inspect and configuration validation retain operation-specific status handling. This engine implements
no resource discovery, correction proposals or output delivery.
The CLI adapter owns argument handling, configuration, selection/acquisition,
execution-plan dispatch, and presentation. A fix metadata declaration alone never makes a finding
eligible: `fix_available` also requires an implemented correction and a rule's
positive context-specific safety/permission assessment. Lint creates no edits.

## Verification

Run `go test ./...`, `go vet ./...` and `sh tools/lint.sh`. Contract tests cover
registration/schema rejection, metadata and input isolation, exact coordinates,
UTF-8 gating, interrupted partial results, unsupported parser coverage and
independence from input/registration order. To validate actual clean, violation,
invalid-UTF-8, interrupted, omitted-diagnostic and partial operational-error
producer envelopes against the normative schema
and its cross-record semantic validator:

```sh
python3 -m venv /tmp/mdchk-result-schema
/tmp/mdchk-result-schema/bin/pip install -r docs/contracts/results/requirements.txt
MDCHK_RESULT_VALIDATOR_PYTHON=/tmp/mdchk-result-schema/bin/python \
  go test -count=1 -run TestProducerSchema -v ./internal/engine
```

The Python validator is verification tooling, not an engine runtime dependency.

## Directive execution

`ProcessDirectives` consumes the completed parser handoff and the same operation
budget. Configuration admission binds an immutable `DirectivePolicy` snapshot;
the core imports no config decoder. The compatibility `internal/directives`
facade invokes this same implementation. Direct `Registry.Lint` inputs use their
owned resolved rule settings and global permission enabled.

Intervals control scheduling and missing-rule coverage before callback execution.
An enabled EOF-only interval requires coverage; disabled-throughout ordinary rules
can skip execution. Callbacks retain full original document/context/options.
Findings are validated first, then filtered by start anchor. Default file-wide
findings must use source byte zero; EOF-specific findings must use a zero-width
span at `Source.Len()`. Directive diagnostics cannot be disabled. Incomplete state
withholds ordinary callbacks and retains reliable observations and explicit
interruptions/coverage.

`Result.AnalysisRecords` retains immutable `DirectiveAnalysis` values. Their
source-local candidate/interval/suppression accessors preserve exact owner/token,
raw source and all governing configuration origins. Serialized notes identify
permission fields; suppressed lint records carry only declared attribution kind,
with no dangling directive IDs. Report omission never destroys internal analysis.
Inspection commands, observational disabled-rule evaluation, edit-span safety and
MD087 counterfactual execution remain later consumers of these observations.
