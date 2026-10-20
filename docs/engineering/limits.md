# Shared resource limits

Status: accepted on 2026-10-15. Parser-layer budgets are implemented and
verified as described in [parser accounting](parser-limits.md); remaining
adapter/application limits and end-to-end verification remain pending.
These requirements incorporate the revised **1 MiB per-file limit** where applicable.
See the [contract index](contracts.md) for related specifications and issue ownership.

These are finite application-work budgets, not a process memory or throughput guarantee.
The [machine-readable profile](limits.json) carries exact values and accounting scope.
MiB = 1,048,576 bytes; KiB = 1,024 bytes.

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
