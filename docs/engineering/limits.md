# Shared resource limits

Status: accepted on 2026-10-15. Parser-layer budgets are implemented and
verified as described in [parser accounting](parser-limits.md). Lint acquisition
enforces document and aggregate content bytes with the shared execution budget;
all four lint/fix/config-validation reporters enforce encoded response bytes,
reporting time and report reserve feasibility (including both editor streams).
Inspect enforces text/JSON projection/reporting bounds. Config validation also
enforces bounded configuration/Git-source acquisition, directory/path limits,
structured and alias depth, and shared node/work/time budgets; it rejects
inapplicable overrides. Fix shares candidate, reparse and preservation accounting
on one allowance, keeps a saved run's rechecking on a fresh one (see
[saved fix runs](#saved-fix-runs)), and admits the mandatory write-outcome manifest before
native publication. Native writes run on Linux, macOS and Windows. Remaining traversal, resource and MCP adapter/application limits
and their end-to-end verification remain pending. Issue #251 adds internal raw
native acquisition counters for path/depth, directory entries, actual content
bytes and bounded retries using the existing operation budget. It does not enable
CLI/MCP resource lookup; complete replacement-overlay admission remains downstream. Its native Linux/macOS/Windows checks passed in
[run 40145609610](https://github.com/frithuz/mdchk/actions/runs/40145609610).
Issue #252 bounds supplied-index construction, including distinct inferred parent
identities, with the same operation work/node/time budgets. It adds no lookup
CLI/MCP resource capability. Issue #253 composes one operation-owned finite
identity/lookup ledger and single-worker cache across both adapters. It refuses
new admission at the cap while retaining first facts, rather than evicting and
reopening resources. Parsed-source reuse is bounded by the same identity policy
and shared node/work/time counters; CLI/MCP consumers remain downstream.
These requirements incorporate the revised **1 MiB per-file limit** where applicable.
See the [contract index](contracts.md) for related specifications and issue ownership.

These are finite application-work budgets, not a process memory or throughput guarantee.
The [machine-readable profile](limits.json) carries exact values and accounting scope.
MiB = 1,048,576 bytes; KiB = 1,024 bytes.

## Current CLI enforcement

Lint reports only applicable enforced values in `effective_limits`, including
its parser budgets, aggregate acquisition bytes and `response-bytes`,
`reporting-time`, and `report-reserve-bytes`. Reporting-time uses nanoseconds;
response/reserve values use bytes. Operator overrides replace the corresponding
defaults. Reporting gets its own deadline after analysis stops and measures
actual encoded bytes in each supported format, including escaping, newline
framing and both editor output streams. Whole
optional records can be omitted with exact known counts. Detailed error records
can become identity-preserving summaries with exact `/errors` omission counts;
categories, IDs, paths, snapshots and ranges remain intact. Actual encountered
errors, interruptions and file outcomes retain their meaning. A delivery failure returns
IO/13 unless an established internal failure requires 14.

Before analysis, reserve feasibility requires `report-reserve-bytes` plus the
encoded mandatory target identity/outcome manifest to fit `response-bytes`.
The final encoded report can use the full response budget. A genuinely known
infeasible request or envelope is a preflight usage/11 failure. CLI path
arguments are selection requests, not known target records: a directory or
excluded path can select zero targets. CLI preproject feasibility therefore
checks only the known empty envelope. After discovery, an infeasible actual
target manifest stops before acquisition/analysis as incomplete/15, with exact
`/files` and `/coverage` output-limit omissions; already encountered errors
retain their precedence.
Reporting pre-encodes an essential partial fallback for deadline expiry.

Accepting an override name does not assert that every future application scope
is already integrated. Lint/fix traversal counts and logical discovery-path
budgets remain partially pending; config validation enforces its scan/path and
aggregate configuration budgets. Resource inventories and MCP admission remain
future adapter work. Native fix write reservations are delivered; they do not
establish future MCP reservation or whole-profile enforcement. The full profile below is
the accepted implementation contract.

## Defaults

| Name | Default | Accounting scope |
| --- | --- | --- |
| `document-bytes` | 1 MiB (1,048,576 bytes) | Each target/resource text snapshot and each corrected candidate; do not load binary image contents for existence checks. |
| `total-content-bytes` | 8 MiB (8,388,608 bytes) | Original supplied/acquired target and resource contents per operation, including submitted excluded files; independently cap each complete candidate overlay to the same size. |
| `request-bytes` | 16 MiB (16,777,216 bytes) | Each incoming MCP JSON-RPC message before decoding; enforce for stdio and HTTP. Transport framing/header limits are separate. |
| `response-bytes` | 8 MiB (8,388,608 bytes) | Each complete serialized CLI result or outgoing MCP result JSON message, including duplicated text/structured payloads and JSON escaping, excluding transport headers/framing. |
| `target-files` | 5,000 files | Selected target files in CLI/project operations; supplied files entries in standalone, including excluded supplied entries. |
| `resource-identities` | 25,000 identities | Combined targets, explicit supplied resource entries, inferred parent directories and distinct queried logical resource identities, including missing/blocked identities. |
| `resource-lookups` | 25,000 distinct logical keys | Distinct canonical resource lookup keys including misses; directory/index attempts are separate keys. Cache hits do not count as new lookups. |
| `directory-entries` | 100,000 observed entries | Entries visited during operation discovery/inventory/config scans, including examined excluded entries; reused cached enumeration is not new I/O. |
| `path-bytes` | 4,096 UTF-8 bytes | A canonical logical relative path; native filesystem limits can be stricter. |
| `path-depth` | 64 segments | Canonical logical relative paths and traversal depth within the operation boundary; no symlink expansion. |
| `config-bytes` | 1 MiB (1,048,576 bytes) | Each acquired configuration/policy text definition or supplied serialized config object. |
| `total-config-bytes` | 8 MiB (8,388,608 bytes) | All configuration/policy source text acquired for the operation; also included in the applicable request/acquisition accounting. |
| `structured-input-depth` | 64 nested containers | Raw protocol JSON and configuration syntax. Reject before ordinary decoding loses evidence or allocates unbounded structures. |
| `syntax-depth` | 256 nested constructs | Markdown/metadata syntax and alias traversal; unavailable syntax remains explicit rather than being flattened. |
| `document-nodes` | 250,000 nodes or source records | Each individual document parse, including metadata and retained source records, before allocating beyond the cap. |
| `total-nodes` | 2,000,000 nodes or source records constructed, plus 32 per acquired source byte | Operation cumulative construction, including reparses/fix rounds/retries; reusing immutable nodes does not construct them again. A saved `fix` run applies this cap to each half; see [saved fix runs](#saved-fix-runs). |
| `work-units` | 250,000,000 metered work units, plus 8,192 per acquired source byte | Operation-wide parser, rule, metadata, diff and preservation work; charge byte inspections/comparisons and structural/candidate visits with documented adapter metering. All fix rounds and counterfactual MD087 work share the meter. A saved `fix` run applies this cap to each half; see [saved fix runs](#saved-fix-runs). |
| `snapshot-retries` | 2 additional attempts | At most 3 acquisition attempts per unstable logical snapshot; retry bytes/work remain charged. |
| `operation-time` | 60 seconds | Monotonic wall time for validation/discovery/queueing/analysis/fix commits; MCP begins once the bounded message is received for processing. Network body/idle timeouts belong to transport policy. The deadline covers a whole saved `fix` run and does not restart for its second half. |
| `reporting-time` | 2 additional seconds | Reserved cleanup/outcome reconciliation/report construction after analysis stops; never used for additional fixes. It cannot guarantee interruption of an unresponsive OS call or report delivery to a disconnected client. |
| `report-reserve-bytes` | 65,536 bytes within response budget | Minimum reserve for the envelope, interruption and omission summary; reserve the entire bounded per-target write-outcome manifest in addition before commits. |
| `active-calls` | 4 MCP tool calls | Server-wide active calls; separate small protocol-control handling must remain responsive. |
| `queued-calls` | 8 MCP tool calls | Server-wide admission queue. Above capacity return explicit busy/limit rejection without starting work. |
| `queue-time` | 10 seconds | Maximum queue wait, included in the operation-time budget; expiry follows accepted runtime interruption semantics. |
| `project-fixes` | 1 MCP project fix operation | Serialize project fix operations, including project previews initially; cooperating interprocess write coordination remains required separately. |
| `workers-per-operation` | 1 worker | Initial intra-operation engine worker limit; raise only through explicit operator setting and verified implementation support. |
| `fix-rounds` | 10 changing rounds | Previously accepted fixed correction cap plus a read-only convergence check; not raised by the generic limit override. |

## Content scaling

`work-units` and `total-nodes` are operation-wide, so a fixed value would cap how much Markdown one
run can analyze regardless of the 8 MiB `total-content-bytes` and 5,000 `target-files` the same
profile admits. The listed default is therefore a floor: each byte of target content acquired
raises the work allowance by 8,192 units and the node allowance by 32 records. The accepted bound
on content (`total-content-bytes`) and `operation-time` remain the ceilings on total effort.

Measured analysis of this repository's documentation with every rule enabled costs about 2,300
work units and 9 nodes per source byte, so the rates leave headroom for fix rounds. A default
configuration over 145 files (1.27 MB) previously stopped after 13 files and now completes.

An operator-set `--limit work-units` or `--limit total-nodes` disables scaling for that limit and
is a hard cap, which is how to reproduce the fixed budget. Reports show the configured floor as
the effective limit; `allowed` in a limit interruption is the allowance in force when it was
exhausted. Per-document caps (`document-bytes`, `document-nodes`, `syntax-depth`) do not scale.
Only content acquired as lint, fix or inspect targets grants allowance; resource lookups and
candidate overlays do not.

## Saved fix runs

`--limit` caps each half of a saved `fix` run, not its total. The first half is
everything through publication: selection, acquisition, every fix round, candidate
analysis, preservation proof and the writes. After a complete validation, the second half
uses a fresh allowance of the same limits, with the same content scaling and grant: confirming saved content, analyzing it,
and building diffs and result records. A saved run can therefore spend up to twice
`work-units` and twice `total-nodes`. Per-document and per-parse caps such as
`document-nodes` apply as usual in each half.

The context deadline set from `operation-time` does not restart, so both halves
together stay within one `operation-time`, and cancellation stops either half.

The second half gets its own allowance because the validated bytes were already analyzed
within the limits, so a spent budget can't report a successful write as incomplete. `lint`,
`inspect`, `fix --dry-run` and a saved run whose validation did not complete have one
allowance. Resource lookups for the saved view stay on the first allowance, because resource
bytes are acquired once per operation. Status `15` can still accompany a `written` change
if the run is cancelled, if the `operation-time` deadline passes, if a limit stops the
write itself or a later file, or if such a lookup exhausts the first allowance.

## Overrides

### Interface

Repeatable command/startup option --limit name=value for lint, fix, inspect, config validate and
mcp. Example: --limit operation-time=120s.

### Validation

Known names and positive finite values only; byte sizes accept documented binary units, durations
explicit units, counts decimal integers. Reject overflow, conflicting duplicate values and
incoherent bounds. Identical repeated values may be deduplicated. Require document-bytes <=
total-content-bytes, config-bytes <= total-config-bytes, queue-time <= operation-time, and
sufficient response budget for the minimal reporting reserve.

### Authority

CLI invoker and MCP server operator can set finite lower/higher limits within validated
implementation bounds. Project configuration, Markdown directives and MCP tool arguments cannot
alter them. No unlimited/zero escape hatch; no ambient project/environment override.

### Visibility

Report effective limit values and the particular exceeded limit with observed/allowed amounts when
known. Expose server values through documented discovery/metadata without adding tool-call fields;
precise MCP location follows its protocol decision.


## Accounting notes

- Limits are independent and the first exhausted one stops affected work under the accepted interruption policy. Equivalent CLI/project/standalone work uses the same units/defaults; wire limits apply only where serialization exists.
- Count submitted data before exclusions. Cache repeated observations once for logical identity/input totals, but charge actual repeated acquisition/parse/rule work and retry bytes. Do not retain every historical candidate body merely to detect cycles.
- Use checked arithmetic and enforce caps before large allocations. The AST/work counter specification must identify metering points for every parser, metadata adapter, rule, preservation validator and diff routine; add deadline/cancellation checkpoints around bounded loops and I/O.
- These are application-work limits, not a hard process-RSS or crash-proof OS isolation guarantee. Server concurrency multiplies resident operation data; operators must size deployment memory accordingly.
- Changing numeric defaults requires documented review and representative/adversarial benchmarks; this contract selects starting values, not a claim that absent implementation has already met performance targets.
- The 2026-11-06 revision makes `work-units` and `total-nodes` floors that scale with acquired target content; see [Content scaling](#content-scaling).
- The 2026-10-15 revision reduces per-text, aggregate-content, request and response byte defaults by a factor of eight. Independent node, work, count and timing limits are unchanged. These starting values are not benchmark-derived performance promises.


## Response policy

- Measure the actual encoded result size, including JSON escaping and any duplicated MCP fallback text. Validate known mandatory response feasibility before processing supplied requests where possible.
- Before any filesystem commit, reserve space for every selected target’s outcome and any attributable write error or uncertainty; if the essential manifest cannot fit, perform no commits and report the accepted limit outcome.
- When details exceed the remaining budget, omit whole optional diff/AST sections first, then retain deterministically ordered whole diagnostic/observation records with explicit omitted counts and availability. Never imply omitted findings were absent or an omitted AST was empty; mark complete=false and the output-limit reason.
- Never truncate a corrected content string. Standalone fix may withhold entire file-content records with explicit path/outcome attribution; do not label those files as successfully returned. If the essential file-identity/status list cannot fit, reject before analysis rather than silently dropping identities.
- An output-size limit cannot discard the reserved manifest for files actually written. Later output delivery failure remains io under the accepted precedence. No automatic background storage, filesystem report spool for standalone payloads, pagination or opaque continuation token is introduced.

## Encoding error retention

MD094 reports each invalid maximal subpart, and a binary or mis-encoded file can
hold as many as `document-bytes`. Two fixed bounds, which are not configurable
limits, keep those records bounded:

- A file retains at most 1,000 `invalid-utf8` errors.
- An operation retains at most 10,000, shared across every per-target analysis.
  Files consume this allowance in path order, so `lint` and `fix` retain the same
  records for the same targets.

A file always retains its first error, even after the operation allowance is
spent, so it always has a cause and a located record. The worst case is 14,999
records, since `target-files` caps the targets at 5,000.

Each file with errors left out gets one omission: `section: /errors`, `reason:
output-limit`, `path` set to the file, and `count` set to the exact number left
out. It makes a lint or fix result `complete: false`. An `inspect` result is also
`complete: false`, but this omission alone keeps exit `0` and status `inspected`,
because the observation itself completed.

When a lint or fix text or JSON result still exceeds `response-bytes`, the first
reduction drops trailing `invalid-utf8` records, never a file's first, and records
one path-less `/errors` omission with the exact count. Those path-less omissions
keep their usual meaning, including exit `15` for `inspect`. If the report still
does not fit, the per-file retention omissions are replaced by one path-less
`/errors` omission with their exact total, because they grow with the number of
invalid files.

The first-error guarantee is for retention and these reductions. If a lint text or
JSON report's essential records still exceed `response-bytes`, lint emits a
[minimal report](results.md#shared-result-schema): located errors become category
summaries with an exact `/errors` omission, and JSON can also omit `checked_files`
and file snapshots. File paths and outcomes stay, which the pre-analysis manifest
check reserves room for. Lint encodes this report before the `reporting-time` timer
starts, with no search over whole reports, so an expired deadline still delivers it.
The timed deadline fallback likewise omits all excess `invalid-utf8` records in one
step instead of searching for the largest prefix.

The scan is one forward pass charged to `work-units`: at least one unit per source
byte and a constant per retained record, in addition to the indexing reserve.

## Impact of the 1 MiB revision

| Budget | Previous | Accepted |
| --- | --- | --- |
| Each target/resource/candidate text | 8 MiB | 1 MiB |
| Original content and each separate candidate overlay | 64 MiB | 8 MiB |
| Raw MCP message | 128 MiB | 16 MiB |
| Encoded CLI/MCP result | 64 MiB | 8 MiB |

Eight maximum-sized inputs still fit the aggregate content budget. At 50 KiB
per file, aggregate capacity falls from 1,310 to 163 files; at 10 KiB, the
5,000-target limit previously bound first, while the new content cap allows 819.
These counts assume no additional resource text. Four active calls holding an
original and one candidate overlay account for at most 64 MiB of those raw text
bytes, versus 512 MiB previously; this excludes parser objects, JSON, buffers,
resources retained for other purposes, and other process memory.

MCP includes both structured output and escaped JSON fallback text. Three plain
1 MiB returned documents can fit; four can exceed the 8 MiB response budget once
duplication and metadata are included. Escape-heavy content can exceed it even
for one file. Measure actual serialization and withhold whole contents explicitly
when necessary. Smaller input defaults do not establish an eightfold speedup.

HTTP-specific admission, authentication, and network deadlines are defined in the
[HTTP contract](http.md#transport-limits). Runtime exhaustion follows the
[result contract](results.md#interruption-and-exit-precedence).

## Configuration emergency reporting reserve

Before starting the operator's `reporting-time` interval, configuration validation
encodes a fixed-work emergency envelope. Canonical finalization has already
accumulated at most four error-category counts; the reserve uses those counts,
the typed scan/validity summary and O(1) collection lengths for exact omissions.
It never clones/sorts the source manifest or encodes source-derived error prose.
Its fixed number of records is checked against the encoded response limit.

The requested interval bounds subsequent full-fallback and detail construction.
Expiration in either stage delivers the pre-encoded reserve, with an explicit
reporting interruption, incomplete envelope, retained scan completeness/counts
and operational-error precedence. This supports positive finite deadlines down
to a nanosecond without claiming that constant reserve construction or output
delivery finishes within that interval. No second timed retry performs additional
project work. Lint retains its existing manifest and presentation behavior.


The #254 derived anchor index shares the parser operation's work, node and elapsed
meters. Heading/HTML extraction, visible ID bytes, global explicit reservation,
per-base suffix probes and detached inspection copies are charged before their
corresponding scans or retention. Its service cache is operation-owned, bounded
by parsed admission, and keyed by the full parsed policy and exact source/view
owner. Selected-target content witnesses reuse a finite derived-entry cap;
`resource-derived-entries` reports exhaustion without registering resources.
Original selected bytes are already counted at service construction. The narrow
selected-source seam enforces each replacement's text limit; complete aggregate
candidate-overlay admission remains the coordinated producer's responsibility.

MD032 source-boundary projection and finding construction share the original
parser work, node and elapsed allowance. Empty and accepted inputs still reserve
traversal and line state. Interrupted inventories and findings are withheld.

MD069 shares the original completed parse allowance for item inventory, constant
identity state, physical-line lookups, original gap bytes, run records and output
capacity. Finding growth, copied prefixes and logical records are reserved before
work. Caller and origin stops withhold the entire dependent output.

MD076 shares the original operation allowance for source-line summaries, bounded
index lookups, actual tree frames, prefix rollback, removed-definition association,
group baselines, fact/finding capacity, copied prefixes and logical records.
Empty, clean and interrupted inventories retain caller/origin controls and final
atomic withholding. No source copy, reparse or new allowance is introduced.


MD077 shares the completed original operation allowance for per-line cursors,
confirmed prefix replay, final ownership/definition association, inline literal
protection, traversal growth/copies and detached findings. Producer, facade and
callback publication checks withhold all dependent output after failure.

MD009 analysis shares the original finite operation for physical suffix scans,
final owner association and detached findings, including state/capacity/copy work
on clean and protected inputs. It creates no new allowance.

MD010 uses the original finite operation for source-byte scans, literal/raw
associations and confirmed prefix replay, including failed probes, capacity,
copies and logical records. spaces_per_tab has no numeric-sized work or storage.
Caller/origin controls and final atomic withholding apply to empty, clean and
protected-only inputs as well as violations.

MD012 uses the original finite operation for physical-line state, source visits
and failed probes, actual prefix/protected associations, lookups, traversal frames,
capacity growth, copied prefixes and detached records. It compares observed counts
to `maximum` without threshold-sized storage or maximum+1 arithmetic. Caller and
origin cancellation/deadline, sticky original limits and final publication checks
withhold incomplete output at producer, facade and manual callback boundaries.


MD027 shares the completed original finite operation for compact physical-line state, actual prefix replay and failed probes, final Paragraph contribution associations, inline/raw continuation, HTML tails, traversal capacity/copies and detached span/finding records. Entry, loop and final caller/origin controls atomically withhold failed output, including empty, clean and protected input.


MD028 shares the original finite operation for physical-line state, suffix probes, actual atomic prefix associations, protected raw/literal ownership, bounded lookups, final sibling comparisons, traversal frames, capacity growth, copied prefixes and detached records. Empty, clean and protected inputs retain both caller/origin controls and final atomic withholding at producer, facade and callback.


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
or correction authority is added; this analysis produces no edits.


MD085 borrows the completed original production source, index, options, profile
and operation through ParagraphIndentation. Actual root Paragraph content fixes
one baseline; all container descendants are excluded. Ordinary semantic Text
witnesses prose separately from protected multiline physical prefixes. Actual raw
state crosses Paragraph boundaries and HTML tails, with isolated image labels.
Actual unprotected HardBreaks exempt only their physical successors. Original
ASCII SPACE/TAB surplus spans remain detached. Physical-line state,
failed byte probes, semantic/provenance associations, bounded lookups, raw state,
traversal frames, growth capacity, copied prefixes and logical spans/findings are
reserved before work. Empty/clean/protected input retains caller and originating
controls, sticky finite work/nodes/elapsed and atomic producer/facade/callback
publication. No source/index/tree clone, renderer, decoder or new allowance is
introduced. For fix eligibility, each enabled finding also runs
ParagraphIndentationPreserved: two bounded local reparses of its line and the
previous line, with and without the span, charged to the same operation budget
in lint, check, inspect and fix alike. A finding may then be fix-available;
analysis still produces no edits, and fix charges its own round facts and
checker proof to the fix allowance. See MD085 corrections in fixes.md.


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
is added. MD035 is default enabled and manual; this analysis produces no edits.

MD037 borrows completed original source/index/options/profile and operation
through DelimiterStyle. Actual recognized Emphasis/Strong facts remain distinct
from malformed whitespace ranges. Semantic contributions, original associations,
UTF-8/scalar/word/whitespace and failed marker probes, raw state, traversal frames,
capacity, copied prefixes and retained records are reserved before use. Empty,
clean and protected input retain caller/origin controls, sticky finite work/node/
elapsed limits and atomic final producer/facade/callback withholding. No reparse,
decoder, tree/source clone or new allowance is introduced. Source-derived bounded
reservation tests establish specific fixtures, without a performance claim.

MD038 borrows actual completed CodeSpan ownership through CodeSpanSpaces under
original source/index/options/profile and operation authority. Actual normalized
Literal decides the required innermost padding pair; only retained original ASCII
SPACE edge bytes supply detached manual ranges. Both edges must participate;
one-sided runs and all-SPACE values remain meaningful. Raw lifetimes, HTML tails
and image-alternative isolation preserve protected contexts. Traversal, source
associations, failed byte probes, capacity/copies and logical ranges/findings
reserve original work/nodes before use. Caller/origin cancellation, sticky limits
and final producer/facade/callback checks withhold failed dependent output, including
empty/clean/protected cases. PlannedFix metadata grants no correction authority.


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
this pass creates no MD088 edits, previews or corrected content.

## MD048 fence delimiter observations

MD048 borrows completed original source/index/options/profile and operation
through FenceStyles. Actual owner traversal, source-order checks, delimiter
classification, necessary raw ownership, selected marker proof, physical-line
lookups and detached records reserve finite work and nodes before use. Known
indented owners need no fence metadata; homogeneous known delimiters prove a
clean result under every exclusion subset. Unknown necessary context atomically
withholds the inventory. Distinct producer, facade, comparator and outer final
checks retain caller and origin cancellation, sticky limits and publication
controls, including empty and homogeneous paths. Exact/one-short budget tests
cover empty producer/facade paths; positive totals are statically derived and
positive final-gate tests independently prove output discard.

## MD070 nested Markdown examples

MD070 borrows original completed source/index/profile/options and operation
identity through NestedExamples. Exact comment eligibility and delayed pairing
use actual container identities, including suffix uniqueness after a tentative
close. Per-line structural cursors retain partial-tab provenance; derived body
segments remove only real ancestor prefixes and ordinary outer indentation.
Container arrays, traversal stacks, source maps and bounded body storage reserve
work and records before allocation. The real payload parser uses the same
cumulative allowance and selected CommonMark/GFM profile, with frontmatter
disabled only for this code payload. It never executes lint directives.

Original and derived raw lifetimes and whole-image opacity are resolved before
publishing detached original spans. Required unknown context atomically withholds
the inventory; known bad associations and operational failures clear all facts.
Producer, facade, comparator and outer publication have separate caller/origin
control gates. Exact/one-short empty producer and facade tests use independently
derived costs; positive reached-gate tests prove cancellation, deadline and
sticky work/node/elapsed failures discard prior findings. No derived source token
is published as diagnostic authority, and no fresh per-example budget is granted.

Original opening owners are retained in a charged table sorted by opening offset
and traversal ordinal. Each intended opener uses a binary lookup, preserving
equal-offset envelope precedence without rescanning all earlier syntax per pair.
Both-profile scaling fixtures exercise thousands of independent clean and violating
examples under a source-derived linear/logarithmic work envelope. Their explicit
node/time ceilings isolate that work regression; they make no default-profile
whole-document completion or measured throughput claim.
