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
`Catalog` supplies all 87 known rules, preserving reserved gaps and the 59
enabled / 28 disabled defaults. `NewCatalogRegistry` combines those contracts
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
findings must use source byte zero; missing EOF material uses a zero-width
span at `Source.Len()`; surplus terminal material uses the smallest offending
suffix after retaining the first ending. Directive diagnostics cannot be disabled.
Incomplete state
withholds ordinary callbacks and retains reliable observations and explicit
interruptions/coverage.

`Result.AnalysisRecords` retains immutable `DirectiveAnalysis` values. Their
source-local candidate/interval/suppression accessors preserve exact owner/token,
raw source and all governing configuration origins. Serialized notes identify
permission fields; suppressed lint records carry only declared attribution kind,
with no dangling directive IDs. Report omission never destroys internal analysis.
Inspection commands, observational disabled-rule evaluation, edit-span safety and
MD087 counterfactual execution remain later consumers of these observations.

## Opaque frontmatter

DocumentContext carries immutable parser.FrontmatterOptions independently of
flavor. Every original, candidate and saved-view parse uses that selection.
Root/Walk and comment/reference inventories contain the Markdown body only.
The original Source is retained for coordinates and genuine file-byte checks;
this is not a semantic sandbox for trusted rule callbacks.

Ordinary valid findings within or crossing the header are excluded after normal
finding validation. Their coverage starts at the body boundary. Catalog metadata
UsesFrontmatter explicitly enables RuleContext.Frontmatter and source-header
observations; it grants neither decoded values nor edits. MD094 still validates
all original UTF-8 bytes. A physical EOF observation can remain report-only when
a complete header ends at EOF. Unterminated headers retain located uncertainty,
withhold ordinary execution and cannot acquire completed directive authority.

RuleContext.Headings projects the parser's existing heading facts under the
original budget and operation lifetime; it neither scans source independently nor
exposes a budget. RuleContext.FrontmatterTitle is narrow semantic consumption for
an enabled frontmatter_title option, using metadata.Bind/Decode/Title on that same
original document. Lexical absence is known absence; unknown title semantics
reduce affected coverage without discarding reliable body comparisons. The option
false performs no decoding. MD001, MD025 and MD041 use these seams while retaining
UsesFrontmatter=false and body-only finding protection. Title consumption grants
no header observations, edits, preview or write authority.

RuleContext.HeadingSpacing is a narrow original-operation bridge for physical
heading endpoints and adjacent blank body runs. It requires reliable complete
syntax and the original source index, shares all original controls and withholds
a failed inventory. MD022 consumes these facts with before/after integer minima
and ordinary start-anchor directive filtering; it produces no correction.

MD023 retains a private positive leaf-indentation aggregate while the block parser
still owns each line's actual post-matched-container cursor, including partial
tabs, lazy prefixes and deep Setext continuations. Leading reference extraction
recomputes the aggregate from surviving retained paragraph parts. Only actual
heading dispatch retains one original opening/underline marker; paragraph and
table ownership publishes none. Existing HeadingFact and MD022 facts are unchanged.

Private aggregate updates use the existing metered line visits; retained markers
use metered append reservations. The narrow IndentedHeadings projection requires
the completed original parse, source and operation. It charges each copied marker,
record and amortized slice growth before publication and withholds the inventory
on failure. RuleContext joins caller and originating controls; the callback
charges each finding and retains reliable prior findings with a later error.
There is no new parse, source, allowance, whole-document rescan or public-tree copy.
This is finite application-work accounting, not a process memory or throughput
guarantee. No correction provider or write authority is added. See [MD023 policy](../../docs/rules/headings/md023.md).

MD024 adds a raw Label span to HeadingFact without changing existing fields.
First/last original Content source bounds preserve NUL mappings and multiline
Setext gaps; raw-empty ATX uses opening.End. The original-operation HeadingKey
bridge collapses parser.IsUnicodeWhitespace and uses pinned Unicode17 FoldCase
without decoding again. All scans, builders and fold expansion reserve work and
construction before allocation, with long-loop checkpoints. The callback reserves
map capacity for the complete finite heading inventory before construction, so
ordinary insertions do not grow the map or rehash prior variable-length keys;
whole-key lookup/comparison, key retention, fixed outline state and output growth
are separately charged. One linear heading pass uses actual occurrence IDs for
nearest-shallower parents, preserving all original context across suppression.
Projection failure withholds facts; a later callback error retains prior reliable
findings and explicit incomplete coverage. No fresh source, parse, allowance or
public-tree clone is created. This is finite application-work accounting, not a
process memory or throughput guarantee. See [MD024 policy](../../docs/rules/headings/md024.md).

MD025 counts matching-level headings with one saturated baseline flag and no
text comparison or container partition. Opt-in metadata is decoded once through
the original title provider; unknown semantics withhold only the first matching
body comparison and retain reliable later duplicates with incomplete coverage.
The callback charges each heading, each retained finding and potential slice
copy before append through the original compound caller/operation and sticky
work/node/elapsed controls. It uses original raw Label spans and adds no source,
parse, index, allowance, edit or write authority. See [MD025 policy](../../docs/rules/headings/md025.md).

MD026 adds HeadingTerminals, a narrow provenance projection sharing the exact
semantic walk with HeadingFact.Visible. Existing Visible and Label contracts stay
unchanged. It selects one final non-whitespace scalar before code/escape exclusion;
Exact text selects original rune bytes and transformed tokens remain atomic.
Code text participates in selection, so it cannot expose earlier punctuation.
The projection borrows actual immutable inline segments under the completed
original parse and allowance, charges tree/segment/rune visits and output growth,
and withholds all facts on failure, including final publication checks.
RuleContext joins caller and origin controls; the callback reserves scalar-set
construction, membership and finding growth, retaining reliable prior findings
with a late interruption. No source/parse/index/allowance/tree clone, correction
provider or write authority is added. This is finite application-work accounting,
not a process memory or throughput guarantee. See [MD026 policy](../../docs/rules/headings/md026.md).

MD036 adds EmphasisHeadings, a narrow completed-parser projection of actual
one-line emphasized Paragraph labels and their immediate same-parent prose
followers. It borrows immutable internal nodes and the original physical index;
actual retained container prefixes and one cumulative nonblank-line pass prevent
jumping source-only reference definitions. Candidate descendants and follower
ordinary Text use actual parser ownership, including task and raw-region
exclusions. Original outer envelopes retain both delimiter markers.
All line arrays, tree/marker/line/rune visits, binary searches and retained output
growth share the original work/node/time allowance. Caller/origin controls and
final withholding apply; callback interruptions retain reliable prior findings.
No fresh source, parser, index, budget, public-tree clone, edit or write authority
is added. Accounting bounds application work, not process RSS or throughput.
See [MD036 policy](../../docs/rules/headings/md036.md).


MD041 adds OpeningBlock, a narrow original-operation projection of the first
actual root body block after fully comment-owned introductory blocks. It borrows
actual comment/tail and raw-protection facts and scans only uncovered ASCII
whitespace gaps, without copying comment payload or a public tree. It retains
one original zero point/heading-level record; empty bodies use original EOF.
Root/comment/marker/source-gap visits and the fixed output record are charged
before work/publication. Caller/origin and sticky work/node/time controls remain
shared; any failed projection withholds its fact, including at the final check.
The callback short-circuits a sufficient opening heading before metadata decode;
otherwise opt-in title truth supplies an alternative or explicit affected
coverage. No Source, parse, index, allowance, edit or write authority is added.
Finite application accounting is not a process-memory or throughput guarantee.
See [MD041 policy](../../docs/rules/headings/md041.md).

MD043 compiles its exact single-line descriptor list through HeadingOutline after
reserving independent entry storage, without Options' eager list clone or mutable
option borrowing. An additive semantic list domain rejects invalid entries at
admission, with per-element source locations even while disabled. Empty outlines
avoid heading projection and comparison workspace.
For nonempty patterns, existing HeadingFact level/Visible/Label/Envelope facts
supply the whole source-ordered sequence. A checked suffix cost/action grid reserves
both arrays under the original allowance before allocation; cells, transitions,
chunked repeated exact byte comparisons and canonical reconstruction are charged.
Worst-case matching uses O((patterns+1) * (headings+1) * (label-bytes+1)) work
and O((patterns+1) * (headings+1)) state, rather than unbounded wildcard backtracking. Overflow or exhausted
original work/node/time controls produces explicit incomplete analysis, not a
guessed mismatch. The completed-original parser/index witness and compound
caller/origin checks also apply to empty option access. Failed alignment withholds
its observation through the final check; existing late-operation handling retains
one already justified finding. No source, parse, index, budget, public tree clone,
second Visible definition, correction or write authority is introduced. These are
finite application controls, not hard RSS or OS preemption guarantees.
See [MD043 policy](../../docs/rules/headings/md043.md).
