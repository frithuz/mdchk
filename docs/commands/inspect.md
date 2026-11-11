# mdchk inspect

Reveal how mdchk processes one Markdown file: its identity, parser output, rules, findings, directives, and any limits on reliable parsing. Inspect is read-only and reports processing details rather than determining whether the file passes validation.

```text
mdchk inspect [options] <file>
```

Exactly one file path is required for inspection; valid help/version requests bypass
that requirement. Options may appear before or after the target; use `--` for
dash-prefixed paths. Paths may be absolute or relative to the working directory. Directories are not accepted.

```sh
mdchk inspect README.md
mdchk inspect /path/to/project/docs/guide.md
mdchk inspect docs/guide.md --lines 18-30
```

These examples assume the [current reduced setup](../index.md#get-started-with-this-build):
a CommonMark+MD047 configuration and readable fixture paths. GFM is also
available; every default check runs for either flavor. Inspect completes observations with `0`
even when configuration or check availability is unresolved. Replace
`/path/to/project` with a real project path and ensure line-range examples have
enough physical lines.

MD088 ASCII-punctuation analysis is available for CommonMark and GFM, disabled
by default. Full original raw/link/image context is analyzed before the selected
line intersection; findings use exact original scalar ranges and shared directive
START states. The rule emits no correction or preview in this pass.

## Options

| Option | Effect |
| --- | --- |
| `--limit NAME=VALUE` | Set a finite operator [resource limit](../engineering/limits.md); repeat for multiple limits |
| `--lines N` | Focus on one line |
| `--lines N-M` | Focus on an inclusive range of lines |
| `--config PATH` | Select a configuration inside the target's discovery boundary |
| `--format text\|json` | Choose readable inspection output or structured data; defaults to `text` |

The [global options](../index.md#global-options) also apply. Line numbers start at `1`. Bounds must be positive, ordered, and within the file. Omit `--lines` to inspect the whole file. Empty files can be inspected without a line range.

## File and processing details

Every inspection includes:

| Detail | Meaning |
| --- | --- |
| Resolved path | Full absolute path on the host, resolving symlinks |
| Binary version | Exact version selected for `--version`, including the `dev` fallback; see [version policy](../index.md#global-options) |
| File size | Human-readable size followed by the exact byte count, such as `1.5 KiB (1536 bytes)` |
| SHA-1 | Checksum of the full file's original bytes |
| SHA-256 | Checksum of the same original bytes |
| Markdown flavor | Effective flavor from configuration, including whether it came from defaults |
| Abstract syntax tree | Parsed nodes, nesting, source locations, and relevant attributes |
| Rules run | Rule identifiers, effective options, execution status, and coverage relevant to the inspected area |
| Findings | Reported issues with locations, rule identifiers, and fix availability; suppressed findings are identified separately |
| Rule counts | Total active and inactive rules in the inspected area |
| Directives | Exact directive text, source locations, affected rules, and effective scope |
| Parsing notes | Unsupported or ambiguous syntax, uncertain regions, and other reasons processing may be incomplete |

Metadata and checksums describe the entire file even when a line range is selected. A note that no findings were reported does not imply complete parsing coverage; check the parsing notes and rule execution details.

## Focus on a region

The whole document is parsed before the display is narrowed. This preserves context from enclosing lists, code blocks, and directives elsewhere in the file.

With `--lines`, the AST includes nodes overlapping the selected lines, plus their ancestors for context. Source locations remain the original file's locations. Rule details and findings focus on that region. File-wide rules appear only when their relevant source location overlaps it.

Counts use the known rule catalog, including unavailable implementations: a rule is active when enabled for at least part of the selected region, and inactive when disabled throughout it. Partially disabled rules count once as active, with disabled subranges listed. Active does not necessarily mean executed: the rule may not apply to the syntax there, its implementation may be unavailable, or parsing uncertainty may prevent a reliable check. Execution status explains the difference.

The directive list includes directives within the region and any earlier directives still affecting it. When directives disable rules, inspect names the exact rules and the precise source directives responsible, including their locations and affected lines. A directive outside the selected lines is labeled as context. Unknown or malformed directives appear with explanatory notes rather than disappearing from the report.

See [directives](../directives.md) for comment syntax, onward scope, and global or per-rule permissions. Inspect distinguishes allowed changes from requests blocked by configuration.

## Configuration and excluded files

Without `--config`, inspect discovers configuration upward from the resolved file's directory, stopping at the repository root when identifiable, or the filesystem root otherwise. This lets an absolute target outside the working directory use its own project's expectations.

Inspect can reveal an explicitly named file even when normal lint or fix selection excludes it. The report identifies the exclusion, then processes the file using the effective rules. It never broadens MCP access permissions.

If configuration is invalid, inspect shows the configuration problems and marks flavor and rule data as unresolved. It still reports available file metadata, checksums, and raw directive candidates. It does not silently substitute default parsing or claim an AST based on the requested configuration.

See [configuration](../configuration.md) for settings and [result formats](../output-formats.md) for structured inspection output.

## Understand the result

A completed inspection returns `0`, regardless of Markdown findings, directive problems, configuration problems shown in the report, or parsing notes. It does not return validation codes `10`, `12`, or `15` for those observations. Use [lint](lint.md) or [config validate](config/validate.md) when you want a validation result.

Errors that prevent inspection still use the operational codes: `11` for missing or extra arguments, a directory target, or invalid line bounds; `13` for an unreadable or missing target or output failure; and `14` for an internal execution failure. An unreadable configuration is shown as unresolved configuration within the inspection when the target itself is readable. See [exit codes](../exit-codes.md).

Runtime limits, deadlines and cancellation instead return `15` unless an operational error takes precedence. An acquired invalid UTF-8 snapshot can still produce a completed inspection: exit `0`, status `inspected`, `complete: false`, byte metadata and a decoding observation per retained invalid sequence, with parsed sections unavailable. A file capped at its [retained error count](../engineering/limits.md#encoding-error-retention) keeps that exit and status and adds an exact `/errors` omission for the file.

## Current implementation

This build implements the text/JSON command using one shared snapshot, analysis
and line-filtering budget. CommonMark and GFM are available; unavailable selected flavors
and missing ordinary rule implementations remain visible observations. Opaque
frontmatter recognition is reported in document context.
Resources remain not requested.

Applicable overrides are `document-bytes`, `total-content-bytes`, `config-bytes`,
`total-config-bytes`, `structured-input-depth`, `syntax-depth`, `document-nodes`,
`total-nodes`, `work-units`, `operation-time`, `response-bytes`, `reporting-time`
and `report-reserve-bytes`. Other limit names are rejected before project access.

Reports size the owned typed projection with deadline checkpoints before encoding
JSON, and render text directly from the typed view. Bounded counting stops once
admission is impossible; it does not encode an oversized AST to learn its size.
Text visibly escapes terminal controls in paths and source data while JSON keeps
its structured string identities. Reports omit the whole optional AST first
when details exceed the encoded byte budget. Further reduction omits whole linked rule/directive/suppression sections
and other detail collections, retaining the acquired target identity and outcome
with exact known omission counts. A pre-encoded emergency report supports even
positive reporting deadlines of `1ns`: its fixed reserve construction and output
delivery are outside that timed detail interval. Reporting omissions return
incomplete/15. A true failed or short output write returns 13, unless an already
established internal operation error requires 14.

Before analysis starts, the reporting reserve and the now-known target identity
must fit together. If acquisition reveals an infeasible manifest, inspection
retains the acquired metadata, leaves analysis explicitly not started, and
returns incomplete/15 without invoking rule callbacks.


The JSON `document_context` section reports only opaque `frontmatter` recognition (`disabled`,
`absent`, `yaml`, `toml` or `unterminated`). This context does not decode metadata
or acquire resources. Unresolved header recognition stays unavailable; it never becomes ordinary false.
Line filtering keeps the document context. Output limits can omit the whole section.

The `Metadata` text section and JSON `metadata` section report what a recognized
header decodes to. `state` is `disabled`, `absent`, `unterminated`, `complete` or
`incomplete`; the first two carry nothing else, and an unterminated header carries
only its format and location beside the existing parsing note. A decoded header
also lists located `problems` (`malformed`, `unsupported`, `duplicate-key`), a
`title` decision (`found` with its text, `missing` or `unknown`), and `ordering`:
each mapping checked under MD072's effective `recursive` and `case_sensitive`
options, whether it is out of order, and its `offending_keys`.

These are observations, not findings: they appear even with MD072 disabled and
change no rule state, count, `diagnostics` or `complete`. Decoding shares the
inspection's limits and runs after rules, so a limit reached there returns
incomplete/15 with this section unavailable and completed sections intact. Line
filtering keeps the whole record and flags it `context_only` or `extends_region`
like the opaque header. Text escapes terminal controls in decoded strings; JSON
keeps them. Exact field rules are in
[inspection projection version 1](../engineering/inspection-projection-v1.md#metadata-record).

If unrelated parser or
directive work is unavailable, a partial rules section can appear; this does not
claim full rule counts, body syntax or complete analysis.

MD029 is available for both flavors, with admitted `ordered` or `one` style.
Inspection reports analysis availability and implemented fix metadata. `fix`
corrects eligible full numeric marker findings; deferred ones stay manual.

MD030 is available for both flavors with positive integer spaces (default 1).
Inspection exposes analysis availability and ImplementedFix metadata: eligible
single-line separators are corrected, and other findings stay manual.

MD032 is available for both flavors. Inspection exposes complete source-body
boundary analysis. PlannedFix metadata grants no edits or correction previews.

MD069 inspection identifies available default-enabled CommonMark/GFM analysis.
Its maximal repeated-marker ranges remain manual and never grant fix authority.

MD076 inspection reports available default-enabled CommonMark/GFM analysis and
its admitted exact `style`. Hidden siblings still establish the per-list baseline;
suppressed findings use their original deficit or first-extra-line surplus range.
Inspection grants no MD076 correction authority.


MD077 inspection identifies available default-enabled CommonMark/GFM analysis
and findings. Selection slices findings after complete structural context
and protected-line eligibility are established. Inspection itself makes no edits.

MD009 inspection exposes available analysis with original physical suffix ranges,
manual findings and START-based directive attribution. Line selection retains full
parser context. It adds no AST properties or inspection schema.

MD010 inspection exposes available manual analysis and admitted scalar options.
It classifies full original source ownership before selecting lines; each finding
covers one original TAB byte and one scalar column. CodeSpan stays protected
with code_blocks enabled. No MD010 correction proposal or content is returned.

MD012 inspection exposes available analysis and admitted `maximum`.
Full original physical runs and protected context precede line slicing and START
suppression: hidden earlier blanks consume the allowance, and selecting a later
suffix cannot invent a finding. Empty-body ranges can have equal byte endpoints.
Inspection returns no correction proposal or corrected content; MD012 runs are corrected by [`fix`](fix.md).

MD013 inspection exposes available but default-disabled manual analysis and all four admitted options. Full original physical width and protected/URL context precede line slicing. Each finding selects one original overflowing scalar, retaining exclusive scalar coordinates and exact snapshot identity; no MD013 correction or returned content is produced.


MD027 inspection exposes available default-enabled analysis. Full final Paragraph, actual quote-prefix and literal/raw ownership precede slicing. Findings select original excess ASCII spaces after the first SPACE; any TAB in the whole separator exempts it. Inspection produces no correction or returned content.


MD028 is available and enabled by default. Full original same-parent quote neighbors and protected gap context precede slicing and START suppression. Each qualifying physical line selects its remaining blank body, including a present zero-width position after an atomic retained prefix. Selecting only the later quote does not relocate a gap finding.

MD064 is available and enabled by default. Full actual Paragraph/Heading ownership,
transparent word witnesses and raw/protected context precede slicing. Findings use
only the surplus original SPACE range START and retain earlier directive state.

MD085 is available but disabled by default. Full original Paragraph content fixes the baseline before directive filtering at the surplus START and line selection. Protected multiline prefixes and actual HardBreak successor exemptions retain their outside-selection context. Inspection reports manual findings and never creates edits.

MD089 establishes complete original prose ownership and raw lifetime before
filtering each zero-width insertion START. Hidden or unselected earlier context
remains binding; line selection cannot create source adjacency or reset raw state.

MD035 is available and enabled by default for CommonMark and GFM. Its `style`
option accepts exactly `consistent` (default), `dash`, `asterisk` or `underscore`.
Consistent compares exact original first-to-last marker bytes against the global
first eligible break; named styles require only the marker family. Full protected
and earlier disabled context precedes suppression and line selection. Findings
remain manual with no MD035 edits or previews.

MD037 is available and enabled by default for CommonMark and GFM, with no
rule-specific options. Shared `enabled` and `allow_directives` remain strict
booleans, including when disabled. It analyzes full-source ordinary Text islands
before directive or line filtering and reports each original inside-whitespace
edge separately. Findings are manual; this analysis produces no edits.

MD038 is available and enabled by default for CommonMark and GFM, with no
rule-specific options. Shared `enabled` and `allow_directives` settings apply;
unknown options fail even when disabled. Analysis first resolves actual CodeSpan
ownership, normalized value and original paired ASCII SPACE edges over the full
source. Each excess edge range uses its own start for directive suppression;
selected inspection lines filter results after full-source analysis. All findings
remain manual, with no correction eligibility, edits or previews.

MD033 embedded-HTML analysis is available for CommonMark and GFM, disabled by
default and report-only. Its full original named-tag inventory and global raw
context precede finding-START suppression and selected-line intersection. A
permitted enable directive uses the admitted `allowed_tags` string list; inherited
global/per-rule denials remain binding. Allowed raw names still protect payload.
Failed named context in an actual HTMLBlock withholds the entire MD033 inventory
with explicit coverage. No MD033 edits or previews are produced.

## MD086 unmatched comment analysis

MD086 is enabled by default, has no rule-specific options and is permanently
report-only for CommonMark and GFM. Full actual HTML-owner context is computed
before shared START suppression and inspection selection. A permitted complete
disable can suppress a later opener; an unclosed directive-looking comment
cannot suppress itself, and an apparent enable inside comment payload is not an
instruction. Inherited directive denial preserves configured state. Findings
cover only the four-byte original opener, so selecting later payload lines does
not intersect the finding. Missing owned named-token context withholds the whole
inventory with coverage. Existing directive comment recognition is unchanged.

## MD087 suppression audit

MD087 analysis is available for CommonMark and GFM, disabled by default, with
no options and permanent report-only support. Inspection's optional per-effect
reason distinguishes used, unused, uncertain, self-excluded and inactive-audit
targets while retaining ordinary state, permission, effect and provenance.
Join each effect's rule to its first matching named token and the directive ID
to owned rule intervals; final intervals own EOF. Full-document conclusions
precede focus, retaining preceding owners as context_only.

The audit produces no edit or preview. A separately authorized MD047 correction
recomputes the audit against candidate source with the original residual budget and
against actual saved source under the [saved-run allowance](../engineering/limits.md#saved-fix-runs). Hidden observations bypass correction eligibility; ordinary
active MD047 corrections retain their established behavior.

MD092 evaluates conflict state on the full original source before inspection
focus. A selected separator can depend on an earlier hidden or out-of-focus
opening; a hidden closer still clears state. Focus does not strip container
prefixes or reset the Boolean. Unknown original protection context withholds the
whole MD092 inventory, and MD087 reports uncertain suppression usefulness instead
of treating the missing prefix as proof of absence. MD092 remains report-only.

MD048 fence-delimiter analysis uses the full original document to establish its
global consistent baseline before applying inspection focus. A mismatch spans
only its opening marker: focusing the payload or closing fence alone does not
select it. Inactive observations and directive-hidden findings retain the shared
inspection behavior. Findings remain manual; no delimiter replacement preview is
produced.
