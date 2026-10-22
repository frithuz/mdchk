# Inspection engineering design

`mdchk inspect` exposes processing details for exactly one file without modifying it or treating observed problems as validation failure. The [command guide](../commands/inspect.md) defines the user-facing report and line-range behavior.

## Confirmed requirements

- Require one absolute or relative file path.
- Support `--lines N` and `--lines N-M` to focus AST, rule execution, findings, rule counts, and directives.
- Report resolved host path, binary version, readable and exact size, SHA-1, SHA-256, configured Markdown flavor, AST, rules run, findings, active and inactive rule counts, directives, and parsing notes.
- Identify the exact rules disabled by directives affecting the inspected area.
- Reveal processing details without making a validation pass or fail decision.

## Snapshot and identity

Resolve the target to an absolute canonical host path, following explicit symlinks, and require a regular file. Read one immutable byte snapshot and derive size, digests, line offsets, AST, directives, and findings from it. Use standard-library path and hashing packages. Do not hash normalized newlines or rendered Markdown. A file changed during acquisition must be retried within a bounded policy or reported as an acquisition error, rather than combining data from different reads.

Render sizes in binary units with an exact parenthesized byte count. In JSON, keep `size_bytes` as an integer and `size_display` as a string; encode SHA-1 and SHA-256 as lowercase hexadecimal. Report the build's binary version, using an explicit development identifier when a release version is unavailable.

Inspection follows explicitly named symlinks for read-only analysis; directory scans and project MCP retain their existing symlink restrictions. Record whether normal file selection would exclude the target, while allowing this command to inspect it. Do not reuse this exception to expand MCP filesystem access.

## Configuration and parser instrumentation

Use the shared configuration loader and precedence, with discovery starting at the canonical target's parent directory rather than the invocation directory. Record configuration provenance, effective flavor, and rule options. Include effective global and per-rule directive permissions and the configuration paths and fields imposing restrictions, including ancestors. Distinguish an inherited permission denial from configured rule disablement. Add a validated `flavor` setting with values `commonmark`, `gfm`, and `pandoc`, defaulting to `gfm`; the parser and all operations must use the same flavor selection.

Expose a versioned AST projection with node kind, source span, relevant attributes, and children. Preserve byte offsets and documented public coordinates. Include a full document root and source-backed nodes without generating an HTML representation. Ancestors included only for context must be explicitly marked. Do not recursively serialize private Go structs, pointer addresses, or unstable implementation fields.

Instrument rule execution independently of findings. Record known catalog IDs, implementation availability, configured enablement, directive suppression spans, eligibility in the selected area, actual execution or skip status, and skip reasons. Do not infer that a rule ran because a finding exists, or report that it did not run merely because there are no findings. Unavailable implementations remain visible through catalog metadata.

Use the same directive recognition, scopes, and suppression decisions as lint and fix. Preserve raw directive text, resolved rule IDs, source location, effective ranges, and links from suppressed findings to the responsible directives. Keep suppressed observations separate from emitted findings. Unknown directives and unresolved rule references produce notes. Raw candidates within code or other protected syntax must be distinguished from effective directives.

The [directive processing design](directives.md) defines grammar and scoping. Inspect exposes the engine's shared semantics rather than implementing a separate suppression interpreter.

Resource inspection uses the same [root and lookup contract](resources.md) as analysis, starting root discovery from the canonical target's parent. Record root/policy provenance, logical target identity, lookup outcome, and coverage without exposing unrelated contents or resolving blocked paths beyond the permitted context. The explicit-target symlink exception applies only to acquisition of that target.

For Pandoc, expose source-backed footnote references and definitions independently of resolved note bodies, raw and effective attributes/IDs, original table cells and separators, and HTML-origin/container distinctions. The [Pandoc source contract](pandoc-compatibility.md) applies to the versioned projection; semantic normalization must not erase source facts required by rules.

## Line ranges

Parse and analyze the full snapshot before filtering presentation. Validate the requested inclusive line interval against the line index. A terminal newline does not create an extra selectable content line; an empty file has no selectable lines. Convert the interval into source spans with consistent handling of CRLF and end-of-file.

Include intersecting AST nodes and context ancestors. Include findings whose source ranges intersect the interval, treating zero-width findings as positions. Show which returned nodes or findings extend beyond the selected lines. Record rule evaluation source coverage so rules checked elsewhere can be distinguished from rules relevant to the region. File-wide checks need a defined diagnostic anchor or applicable range.

For each known catalog rule, compute whether any selected source position is enabled after configuration and directives. Count it once as active if any position is enabled, otherwise once as inactive. Include partial suppression ranges and reasons, independently of whether implementation availability, syntax applicability, or parser uncertainty permits execution. When configuration is resolved, active plus inactive must equal the known catalog size; never count a rule multiple times because it ran on several nodes.

Include directives intersecting the interval and directives outside it that still affect its rule state. List each resolved ID for directives naming multiple rules. Preserve exact directive attribution when transitions overlap. Parser notes remain visible for the whole document, labeling whether each note intersects the region or affects its interpretation through context.

## Incomplete processing and result semantics

Invalid or unreadable configuration is inspection data: retain metadata, digests, raw directive candidates, and configuration errors. Mark effective flavor, AST, rule execution, counts, and findings as unavailable where they cannot be determined. Do not silently use default flavor or fabricate empty successful sections. Parser uncertainty similarly produces coverage notes and explicit skipped checks.

Expose `--format text|json` only. Text uses labeled sections for all requested details. JSON uses operation `inspect` and a versioned `inspection` object within the shared envelope, including file identity, selected lines, configuration provenance, flavor, AST, rules, rule counts, directives, parsing notes, and per-section availability. Preserve shared `diagnostics`, `coverage`, categorized `errors`, and `complete` fields; add `suppressed_diagnostics` for observations removed by directives. Mirror file metadata in `checked_files` only after acquisition succeeds.

A completed inspection uses status `inspected` and exits successfully even when `complete` is false or observed problems appear in `errors`. For this operation, completeness describes available processing data, not validation success. If argument validation, target acquisition, output delivery, or an internal failure prevents inspection, use the operational error status and codes from the command guide. Do not map observed Markdown findings or configuration problems to validation exit codes.

## Verification

Test relative and absolute targets, explicit symlinks, empty files, Unicode, CRLF, checksum consistency, build version, excluded targets, target-relative configuration discovery, configured flavors, invalid configuration, line boundaries, multiline nodes, zero-width findings, contextual directives, overlapping transitions, multi-rule disabling, blocked directives, rule counts, parser uncertainty, and JSON stability. Verify lint and inspect agree on active findings and directive behavior for the same snapshot and configuration. Assert no writes and successful process status for completed inspections containing observations.

The [result contract](results.md) and [schema/projection fixtures](../contracts/results/README.md) define exact section availability, snapshot identities and rule-state fields. Invalid UTF-8 can produce a completed byte-level inspection with unavailable parsed sections; runtime interruption uses incomplete status and exit 15 unless an operational error overrides it. `checked_files` always contains path strings; file metadata belongs in `inspection.file` and `files` snapshots.

## Implemented snapshot foundation

`internal/inspect.Acquire` implements the read-only snapshot and configuration
foundation. `Analyze` now supplies the stable AST, actual rule execution and
shared directive projection described in [projection version 1](inspection-projection-v1.md).
`Filter` adds physical-line presentation and catalog counts while preserving the
full analysis authority. The CLI now supplies text/JSON reports with compiled
end-to-end acceptance. The caller supplies the build version (an empty version is
reported as `dev`, following the [version policy](../index.md#global-options)),
registry and implemented capabilities.

Acquisition makes one attempt, follows the explicitly named target's symlinks,
and then uses the shared pinned directory/regular-file reader. It checks file
identity, size and modification time before and after reading and verifies the
selected path again. A changing file or replaced component returns an acquisition
error without retrying; no metadata combines separate target reads. These checks
cannot detect an adversary rewriting and restoring identical metadata during the
read; inspection does not promise an atomic filesystem transaction. Ordinary
selection and MCP access keep their existing restrictions.

The immutable source, physical-line index, size and both checksums describe the
same acquired bytes, including malformed UTF-8. Returned configuration, source
identity and exclusion-origin views are owned copies; later disk changes do not
affect them. Configuration discovery begins at the canonical target parent;
relative explicit configuration remains relative to the invocation directory.
The existing repository/caller boundary and strict ancestor policy apply.

`config.ResolveValidatedBounded` shares the operation's context, elapsed time,
work and construction budget. Configuration source and aggregate byte caps are
independent of the Markdown byte cap. Effects, expanded YAML semantics, provider
callbacks, hashing, line indexing and initial owned views reserve work before
construction. Runtime cancellation and limit exhaustion preserve typed causes.
An invalid or unreadable definition remains a configuration observation with
available target metadata and only successfully read configuration identities.
It does not yield substituted defaults or a known-negative exclusion decision.
Fully validated settings and permission provenance remain available when build
capability admission fails; the admitted execution bridge is then unavailable.
A normally excluded explicit target is still acquired. Normal lint/inspect
exclusion status uses only the selected definition's patterns and defining
directory; ancestor configurations contribute validation and inherited permission
denials, without merging their exclusion patterns.
