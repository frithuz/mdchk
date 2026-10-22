# Shared results and interruption contract

Status: accepted on 2026-10-15. The shared result model, canonical lint
finalizer, schema/semantic validators and lint text/JSON reporters are
implemented and verified. Config validation text/JSON and its operation-specific finalization are also
implemented. Inspection also has operation-specific text/JSON reporting and compiled
acceptance, including observation versus interruption exits. Fix dry-run text/JSON
uses the shared candidate validator and typed v1 changes, deferrals and line diffs,
with compiled production MD047 and synthetic producer acceptance. Apply is implemented on the verified native adapter. MCP and the remaining format
adapters require their own implementation and end-to-end verification.
These requirements incorporate the revised **1 MiB per-file limit** where applicable.
See the [contract index](contracts.md) for related specifications and issue ownership.

## Interruption and exit precedence

### Shared meaning

Distinguish rejected input, incomplete execution and encountered operational failures across CLI and
MCP. Use one shared engine outcome; adapters translate it without inventing independent
limit/cancellation semantics. Preserve completed observations, affected coverage and actual
committed changes.

### Preflight

Malformed invocation, invalid request schema and request limits decidable directly from supplied
input before work starts are usage failures: CLI exit 11, no document processing or writes.
Invalid/unreadable lint/fix configuration retains exit 12 before document processing. Validate known
input and output feasibility limits as early as possible. Bounded filesystem discovery/acquisition
is execution work; a work quota exhausted there is a runtime interruption, not a retroactive usage
error.

### Runtime

Controlled cancellation, deadline expiry or a limit reached during execution produces complete=false
and status incomplete with CLI exit 15 unless a higher-priority actual operational failure applies.
Record an explicit operation interruption reason (cancelled, deadline-exceeded, limit-exceeded), the
relevant limit/phase and available scope attribution. Also describe the dependent checks/sections
left unassessed. These causes alone do not create a usage/io/internal error entry. This deliberately
amends the earlier resource-contract wording that mapped all budget failures to usage/operational
errors.

### Precedence

For lint, fix and preview retain 14 internal > 13 actual file/I/O error > 15 incomplete/interrupted
> 10 violations > 0 clean. Usage 11 and configuration 12 remain preflight failures. Include all
observed errors and interruption reasons even when one code wins. Thus an I/O failure plus a later
deadline returns 13 while retaining both; a deadline plus ordinary violations returns 15.

### Partial observations

Keep findings only when their conclusions remain justified by acquired data. Mark unvisited files,
unfinished inventories and unmet prerequisites explicitly; absence of findings cannot imply those
checks ran. Do not turn unknown or interrupted resource discovery into a missing-target finding.
Retain successful existence observations even when later content acquisition stops. Mark
operation-level termination separately from analysis coverage so config validation and inspection
can explain incomplete execution without fabricating Markdown-rule coverage.

### Cancellation and writes

Check cancellation/deadline before starting each commit. Once observed, start no new replacement and
stop scheduling analysis; use bounded cleanup/reconciliation. An already-started indivisible
replacement may finish and must be reported truthfully, including uncertain outcomes if verification
is unavailable. Do not roll back successful writes or apply previously planned but uncommitted edits
after stopping. Cancellation during candidate construction performs no target writes. Other
already-completed work may appear in the partial report.

### Delivery

Attempt a complete, valid structured partial-result envelope within a reserved reporting/cleanup
budget after work stops. Never cut a JSON document or corrected file string and present it as
complete. A real output-delivery failure is io/13, including when caused by disconnect after
completed writes. For response-size caps use an explicit bounded incomplete-result policy, finalized
with numeric budgets and result schemas, rather than writing first and silently discarding outcome
information.

### Inspect

A completed inspect still returns inspected/0 for observed Markdown/configuration/encoding problems
and unavailable analysis sections, as previously accepted. A controlled interruption that prevents
completion of the requested inspection is instead incomplete/15 with the available inspection data.
Invalid arguments, acquisition/output failures and internal failures retain their existing exits.
Observed source/config errors are still inspection data and do not become operational exit 13/12
merely because cancellation also occurred.

### Config validate

Configuration validation interrupted by a limit/deadline/cancellation is incomplete/15 if no
stronger encountered failure applies. Its Markdown coverage array remains empty; record operation
interruption and incomplete scan explicitly. Preserve existing validation precedence when failures
coexist: internal 14 > traversal/output io 13 > encountered configuration errors 12 > interrupted
scan 15 > valid complete scan 0. An unfinished scan never claims that every definition is valid.

### Cli signals

Handle the first supported CLI interrupt/termination request as controlled cancellation and use the
same incomplete/15 result when graceful reporting succeeds. Do not introduce a competing normal
mdchk exit code for the same outcome. Forced termination, a second forced interrupt, runtime crash
or an unresponsive OS operation can prevent reporting and may retain platform/process-level exit
behavior; never promise a report after an uncatchable termination.

### Mcp

Tool results use the same domain status/completeness/interruption data when a result is delivered;
calls do not return CLI process codes. The selected MCP revision may suppress or constrain a
response to a cancelled request; exact transport delivery follows the [MCP](mcp.md) and [HTTP](http.md) contracts.
Cancellation or disconnection never implies rollback or guarantees zero writes. Preserve actual
outcome tracking even if the client no longer receives the result.

### Limits ownership

Numeric defaults, operator overrides, accounting units, response-size reduction and reserved
reporting budgets follow the [accepted limits](limits.md). Configuration/documents cannot
silently raise server limits; no new per-rule timeout settings are introduced here.

## Shared result schema

The normative [JSON Schema](../contracts/results/result.schema.json),
[fixtures](../contracts/results/fixtures.json), and [validator](../contracts/results/README.md)
define schema version 1 and inspection projection version 1.

### Compatibility

- All listed top-level collections are present, including empty arrays. Optional fields are absent when inapplicable; structural unknowns are not null, fabricated zero values or successful empty checks. A literal null originating in source data can appear inside typed JSON property data; it is not an absence marker.
- Schema v1 is the initial result contract; keep producers and accepted fixtures consistent. Breaking type/required-field/meaning changes require a schema-version increment. New optional fields and documented open reason codes/node kinds can be additive; consumers ignore unknown optional fields and render unknown codes generically.
- Producer/schema validation rejects undeclared fields for the selected schema revision. This does not relax strict input validation; requests still reject unknown fields.

### Semantics

- checked_files stays an array of paths: a snapshot was acquired and entered validation/inspection, including a failing UTF-8 gate. It never means every check succeeded. Resource-only files never enter it. Keep checked_files as paths across all operations; inspection metadata lives in inspection.file and file snapshots rather than changing this array element type. files provides authoritative per-known-target analysis/write/return outcomes; unvisited discovered targets are not-started, and unenumerated paths are not invented.
- skipped_files means intentional selection/policy skips, not read failures, cancellation or missing implementations. Record failed/unfinished targets in files with errors/coverage/interruption instead.
- Diagnostics and located observations identify their exact snapshot (view, SHA-256, byte count). Original, proposed candidate and observed saved coordinates cannot be silently mixed. Public line/Unicode-code-point positions are one-based and end-exclusive; byte offsets are zero-based. The MD094 point location and byte offset keep their accepted special meaning.
- Every acquired document result is tied to byte identity; no host timestamps or random IDs are needed for deterministic analysis output. Object/record IDs are unique within a result and assigned deterministically after canonical sorting, not promised stable across changed input.
- Changes describe atomic validated proposals with before/candidate byte identities and contributing rules. proposed, written and returned are distinct. uncertain means a replacement was attempted but its outcome could not be confirmed; it never counts as a confirmed write. A confirmed write record can remain even if later external changes make the current saved view unavailable.
- File write_status is authoritative for attempted/refused/failed/cancelled/unknown writes, including files with no published correction. Return_status returned requires a matching full documents entry; withheld requires an explicit omission. Full contents appear only for standalone fix, including successfully returned unchanged targets. Excluded/failed/withheld files have no content entry.
- Deferred fixes carry atomic group IDs, precise reasons and conflict/cause links; fix_available requires an implemented correction and a positive context-specific safety/permission eligibility assessment. Lint may assess eligibility without constructing edit proposals and remains read-only. Planned/unavailable corrections report false; in fix results a conflicting or unsafe deferred correction reports false.
- Diffs are structured line hunks with three context lines. Each line retains kind, exact text without its terminator, and explicit lf/crlf/cr/none ending; headers use original physical line numbering. Text adapters render familiar unified diffs and no-final-newline markers. Full returned content and before/candidate digests, not a normalized display diff, identify exact bytes.
- errors preserves the four stable categories usage/configuration/io/internal plus a stable code and message. scope=observation is used for errors revealed by a completed inspect; scope=operation affects execution status. One underlying resource failure can link to several coverage records without duplicate error inflation.
- Coverage reasons extend the existing rule-unavailable/parser-uncertain/context-unavailable set with decoding-failed, saved-view-unavailable, fix-nonconvergent, cancelled, deadline-exceeded and limit-exceeded where applicable. Operation interruption is also separately represented so config validation does not invent Markdown coverage.
- Notes hold located informational/parser/directive explanations that are neither rule violations nor operational failures. They must not replace required coverage/error records. Unknown metadata/Quarto context stays incomplete under the accepted rule policies.
- Omissions identify the affected JSON-pointer section, optional file and exact omitted count when known. Unknown count is omitted, never zero. Keep referenced cause/error summaries when omitting detailed records so retained IDs never dangle. Any omission/interruption forces complete=false; preserve mandatory per-file write outcomes and reserve them before commits.
- Completed inspect uses inspected and can have complete=false plus observational errors. Interrupted inspect uses incomplete unless an operational failure overrides it. Configuration validation can finish scanning with complete=true and status=error for invalid definitions; scan_complete and definition counts distinguish that from interrupted scanning.
- Inspection sections use availability available/partial/unavailable/not-requested, explicit reasons, and data only when available/partial. Missing AST/rule data cannot be represented as a successful empty array. Counts are unavailable when effective directive state cannot be resolved; when available active+inactive=known catalog size.
- AST projection exposes source-backed node kinds, spans, ordered raw attributes, typed JSON properties and children, with context-only ancestors marked. It is independent of private Go parser structs. The parser/format owners must document stable node-kind/property meanings and preserve the already required raw/effective IDs, source notes/table cells and HTML origins; private pointers/addresses are forbidden.
- Inspection rule entries separate configured state, implementation availability, effective state and actual active/observational execution. Directive entries retain raw text, closing offsets and per-rule permitted/blocked/inapplicable effects; suppressed diagnostics refer to responsible directive IDs or configured disablement.
- Sort target and document records by path, diagnostics by path/start/rule with deterministic tie-breaks, errors/coverage/notes by absent-first path/position then rule/code. Preserve source order inside AST children, raw attributes, directives and diff hunks. Sort rule lists by canonical ID. Do not reorder source lists to make output look canonical.

### Validation invariants

- No duplicate record IDs or duplicate per-file identities; all referenced IDs resolve to the appropriate record family.
- Diagnostic ranges are ordered and consistent with snapshot/source coordinates; available rule counts sum to catalog size.
- Returned content UTF-8 bytes match its snapshot size/hash; its FileResult return_status is returned. Withheld/skipped/failed target content must be absent.
- Confirmed written changes require written file outcomes and apply mode; dry-run can contain only proposed corrections and never written file outcomes.
- Read-only operations have no changes or deferred edit proposals. MCP modes expose only lint/fix; inspect/config validation are CLI operations.
- Complete=true forbids coverage deficits, interruptions and omissions. Status/exit follow the previously accepted operation-specific precedence, not array emptiness alone.
- Diff hunk line counts match their context/remove/add records and line text excludes CR/LF terminators. Do not use display formatting to hide raw line-ending changes.
- Project/CLI outputs contain no corrected content strings; standalone fix content includes unchanged successfully returned targets and excludes resource-only entries.

## Implemented lint presentation

The CLI presents the canonical shared result as readable text or one undecorated
schema-v1 JSON object, including every mandatory collection. Both formats use
stdout; stderr is reserved for execution logs. Text escapes CR/LF in every
line-oriented user field and preserves Unicode scalar coordinates. JSON retains
original strings and same-read source identities. Usage and configuration
failures retain the selected valid presentation, including format options after
an earlier malformed option; `--` still ends option recognition. VS Code/GitHub
formats are rejected before project access until their adapters are implemented.

Actual encoded response bytes, including JSON escaping and the final newline,
are bounded. Whole optional records are reduced deterministically, recording
exact known omission counts and incomplete status. Detailed errors may be
replaced with identity-preserving summaries, recording exact `/errors` omission
counts while retaining categories, IDs, paths, snapshots and ranges. Encountered
errors and file outcomes remain authoritative, and canonical finalization
rebuilds retained references. Before analysis, report feasibility requires the
minimum reserve plus the encoded mandatory target manifest. Genuinely known
request/envelope infeasibility is preflight usage/11. CLI paths are selection
requests rather than known target records, so preproject feasibility checks
only the known empty envelope. After discovery, infeasible actual target
manifests stop before acquisition/analysis as incomplete/15 with exact `/files`
and `/coverage` output-limit omissions, retaining encountered errors. Reporting uses an independent cleanup deadline after analysis stops
and pre-encodes an essential partial fallback for deadline expiry.
Short writes and other delivery failures return IO/13 unless internal/14 already
applies; failed delivery does not promise a complete JSON object. Only enforced
applicable limits are exposed, with operator/default provenance.

Emitted CLI JSON is checked against both the schema and semantic validator;
these are complementary because JSON Schema cannot establish cross-record
state, ID and hash invariants.

## Remaining verification

- Extend schema-backed producer and semantic tests to every remaining operation
  and reporter; preserve cross-record/state/hash invariants.
- Complete the stable AST node/property catalog within #139 using the established CommonMark/GFM/Pandoc source contracts; validate representative dialect projection fixtures.


Extend the implemented bounded lint serialization to the remaining adapters
against these shared fixtures.

Config validation preserves completed invalid file analysis through canonical
ID assignment and every reporting reduction. The typed schema-v1 summary counts
completed valid/invalid decisions; interrupted acquired sources remain in
`checked_files` with partial outcomes but no invented validity decision.

Read-only configuration reports can reduce oversized known `files` and
`checked_files` collections with exact omissions. If error identities alone
exceed the bound, explicit category/count summaries preserve all encountered
error categories and precedence, with `/errors` omissions for the replaced
located details. This is configuration-specific; lint's pre-admitted essential
manifest and future write outcomes are not discarded. Response quotas are
interruptions, while actual output-delivery failures remain IO.

Configuration validation additionally pre-encodes a fixed-record emergency result
before starting timed full-fallback/detail construction. Error-category counts
are accumulated during canonical finalization. The emergency result preserves
actual scan/validity counts and every encountered category, with exact omissions
for source records/details, even if the reporting deadline expires before the
full fallback exists. It retains any operation interruption and adds reporting
deadline attribution. The response bound still applies, and actual failed output
retains IO/internal precedence. See the [reserve timing boundary](limits.md#configuration-emergency-reporting-reserve).

CLI apply exposes optional `files[].leftovers` for owned native staging siblings
that cleanup could not remove. These names are essential outcome evidence and
survive reporting deadlines. Saved diagnostic and suppressed-diagnostic positions
match the exact saved `result_snapshot`; a candidate snapshot is never substituted
for unavailable saved content.
