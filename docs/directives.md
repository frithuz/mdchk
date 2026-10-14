# Markdown rule directives

Use inline HTML comments to enable or disable rules from a point in a Markdown file onward. Directives affect lint, fix, inspect, and MCP operations consistently. Configuration controls whether directives are allowed at all and which rules they may change.

Directives are intended for exceptions to specific rules within a document. To exclude a whole file from all rule linting, add its path to `exclude` in configuration. Do not enumerate every rule in a directive to suppress linting across the file. A whole-file exclusion does not prevent an explicitly requested inspection.

## Disable and enable rules

```markdown
<!-- mdchk:disable heading-increment -->

#### An intentionally skipped heading level

<!-- mdchk:enable heading-increment -->
```

The disable comment turns off `heading-increment` after the comment's closing `-->`. The enable comment turns it back on after its own closing marker. Each change continues to the end of the file or until another allowed directive changes the same rule.

Enable means explicitly enable the rule, including a rule disabled in configuration. It does not mean restore the configured default. A rule's configured options still apply when a directive enables it.

Name one or more rules, separated by whitespace:

```markdown
<!-- mdchk:disable heading-increment fenced-code-language -->
<!-- mdchk:enable heading-increment -->
```

Here, the second comment enables only `heading-increment`; `fenced-code-language` remains disabled. Rule names and the `mdchk:disable` and `mdchk:enable` commands are case-sensitive. At least one rule name is required. There is no implicit all-rules form, wildcard, or comma-separated list.

Directives can be standalone comments or appear between other content on a line. Content before the closing comment marker retains its previous rule state; content after it uses the new state. Recognized comments may span lines, with whitespace separating rule names.

## Understand scope

Rules start each file with the state selected by configuration. Process directives in source order: the last allowed directive for a rule determines its state at each later location. Repeating an enable or disable is harmless. Directives do not form a stack and do not automatically end at a heading, paragraph, code block, or directory boundary. State never carries into another file.

Only actual Markdown HTML comments act as directives. Text inside code blocks, code spans, front matter, or other literal content does not change rule state. If parsing cannot establish that a comment is a directive, mdchk leaves it inactive and reports the uncertainty.

A finding is controlled by the rule state at its starting location. File-wide findings use the rule's documented source location, such as end-of-file for a missing final newline. Disabling a rule later in the file does not hide an earlier finding.

Fix does not apply corrections for a rule disabled at the finding's location. It also leaves an edit alone if any part of the edit would change a region where that rule is disabled. Directives change rule behavior, not parsing, file selection, or filesystem permissions.

## Control directives in configuration

Directives are allowed by default. To ignore all directive requests:

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
  heading-increment:
    enabled: true
    allow_directives: false
  fenced-code-language:
    enabled: false
    allow_directives: true
```

Ancestor directive restrictions remain binding when a descendant configuration is selected. Effective global and per-rule permissions are the logical AND of applicable ancestor and selected values. An omitted value or child `true` cannot undo an ancestor `false`; a child may add restrictions. These permissions protect rule state from comments, but do not lock a child configuration's rule enablement or options. See [inherited directive restrictions](configuration.md#enforce-directive-restrictions-in-descendant-configurations) for examples and discovery boundaries.

`allow_directives` defaults to `true`. In this example, comments cannot disable or enable `heading-increment`, but can enable the configured-off `fenced-code-language` rule. Global `directives.enabled: false` takes precedence over per-rule permissions.

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
