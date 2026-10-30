# Directive processing design

The [directive guide](../directives.md) defines the source syntax, state transitions, configuration permissions, and observable behavior. One shared directive processor must serve lint, fix, inspect, and both MCP modes.

## Confirmed requirements

- Inline comments can disable a rule from their location onward.
- Inline comments can enable a rule from their location onward.
- Configuration can disable all directives or prevent directives from changing specific rules.
- Ancestor directive prohibitions remain binding on descendant configurations; children may add restrictions but cannot relax them.

Directives express exceptions to particular rules within a document. Whole-file exclusion from all rule linting belongs to configuration file selection, not a synthetic directive disabling every rule. Preserve the explicit rule-list grammar and do not introduce an all-rules directive or infer file exclusion from rule state. This is a usage distinction: ordinary permitted per-rule transitions retain their documented onward scope.

## Recognition and validation

Recognize directives only in parser-confirmed HTML comment source spans outside protected literal content. Trim surrounding comment whitespace, then require the exact command `mdchk:disable` or `mdchk:enable` followed by one or more whitespace-separated known catalog rule IDs. Treat names as case-sensitive; do not accept aliases, commas, wildcards, omitted rule lists, or an all-rules token. Permit multiline comments. Deduplicate repeated names within a directive.

Preserve raw text, comment span, closing-marker offset, operation, named and resolved rule IDs, and per-rule permission decisions. Validate the full directive before applying it. An unknown rule or malformed command invalidates the entire directive, producing a located diagnostic with identifier `directive`. This is an engine diagnostic rather than a configurable rule; it cannot be disabled by file content and is not counted in the rule catalog totals. A known unavailable rule is distinct from an unknown ID: if a permitted directive enables it in an applicable region, retain the state transition and report `rule-unavailable` coverage for the required check. Never claim execution or fabricate a violation.

If global directives are disabled, retain candidates for inspection but do not interpret their syntax as directive violations. Valid directives naming protected rules produce blocked-effect notes for those rules while allowed effects are applied. Protection is not a syntax error. Uncertain parser context produces a parsing note and no transition. In incomplete or unresolved configuration, do not guess directive permissions.

Use the selected flavor's source spans. A resource's directives never alter its referring source, resource access, shared anchor/index policy, or execution budgets.

## State and configuration

Add configuration object `directives` with boolean `enabled`, default true. Add boolean `allow_directives` to each rule's configuration, default true. Both fields use the shared schema for on-disk and supplied MCP configuration; unknown names and incorrect types are configuration errors.

Resolve inherited directive permissions through the shared configuration loader as defined in the [configuration design](configuration.md#inherited-directive-permissions). Preserve the ancestor path and field responsible for each denial. Child true values do not relax inherited false values and do not produce configuration errors. Rule enablement and options still come from defaults and the selected configuration.

Initialize each rule from effective configuration. A directive is permitted only when global directive use and that rule's permission are both enabled. Enable explicitly sets state to true even if the configured initial state is false; disable sets it to false. Effective rule options remain unchanged. Protected rules therefore retain their configured state throughout the file.

Build per-rule state intervals over the original source snapshot. Changes begin immediately after the comment's closing marker and last until the next permitted transition for that rule or end-of-file. Use source order rather than nesting. Store the responsible directive for each changed interval, including no-op repeated requests so inspection can explain the last explicit instruction. State resets for each document and request.

## Findings and corrections

Evaluate full-document syntax and any context a rule requires even across disabled intervals. Suppress a rule finding when the rule is disabled at the finding's start position. Define a source anchor for every file-wide diagnostic, including end-of-file positions, so transitions are deterministic.

For inspection, retain suppressed observations separately with references to the responsible directive or configured disablement. A rule disabled everywhere need not run during ordinary lint or fix; inspection may evaluate it for explanation, clearly labeling that evaluation as observational rather than active execution. Keep active and inactive counts based on effective state, not evaluation effort.

Do not apply an edit if its diagnostic anchor is disabled or its replaced span crosses any disabled interval for its rule. For an insertion, test the insertion position. Do not rewrite or remove directive comments as an incidental rule fix. After fixes, reparse directives and rerun shared analysis against the candidate content; reject edits that unexpectedly change directive structure or permissions.

Expose directive diagnostics and parsing or policy notes through the shared result model. Directive findings contribute to lint and fix violation outcomes; policy-blocked requests are notes. Completed inspect remains successful regardless of these observations.

## Inspection and verification

Link rule state, suppressed diagnostics, and fix decisions to exact source directives. Region filtering must include earlier transitions still affecting the selected lines. Distinguish effective directives, blocked directives, invalid directives, and literal candidates. Do not report a blocked request as the cause of an actual rule-state change.

Test inline and standalone comments, multiline syntax, code and front matter exclusions, parser uncertainty, rule-name validation, directive ordering, repeated transitions, partial permission lists, global blocking, enabling configured-off rules, protected configured-on and configured-off rules, file boundaries, end-of-file anchors, crossing edits, and line-range attribution. Check consistency across CLI and MCP and verify config validation covers both permission fields.

## Special rule effects

MD074 is project-scoped and MD094 controls pre-parse UTF-8 error attribution; directives targeting either have an inapplicable effect. A valid mixed-rule comment still applies its other permitted effects. UTF-8 validation cannot be disabled.

MD087 observes otherwise-enabled target rules without disable intervals, on the original source and shared budgets. Last permitted disable instructions own intervals, including repeated disables. A proved hidden finding establishes use; only complete absence establishes an unused suppression. Missing context, unavailable rules or interruption withhold that conclusion and report incomplete coverage. See the [counterfactual contract](rule-behavior.md#unused-suppressions).

## Implemented shared foundation

`internal/directives` recognizes candidates and builds owned rule-state intervals
from parser-confirmed comment facts and immutable configuration authority. Its
README documents candidate classification, Unicode whitespace, original byte
anchors, EOF ownership and incomplete outcomes. Parser collection rejects
constructed or unfinished documents and uses the caller's existing operation
budget. Block comment boundaries are separate from physical HTML-block envelopes.

The shared lint engine now processes these intervals before scheduling callbacks
and calculating missing-rule coverage. The implementation lives in
`internal/engine`; `internal/directives` retains the recognition/state facade.
Configuration admission binds private immutable authority through the engine's
consumer-owned policy interface. Direct registry inputs use their owned resolved
settings with global permission enabled.

Callbacks receive full original context and options. A validated finding's start
anchor selects its state; crossing the interval end does not change attribution.
File-wide findings require a source span: use byte zero for default file-wide
observations and the zero-width source end for missing EOF material. Surplus terminal
material uses its smallest offending suffix after retaining the first ending. An enabled
final EOF interval requires execution or truthful missing-rule coverage even if
it has zero width. Disabled-throughout rules may skip ordinary lint execution.
Incomplete processing retains reliable earlier diagnostics and observations but
withholds unresolved intervals and ordinary callbacks.

The immutable shared `engine.DirectiveAnalysis` observation retains candidates,
intervals, exact suppression owners/tokens and all configuration restriction
origins. Accessors own their collection/position/option views. Mutating the
serialized result cannot change those observations. Lint's schema-v1 notes name
the governing permission fields and retain source coordinates; exact ancestor
paths/points remain internal inspection data. Suppressed lint records use the
declared `suppressed_by.kind` without a dangling `directive_id`. No directive
collection, returned source documents or undeclared wire fields are fabricated.
Both text and JSON reporters include these observations and count whole-record
omissions under reporting limits. The inspection package now projects these observations and optionally runs
disabled-throughout callbacks on the shared budget. Inspection/fix/MCP commands
and MD087 counterfactual conclusions remain downstream.

MD029 compares full actual ordered groups before marker-start suppression. Hidden
first items supply the start; hidden later mismatches consume direct ordinals.
A later enable directive retains the original group counter and admitted style.

MD030 inventories complete original list context before suppression. Each manual
finding uses its owned separator start, excluding marker and ancestor prefixes.
Empty marker lines remain exempt; separately enabled nested items still run.

MD032 evaluates complete source-body neighbors before chosen-start suppression.
A removed reference definition remains context in its actual owning container.

MD069 retains complete actual item context through maximal-run construction.
Suppression uses the first marker start of each full run; an inline comment
after that start cannot suppress the run retroactively.

MD076 evaluates full direct sibling context before filtering. The immutable exact
first separator baseline survives hidden items and nested visits. Chosen original
range START governs suppression: next-marker deficits differ from first-extra-line
surplus ranges, which exclude ancestor prefixes and later surplus lines.


MD077 uses final nearest-item ownership and cumulative ancestor ContentIndent
before suppression. The first item-facing quote boundary controls eligible
prose/fence/table lines; protected payload is excluded first. Filtering anchors
to the original surplus/deficit START, including shared whole-tab evidence.

MD009 inventories whole physical suffixes before START-based filtering, retaining
actual code/raw/prefix/hard-break context across disabled and selected regions.

MD010 inventories original TAB bytes and full actual literal/raw/container
ownership before START-based filtering. Each byte is independently controlled;
code_blocks never admits CodeSpan or raw/header bytes.

MD012 constructs full maximal physical blank runs before filtering the chosen
first-excess body START. Earlier hidden blanks consume the configured maximum;
protected lines break runs. A later enable cannot create a suffix finding from
an earlier anchored run. Inspection slicing retains full original context.

MD013 inventories physical width and full actual protected/URL context before chosen-scalar START filtering. Protected comment bytes can precede an eligible same-line scalar; earlier context remains available to inspection slicing.


MD027 establishes full final Paragraph, actual nested/lazy prefix and literal/raw ownership before filtering at the excess-SPACE range START. A later same-line disable cannot suppress it retroactively; slicing retains earlier context.


MD028 establishes full original sibling, gap and protected ownership before filtering each remaining-body START. A comment in the gap breaks adjacency; a later second-quote comment cannot suppress an earlier gap. Inspection slices retain outside neighbors and raw state.

MD064 establishes complete semantic word, actual raw and authored SPACE ownership
before filtering the surplus range START. Hidden context remains available to
inspection slicing; configured-off enables retain the admitted maximum.

MD085 establishes the full actual Paragraph baseline, protected-prefix ownership and actual HardBreak successor context before filtering at the surplus START. Earlier disabled content remains structural context; inspection selection never resets the baseline.

MD089 establishes complete original prose ownership and raw lifetime before
filtering each zero-width insertion START. Hidden or unselected earlier context
remains binding; line selection cannot create source adjacency or reset raw state.

MD035 establishes the global first eligible original thematic pattern, including
disabled breaks, before START suppression or inspection selection. Protected raw
lifetimes remain full-document context. Later enabling never resets the baseline.

MD037 constructs complete ordinary Text islands and resolves all competing runs
before suppression. Each interior whitespace edge uses its own original START,
independently of the opener. Inspection filtering preserves full original state.

MD038 is available and enabled by default for CommonMark and GFM, with no
rule-specific options. Shared `enabled` and `allow_directives` settings apply;
unknown options fail even when disabled. Analysis first resolves actual CodeSpan
ownership, normalized value and original paired ASCII SPACE edges over the full
source. Each excess edge range uses its own start for directive suppression;
selected inspection lines filter results after full-source analysis. All findings
remain manual, with no correction eligibility, edits or previews.

MD050 retains the earliest actual eligible Strong opening across full original
context, including hidden intervals. Finding suppression uses only that Strong's
two-byte Opening START, independently of closing state or inspect selection.

MD065 evaluates full immediate source-body context, including retained definitions,
before filtering each missing-side zero-width point at its own START. Recognized
headers are outside the root body; nested containers keep independent edges.
Before points follow actual ancestor/owner prefixes, and after points precede
physical endings on the same thematic line. Disabled neighbors remain context;
inspection selection cannot recompute adjacency or reset semantic raw lifetimes.


MD081 counts the complete original actual Paragraph before filtering its full
Content-hull START. Later inline transitions cannot split or recompute the ratio
or retroactively change that starting state. Full-range intersection selects
inspection observations after full-source analysis; hidden context and original
raw lifetimes remain binding. Inherited directive denials and last permitted
suppression ownership follow the shared processor. No corrections are generated.


MD088 establishes all original prose/raw/Link/Image ownership before shared
finding START suppression. Its default is OFF, but permitted directives may
enable it with the admitted ascii style. Link-descendant raw transitions remain
global even when labels are ineligible; images are isolated. Earlier unselected
raw context remains effective for inspect line intersections. Later inline
instructions cannot retroactively suppress an earlier scalar. This analysis
produces manual findings only; it adds no correction authority.
