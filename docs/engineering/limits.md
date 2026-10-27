# Shared resource limits

Status: accepted on 2026-10-15. Parser-layer budgets are implemented and
verified as described in [parser accounting](parser-limits.md). Lint acquisition
enforces document and aggregate content bytes with the shared execution budget;
all four lint/fix/config-validation reporters enforce encoded response bytes,
reporting time and report reserve feasibility (including both editor streams).
Inspect enforces text/JSON projection/reporting bounds. Config validation also
enforces bounded configuration/Git-source acquisition, directory/path limits,
structured and alias depth, and shared node/work/time budgets; it rejects
inapplicable overrides. Fix shares candidate, reparse, preservation, diff and
saved-content accounting, and admits the mandatory write-outcome manifest before
native publication. Native writes require the verified Darwin arm64+cgo/local
APFS adapter. Remaining traversal, resource and MCP adapter/application limits
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
| `total-nodes` | 2,000,000 nodes or source records constructed | Operation cumulative construction, including reparses/fix rounds/retries; reusing immutable nodes does not construct them again. |
| `work-units` | 250,000,000 metered work units | Operation-wide parser, rule, metadata, diff and preservation work; charge byte inspections/comparisons and structural/candidate visits with documented adapter metering. All fix rounds and counterfactual MD087 work share the meter. |
| `snapshot-retries` | 2 additional attempts | At most 3 acquisition attempts per unstable logical snapshot; retry bytes/work remain charged. |
| `operation-time` | 60 seconds | Monotonic wall time for validation/discovery/queueing/analysis/fix commits; MCP begins once the bounded message is received for processing. Network body/idle timeouts belong to transport policy. |
| `reporting-time` | 2 additional seconds | Reserved cleanup/outcome reconciliation/report construction after analysis stops; never used for additional fixes. It cannot guarantee interruption of an unresponsive OS call or report delivery to a disconnected client. |
| `report-reserve-bytes` | 65,536 bytes within response budget | Minimum reserve for the envelope, interruption and omission summary; reserve the entire bounded per-target write-outcome manifest in addition before commits. |
| `active-calls` | 4 MCP tool calls | Server-wide active calls; separate small protocol-control handling must remain responsive. |
| `queued-calls` | 8 MCP tool calls | Server-wide admission queue. Above capacity return explicit busy/limit rejection without starting work. |
| `queue-time` | 10 seconds | Maximum queue wait, included in the operation-time budget; expiry follows accepted runtime interruption semantics. |
| `project-fixes` | 1 MCP project fix operation | Serialize project fix operations, including project previews initially; cooperating interprocess write coordination remains required separately. |
| `workers-per-operation` | 1 worker | Initial intra-operation engine worker limit; raise only through explicit operator setting and verified implementation support. |
| `fix-rounds` | 10 changing rounds | Previously accepted fixed correction cap plus a read-only convergence check; not raised by the generic limit override. |

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
- The 2026-10-15 revision reduces per-text, aggregate-content, request and response byte defaults by a factor of eight. Independent node, work, count and timing limits are unchanged. These starting values are not benchmark-derived performance promises.


## Response policy

- Measure the actual encoded result size, including JSON escaping and any duplicated MCP fallback text. Validate known mandatory response feasibility before processing supplied requests where possible.
- Before any filesystem commit, reserve space for every selected target’s outcome and any attributable write error or uncertainty; if the essential manifest cannot fit, perform no commits and report the accepted limit outcome.
- When details exceed the remaining budget, omit whole optional diff/AST sections first, then retain deterministically ordered whole diagnostic/observation records with explicit omitted counts and availability. Never imply omitted findings were absent or an omitted AST was empty; mark complete=false and the output-limit reason.
- Never truncate a corrected content string. Standalone fix may withhold entire file-content records with explicit path/outcome attribution; do not label those files as successfully returned. If the essential file-identity/status list cannot fit, reject before analysis rather than silently dropping identities.
- An output-size limit cannot discard the reserved manifest for files actually written. Later output delivery failure remains io under the accepted precedence. No automatic background storage, filesystem report spool for standalone payloads, pagination or opaque continuation token is introduced.

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
