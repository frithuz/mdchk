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

MD063 reuses the shared heading Visible walk with independently owned scalar,
code-owner and exact/atomic source-token records. Proper-name list storage is
reserved before copying; immutable string bytes may be borrowed without exposing
mutable engine options. Name protection is settled before selecting a dependent
initial, with repeated scalar comparisons, protection writes, word visits and
finding records charged to the original operation. Worst-case matching work is
O(heading-scalars * names * name-scalars); retained state is linear in projected
heading scalars and option-list entries, bounded by the original work/node/time
allowance rather than a new name cap. Caller/origin cancellation and completed
private parse/source/index identity remain required, including final withholding.
Already justified earlier-heading findings follow existing late-error handling.
No Source, parse, index, allowance, second renderer, public tree clone or correction
authority is added. These are application accounting controls, not hard RSS or OS
preemption guarantees.
See [MD063 policy](../../docs/rules/headings/md063.md).

MD080 uses OriginalHeadingAnchors to invoke the existing shared resource index
builder on the actual completed original parse. A private profile/options/source/
index/operation witness prevents forged public parse fields from granting facts.
Only owned heading records and availability escape; no Service/cache/view/content
witness, resource read, Root or new parse/source/allowance is introduced. Original
caller and origin cancellation, sticky work/node/time and final withholding apply.
The shared builder retains raw labels from the same HeadingFact and preserves its
whole-index uncertainty. MD080 reserves a finite seen-map capacity before allocation,
charges borrowed base bytes before hashing/equality and reserves actual output
records; immutable string bytes are not charged again as constructed nodes. Every
later present-base duplicate, including empty bases, uses the original full Label.
Local AnchorStyle defaults github, validates github/explicit and persists through
plans/candidates. This does not enable index_file or cross-file consumers. Finite
application accounting is not a hard RSS or OS-preemption guarantee.


### Empty sections

MD082 uses the parser-owned EmptySections projection under the complete original
production profile, parse options, source/index and operation allowance. One
pending actual heading and intervening body flag resolve each heading once; a
deeper next heading itself satisfies its parent. All actual container and hidden
headings participate before filtering. Only the globally last actual heading per
document can receive the boolean allow_final_section exemption. Labels share the
raw heading-label helper, including empty-label points; own inline labels are not
body. Semantic whitespace, actual object presence and confirmed comment ownership
follow the [MD082 policy](../../docs/rules/headings/md082.md).

Traversal stack growth and derived output are reserved before allocation. Borrowed
AST visits and bounded semantic UTF-8 byte scans use work from the original
allowance; both caller and origin controls and final publication are checked.
An interrupted projection withholds its inventory atomically. Raw/comment state
uses actual private nodes and owned content/tails, excluding container prefixes.
This provider does not build an anchor index or consume AnchorStyle, metadata,
resource availability, correction, write or MCP authority. MD082 remains default
disabled and report-only; MD047 retains the sole production correction.


### Thematic breaks before headings and explicit slides

MD090 borrows direct actual body-block siblings under the complete original
production profile/options/source/index/budget witness. An iterative reserved stack
keeps each container sequence separate; owned first/last thematic markers provide
one original range without raw scanning. Comments interrupt this adjacency. Labels,
traversal/output storage and final publication share original operation controls.

A narrow immutable per-path slide provider stays on the selected execution plan,
including candidate and saved views. Configuration owns the existing direct bounded
matcher, first match and exact origin; adapters bind invocation paths to the selected
configuration directory once. The annotation follows mandatory encoding validation
and changes neither selection nor format. Option-aware applicability may use only
admitted options and explicit context before syntax; it cannot borrow partial syntax.
Known slide exemptions are retained independently of unrelated parser/directive
uncertainty. Inspection prepares context before callbacks and preserves truthful
completed sections when a later operation stop occurs. MD090 stays default disabled
and manual; MD047 remains the sole production correction.

## Heading inline construct projection

MD093 borrows actual inline kinds and complete source envelopes from each real
Heading using the completed private production profile, options, source/index and
original operation allowance. It walks nested wrappers independently, retains raw
HTML closer traversal and image-alt isolation, and never redispatches code, HTML,
destination or title payload. Escaped/unresolved text remains parser-owned text.
No new parse, renderer, public-tree clone, anchor index or resource service is used.

Traversal stacks, output and finding growth reserve original records/work before
construction. Caller and originating controls, sticky work/node/time stops and
final publication checks withhold incomplete inventories. The immutable admitted
allow list is read under the same controls into five fixed allowance bits; returned
facts own their slice and cannot mutate syntax or options. Nested constructs stay
independent and source geometry uses original envelopes, including container gaps.
The rule remains default disabled, RequiresSyntax and NoFix; MD047 alone owns edits.

## Original list structure projection

MD004 borrows actual List/ListItem ancestry and first-line marker facts through
the completed private production profile/options/source/index/original-budget
witness. Detached records retain group/item/parent envelopes, exact marker/prefix
spans and scalar list properties without mutable syntax handles or public-tree
cloning. Body-container preorder preserves outer-before-inner source order; code,
HTML, heading and paragraph payload is not reparsed. Later list rules can reuse
this seam without a second parse, renderer, anchor index or resource service.

Stack/fact storage and copied prefixes reserve original records/work before
construction. Caller and originating controls, sticky work/node/time stops and
final publication checks withhold unfinished inventories. MD004 selects one
per-document first-bullet baseline before directive filtering, or an admitted
explicit marker, and emits marker-only manual findings. It remains enabled by
default with PlannedFix metadata; MD047 alone supplies executable corrections.

MD005 reuses the same detached list facts. It compares structural marker
indentation (`ContentIndent - MarkerWidth - Padding`) against each actual group's
first direct item, before directive filtering. Full source-backed group spans
retain independent nested baselines without replacing an enclosing baseline.
Ordered-width and post-marker padding changes do not move marker starts.
Per-analysis baseline storage, comparisons and finding growth reserve original
work and records before construction, including aligned groups. Final checks
withhold interrupted output. Exact full item-marker spans produce manual
findings; default-enabled PlannedFix metadata grants no correction authority.

MD007 reuses the complete original list inventory and admitted scalar options.
For each actual bullet item, local indentation is ContentIndent minus MarkerWidth
minus Padding. With a bullet parent, its actual marker width plus padding plus
that local indentation must equal configured indent. With an ordered parent,
local indentation must be zero: its content column is the target without extra
indent. Parentless bullets require zero, or exactly indent with start_indented.
Quotes strip prefixes without resetting actual ancestry; hidden or misindented
parents remain anchors. A bounded invocation-local map keys constant source-token
and item offsets, retaining ordered anchors before children; storage, work and
finding growth reserve the original allowance even on clean inputs. Final caller
and origin checks withhold interrupted findings. Findings own one bullet byte
and remain manual despite PlannedFix metadata; MD047 alone produces edits.

MD029 reuses the original detached list facts and borrows its admitted `style`
under the same original operation controls. Each actual ordered group keeps an
immutable first direct numeric start and a direct ordinal, including wrong and
suppressed items. Independent nested groups and quotes retain enclosing state;
ordered descendants below bullets are targets. Numeric leading zeros are equal,
zero starts are valid, and comparing actual minus first to ordinal avoids growing
numeric sums. `one` requires numeric 1 even on the first item. State uses constant
source-token and group offsets, with storage, work, capacity, copies and finding
records reserved before construction, including clean inputs. Caller and origin
checks atomically withhold interrupted findings. Diagnostics own the full digits
and delimiter only. Default-enabled PlannedFix remains metadata; findings are
manual and MD047 alone supplies executable edits.

MD032 borrows the completed original ListSpacing inventory under caller and origin
controls. It emits zero-width manual points before first list markers or at the
following non-list body start. Context precedes directive filtering; default
enablement and PlannedFix metadata grant no edit authority. Finding capacity,
copies and logical records are reserved before construction, with final atomic
withholding. MD047 remains the sole edit producer.

MD069 borrows maximal repeated-marker spans through ListRepetitions under both
caller and originating operation controls. Callback findings reserve capacity,
copied prefixes and logical records before publication, which is atomic on
interruption. The default-enabled NoFix metadata and optionless schema remain
unchanged; MD047 is the sole correction producer.


MD027 borrows the narrow completed-original BlockquoteSpaces projection through a no-options facade. Actual Paragraph contributions, confirmed prefix replay and source-ordered literal/raw continuation govern eligibility. Detached excess SPACE ranges remain manual under both caller and origin controls, sticky work/node/time limits and final atomic withholding. MD047 alone supplies corrections.


MD028 borrows BlockquoteGaps through a no-options facade. Completed original sibling ownership and whole physical blank gaps determine detached manual body spans, including present empty positions. Caller and origin controls, sticky work/node/time limits and final atomic withholding apply at all seams. NoFix metadata grants no correction authority.


MD064 borrows the completed original production profile/options/source/index and
operation through RepeatedProseSpaces. Actual Paragraph/Heading Text, transparent
formatting and visible labels supply semantic Unicode Letter/Number/Mark tokens;
whitespace, Cc, original NUL and opaque syntax are barriers. Only contiguous exact
original SPACE runs count; hidden link fields never supply events. Actual raw tags,
HTML tails and isolated image-alt state protect payload without whole-line hiding.
The detached surplus [start+maximum,end) remains manual. Every source/semantic
probe, traversal frame, capacity, copied prefix and logical span/finding is reserved
before work, including empty/clean/protected input. Caller and original controls,
sticky finite work/nodes/time and final producer/facade/callback checks atomically
withhold failure. No reparse, renderer, source/tree clone, retained AST, new allowance
or correction authority is added; MD047 remains the sole correction producer.


MD085 borrows the completed original production source, index, options, profile
and operation through ParagraphIndentation. Actual root Paragraph content fixes
one baseline; all container descendants are excluded. Ordinary semantic Text
witnesses prose separately from protected multiline physical prefixes. Actual raw
state crosses Paragraph boundaries and HTML tails, with isolated image labels.
Actual unprotected HardBreaks exempt only their physical successors. Original
ASCII SPACE/TAB surplus spans remain detached and manual. Physical-line state,
failed byte probes, semantic/provenance associations, bounded lookups, raw state,
traversal frames, growth capacity, copied prefixes and logical spans/findings are
reserved before work. Empty/clean/protected input retains caller and originating
controls, sticky finite work/nodes/elapsed and atomic producer/facade/callback
publication. No reparse, source/index/tree clone, renderer, decoder, new allowance
or correction authority is introduced. MD047 remains the sole correction producer.


MD089 shares the original completed production source/index/options/profile and
operation through a narrow CJKSpacing projection. Actual Paragraph, Heading and
TableCell Exact scalars supply physical pairs under Unicode 17 Han intersect
Ideographic, Latin intersect Letter and ASCII digits. Actual recognized URL
owners, raw tags, HTML tails and isolated image alternatives retain protection.
Zero-width insertion spans and findings are detached and manual. Source/semantic
association comparisons, failed UTF-8/scalar probes, bounded Unicode table
lookups, traversal state/frames, capacity, copied prefixes and logical records
reserve work/nodes before use. Empty, clean and protected paths retain caller
and origin cancellation, sticky work/nodes/elapsed and final atomic withholding
at producer, facade and callback. No reparse, tree/source/index clone, retained
AST facts, decoder, new allowance or correction authority is introduced.

MD035 borrows completed original thematic-marker spans, private raw tags and HTML
tails through ThematicStyle. A single global borrowed original pattern survives
container changes and disabled intervals; image alternatives isolate raw state.
Traversal frames, marker/source associations, comparisons, state, capacity, copied
prefixes and detached span/finding records reserve the original finite allowance
before work. Empty, clean and protected inputs retain caller/origin controls and
final atomic withholding at producer, facade and callback. No reparse, retained
AST, source/index/options clone, decoder, new allowance or correction authority
is added. MD035 is default enabled and manual; MD047 alone supplies corrections.

MD037 borrows the completed original source/index/options/profile and operation
through DelimiterStyle. Actual Emphasis/Strong marker facts remain distinct from
malformed whitespace candidates. Maximal contiguous ordinary Text islands retain
escape/entity payload, raw lifetimes, HTML tails and isolated image alternatives.
Each authored edge-whitespace span is detached and manual; caller/origin controls,
precharged scalar/traversal/capacity/copy/record work and final atomic withholding
apply at producer, facade and callback. No reparse, decoder or correction authority
is introduced; MD047 alone supplies corrections.

MD038 borrows actual completed CodeSpan ownership through CodeSpanSpaces under
original source/index/options/profile and operation authority. Actual normalized
Literal decides the required innermost padding pair; only retained original ASCII
SPACE edge bytes supply detached manual ranges. Both edges must participate;
one-sided runs and all-SPACE values remain meaningful. Raw lifetimes, HTML tails
and image-alternative isolation preserve protected contexts. Traversal, source
associations, failed byte probes, capacity/copies and logical ranges/findings
reserve original work/nodes before use. Caller/origin cancellation, sticky limits
and final producer/facade/callback checks withhold failed dependent output, including
empty/clean/protected cases. PlannedFix metadata grants no correction authority;
MD047 alone supplies executable corrections.

MD044 borrows ConfiguredNames through a narrow facade binding the actual original
source/index/options/profile and budget. Option list copies, one fold per name,
one borrowed fold record per exact scalar, bounded candidate-prefix comparisons,
transformed semantic neighbor facts, traversal frames and all capacity/copy/output
work reserve the original allowance before use. No source suffix is repeatedly
folded. Original scalar endpoints govern complete full-fold matches. Every empty,
clean, protected and no-name path observes caller and origin cancellation and sticky
work/nodes/elapsed limits; final producer/facade/callback failure atomically withdraws
detached output. No reparse, mutable tree/index/operation access, fresh allowance,
normalization or correction authority is added. Default OFF and PlannedFix remain.

### MD049 italic delimiter style

MD049 reuses the original parser delimiter inventory. It selects the minimum
eligible actual Emphasis opening position per document, including suppressed
and inactive intervals when the callback executes. Strong and malformed
Whitespace candidates cannot establish the baseline. Authored labels and image
alternatives participate; raw lifetimes, code, metadata and actual URL owners
remain protected. Fixed styles select asterisk or underscore directly.

Each mismatch is one manual original opening-byte finding. Suppression uses
its opening START even when closing state differs. The same delimiter facade
preserves MD037 empty options and narrowly admits one valid resolved style;
MD049 additionally rejects absent internal style. All projections and final
publication retain caller/origin authority and the original finite allowance.

MD065 analysis uses a narrow completed-original ThematicSpacing projection and an
optionless RuleContext facade. A full tree/inline/tail walk preserves semantic raw
lifetimes and image isolation; a separate body walk merges retained definitions
with direct source participants. Actual prefix rollback and terminal list/item
content floors preserve immediate physical separators. Detached Point/Before facts
carry present zero-width original evidence only. No source, parse, index, options,
allowance, AST inspection field or correction capability is added. Producer,
facade, comparison and whole callback reserve work/storage before use and finalize
under both caller and originating controls; any error withholds dependent output,
including empty, clean, protected-only and post-construction paths. MD065 remains
default enabled and report-only for CommonMark and GFM.


MD081 uses ParagraphEmphasis to borrow the completed private original production
source/index/options/profile and operation. Detached full Content-hull/count facts
retain actual Paragraph ownership, semantic Text provenance, Emphasis/Strong
boolean union and full raw-tag/closing-tail lifetime. Image descriptions are
opaque and cannot change outer raw state. No renderer, reparse, public AST/schema,
source/index/options clone, resource/metadata or correction authority is added.
Every traversal, terminal probe, provenance/byte/scalar/raw/option test, capacity,
copy and fact/finding reservation precedes work under the original finite controls.
Unicode table shape is guarded before bounded scalar classifications. Producer,
getter, facade, comparison and distinct whole-callback final checks bind caller
and origin and atomically withhold all dependent output on error, including empty,
clean, protected, minimum-skipped and post-output paths. MD081 remains disabled
and manual; MD047 alone produces corrections.


## MD088 original ASCII punctuation analysis

ASCIIPunctuation borrows the completed private original parse, source, production
profile, options, physical index and operation budget. It returns detached scalar
ranges only: actual Paragraph/Heading/GFM prose-cell Exact Text, transparent
formatting, all Link findings excluded with global Link raw transitions, and fully
opaque Image children/tails. It follows private rawTag/rawClosing/htmlTail lifetime
without resetting at owners. Code, comments/attributes, headers and retained
definitions stay protected. Entity/Escape/Normalized/PartialTab contributions
supply no candidates; Exact bytes are validated against original source.

Reservations precede structural and terminal visits, source/provenance checks,
byte association, bounded UTF-8 decode and fixed-six classification, raw comparisons,
stack/output growth, copied prefixes and retained facts/findings. Producer, style
getter, facade, comparison and distinct outer callback final gates retain caller
AND originating controls and sticky work/node/elapsed limits; any error atomically
withholds all dependent MD088 output, including empty/clean/protected/post-output.
No reparse, renderer, public-tree/source/index clone, new allowance or calibration
is used. Finite application accounting is not a CPU/RSS or throughput guarantee.

Exactly one original three-byte/one-code-point range per eligible scalar uses
`Use ASCII quotes and dashes in prose.` Full context precedes shared START
suppression and inspect intersection. Default OFF/style ascii/PlannedFix remain;
this pass creates no MD088 edits, previews or corrected content. MD047 is sole fixer.

## MD033 embedded HTML inventory

`EmbeddedHTMLTags` borrows completed private original production syntax, source,
profile, parse options, physical index and operation. It returns detached Tag/Name
spans and explicit availability. Actual HTMLBlock/InlineHTML owned content is
lexed with existing mapped CommonMark machinery; whole Images are opaque. Complete
named tokens in form-6/7 owners provide local transition proof. Independently
owned InlineHTML requires private rawTag/rawClosing proof under incoming raw state.
Opaque tokens and quoted attributes cannot create a cross-owner closer. Form 1
retains the parser's literal any-of-four closer boundary. Full owners include
closing tails exactly once; no public-tree clone, new parse, renderer, source,
index, allowance, decoder or browser/foreign model is added.

Traversal, failed/terminal scans, names, quotes, mapping/association bytes, raw
comparisons, stack/output capacity, copied prefixes and logical records reserve
original work/storage before use. Existing lexer terminator indexes prevent
repeated failed suffix scans; the first failed owned named candidate atomically
withholds the inventory with context-unavailable coverage. Empty, clean, protected,
allowed and post-output paths observe caller AND origin controls and sticky
work/nodes/elapsed limits through producer/getter/facade/comparison/distinct outer
final publication. The detached allowlist getter preserves the permissive string
list domain; equal-length whole-name ASCII comparison has no trim/Unicode fold.
MD033 remains default OFF, RequiresSyntax, UsesFrontmatter=false and NoFix; MD047
alone supplies corrections. Finite application accounting is not a hard CPU/RSS,
throughput or OS preemption guarantee.

## MD086 original unmatched-comment inventory

`UnclosedHTMLComments` borrows the completed private CommonMark/full-GFM parse,
original source/index/options and original operation. It visits actual HTML
owners in source order, including mapped form-6/7 interiors and owned tails once.
Whole Images are opaque; actual Link labels remain eligible. Existing lexer
short forms, quote/PI/declaration/CDATA precedence and four-name raw lifetimes
are preserved. Matching never crosses the actual owner/container boundary.

Owner scratch capacity, mapping/source association, traversal, terminator indexes,
failed suffixes, raw/quote comparisons, copied prefixes, output capacities and
logical facts/findings reserve finite original work/records before use. Scratch
length arithmetic remains checked and wide before multiplication; saturation
fails closed through the same original allowance. Actual lexer work is metered
as well. Producer, empty-options facade, comparison and distinct outer callback
final publication observe caller AND originating controls, including empty,
clean, protected, unavailable and post-output paths. Any failure atomically
withholds output. Failed owned named-token context gives explicit coverage;
an established unmatched comment is a violation instead. No reparse, retained
parse-time facts, decoder, source/tree/index clone or replacement allowance is
introduced. Finite application accounting is not a CPU/RSS guarantee.

MD086 is default ON, RequiresSyntax, UsesFrontmatter=false and permanently NoFix.
Findings use `HTML comment is missing a closing delimiter.` over exactly the
original four-byte opener. Shared START suppression precedes inspection slicing;
MD047 remains the sole correction producer.
