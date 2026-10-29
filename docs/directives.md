# Markdown rule directives

Use inline HTML comments with canonical MDxxx rule IDs to enable or disable rules from a point in a Markdown file onward. Directives affect lint, fix, inspect, and MCP operations consistently. Configuration controls whether directives are allowed at all and which rules they may change.

Directives are intended for exceptions to specific rules within a document. To exclude a whole file from all rule linting, add its path to `exclude` in configuration. Do not enumerate every rule in a directive to suppress linting across the file. A whole-file exclusion does not prevent an explicitly requested inspection.

Current availability: CommonMark/GFM directive processing is delivered in lint,
fix and inspect. MD001, MD003, MD004, MD005, MD007, MD029, MD030, MD032, MD069, MD076, MD077, MD009, MD010, MD012, MD013, MD027, MD028, MD064, MD085, MD089, MD035, MD037, MD018, MD019, MD020, MD021, MD022, MD023, MD024, MD025, MD026, MD036, MD041, MD043, MD063, MD080, MD082, MD090, MD093 and MD047 are production checks; the MD040 examples below
illustrate rule-state syntax for an unavailable check. Permitted directives that
enable unavailable rules produce incomplete coverage. Independently recognized
YAML/TOML headers remain protected from directives, including when MD001, MD025 or MD041 consumes
a title. Resource consumers, MCP and MD087 counterfactual checks remain planned;
their semantics below describe the accepted future contract.

## Disable and enable rules

```markdown
<!-- mdchk:disable MD001 -->

#### An intentionally skipped heading level

<!-- mdchk:enable MD001 -->
```

The disable comment turns off `MD001` after the comment's closing `-->`. The enable comment turns it back on after its own closing marker. Each change continues to the end of the file or until another allowed directive changes the same rule.

Enable means explicitly enable the rule, including a rule disabled in configuration. It does not mean restore the configured default. A rule's configured options still apply when a directive enables it.

Name one or more rules, separated by whitespace:

```markdown
<!-- mdchk:disable MD001 MD040 -->
<!-- mdchk:enable MD001 -->
```

Here, the second comment enables only `MD001`; `MD040` remains disabled. Rule IDs and the `mdchk:disable` and `mdchk:enable` commands are case-sensitive. Descriptive aliases and lowercase IDs are invalid. At least one rule name is required. There is no implicit all-rules form, wildcard, or comma-separated list.

Directives can be standalone comments or appear between other content on a line. Content before the closing comment marker retains its previous rule state; content after it uses the new state. Recognized comments may span lines, with whitespace separating rule names.

## Understand scope

Rules start each file with the state selected by configuration. Process directives in source order: the last allowed directive for a rule determines its state at each later location. Repeating an enable or disable is harmless. Directives do not form a stack and do not automatically end at a heading, paragraph, code block, or directory boundary. State never carries into another file.

Only actual Markdown HTML comments in the selected flavor act as directives. Resource-document directives never change the source document, resource root, anchor/index policy, or operation limits.

Text inside code blocks, code spans, front matter, or other literal content does not change rule state. If parsing cannot establish that a comment is a directive, mdchk leaves it inactive and reports the uncertainty.

A finding is controlled by the rule state at its starting location. File-wide findings use the rule's documented source location, such as end-of-file for a missing final newline. Disabling a rule later in the file does not hide an earlier finding.

Fix does not apply corrections for a rule disabled at the finding's location. It also leaves an edit alone if any part of the edit would change a region where that rule is disabled. Directives change rule behavior, not parsing, file selection, or filesystem permissions.

## Control directives in configuration

Directives are allowed by default in the shared CommonMark lint engine. Their permissions are captured during configuration admission. To ignore all directive requests:

```yaml
version: 1
directives:
  enabled: false
```

The configured rule states then apply throughout each file. Both enable and disable comments are ignored.

To protect specific rules while allowing directives for others:

```yaml
version: 1
directives:
  enabled: true
rules:
  MD001:
    enabled: true
    allow_directives: false
  MD040:
    enabled: false
    allow_directives: true
```

Ancestor directive restrictions remain binding when a descendant configuration is selected. Effective global and per-rule permissions are the logical AND of applicable ancestor and selected values. An omitted value or child `true` cannot undo an ancestor `false`; a child may add restrictions. These permissions protect rule state from comments, but do not lock a child configuration's rule enablement or options. See [inherited directive restrictions](configuration.md#enforce-directive-restrictions-in-descendant-configurations) for examples and discovery boundaries.

`allow_directives` defaults to `true`. In this example, comments cannot disable or enable `MD001`, but can enable the configured-off `MD040` rule. Global `directives.enabled: false` takes precedence over per-rule permissions.

If one directive names several rules, protected rules keep their configured state while permitted rules change. The report identifies each blocked request and the configuration setting responsible. File content cannot grant itself directive permission.

See [configuration](configuration.md) for other settings. [`config validate`](commands/config/validate.md) checks directive permissions in configuration definitions; it does not scan Markdown comments.

## Diagnose directive problems

With directives enabled, unknown commands, unknown rule names, missing rule names, and malformed directive syntax are reported as directive findings. An invalid directive makes no changes, including to otherwise valid rule names in the same comment. Directive findings cannot themselves be suppressed by directives.

A valid directive blocked by configuration is ignored and explained in processing notes; it is not a rule violation. When all directives are disabled, directive-like comments are inactive and do not generate syntax findings. Inspect still shows their raw text and why they have no effect.

Use inspection to explain the rule state in a region:

```sh
mdchk inspect docs/guide.md --lines 20-40
```

Inspect includes directives in that region and earlier directives still affecting it. It lists exact text, locations, resolved rule names, allowed or blocked effects, and effective ranges. When a rule is disabled, its entry identifies the directive responsible. Comments inside literal content are distinguished from effective directives.

## Special rule effects

MD074 is project-scoped and MD094 controls pre-parse UTF-8 error attribution; directives targeting either have an inapplicable effect. A valid mixed-rule comment still applies its other permitted effects. UTF-8 validation cannot be disabled.

Once implemented, MD087 observes otherwise-enabled target rules without disable intervals, on the original source and shared budgets. Last permitted disable instructions own intervals, including repeated disables. A proved hidden finding establishes use; only complete absence establishes an unused suppression. Missing context, unavailable rules or interruption withhold that conclusion and report incomplete coverage. See the [counterfactual contract](engineering/rule-behavior.md#unused-suppressions).

MD032 establishes source adjacency and blank ownership before suppression.
Missing before points use the first list marker; missing after points use the
following non-list block body start. Hidden neighbors remain context.

MD069 computes complete maximal repeated-marker runs before filtering their
first-marker diagnostic starts. Hidden ancestors remain structural context and
cannot create an invented inner suffix finding.

MD076 establishes the exact first separator baseline from all actual siblings
before filtering, even when hidden. Each actual nested list is independent.
Deficits anchor at the next marker; surplus anchors on only the first extra blank
body line, so a following item's enable cannot enable an earlier surplus.


MD077 establishes final nearest-item ownership, cumulative item baselines and
protected-line eligibility before START-based filtering. Hidden or misaligned
parent markers retain their parsed structural baseline. Whole-tab surplus ranges
may share a required prefix byte; a later inline enable is not retroactive.

MD009 classifies complete original suffix ownership before suppression. The full
suffix START controls state, so an inline comment ending before that suffix can
change its state. Later comments cannot retroactively suppress earlier suffixes.

MD010 establishes actual code, literal, raw and metadata protection over the
complete original document before filtering each one-byte TAB finding at its
START. A preceding inline control can change it; a later control is not retroactive.
Inspection slicing retains preceding controls and full structural context.

MD012 establishes maximal original physical blank runs before filtering the
first-excess body range START. Hidden earlier blanks consume the maximum; later
enablement cannot invent another finding from the same run. Protected lines break
runs, and inspection slicing retains full original context.

MD013 computes original physical width and full literal/raw/category/URL context before chosen-scalar START filtering. A protected directive comment may precede the first eligible scalar on the same line. A later disable cannot retroactively suppress an earlier scalar. Configured-off enables retain admitted options; inherited denials remain binding.

MD064 establishes complete semantic word, actual raw and authored SPACE ownership
before filtering the surplus range START. Hidden context remains available to
inspection slicing; configured-off enables retain the admitted maximum.

MD085 retains its fixed first actual Paragraph content baseline across disabled intervals, actual hard breaks and later enables. Chosen surplus SPACE/TAB START controls suppression, including a whole TAB crossing the baseline; a later same-line comment cannot suppress it retroactively. Whole container owners and actual protected prefixes remain excluded before filtering.

MD089 establishes complete original prose ownership and raw lifetime before
filtering each zero-width insertion START. Hidden or unselected earlier context
remains binding; line selection cannot create source adjacency or reset raw state.

MD035 establishes the global first eligible original thematic pattern, including
disabled breaks, before START suppression or inspection selection. Protected raw
lifetimes remain full-document context. Later enabling never resets the baseline.

MD037 resolves complete ordinary Text islands and competing original runs before
suppression or inspection selection. Each inside-whitespace edge is independently
anchored at its original START; enabling later does not reset source ownership.
