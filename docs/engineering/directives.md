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

Use the selected flavor's source spans. In Pandoc, protect math, raw syntax, attributes, citation control syntax, and literal HTML interiors; parse permitted Markdown children of native HTML containers normally. Keep HTML origin metadata even when the semantic node is a native Div or Span. A resource's directives never alter its referring source, resource access, shared anchor/index policy, or execution budgets.

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
observations and the zero-width source end for EOF-specific findings. An enabled
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
omissions under reporting limits. Inspection/fix/MCP commands, observational
runs of disabled-throughout rules and MD087 counterfactuals remain downstream.
