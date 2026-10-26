# Rules to implement

We target the implemented rule list from `rumdl` because it covers markdownlint's built-in rules and adds further checks. The individual rule specifications define mdchk's behavior internally. We may expand this ruleset with additional custom rules in the future.

The mdchk ruleset contains 87 rules: MD001 through MD095, excluding MD002, MD006, MD008, MD015, MD016, MD017, MD078, and MD079. Preserve these reserved gaps; do not implement checks under those IDs.

MD047 analysis, configuration, safe previews and native application are implemented for CommonMark. Apply is limited to Darwin arm64+cgo/local APFS; other native adapters refuse writes with `13`, and protected EOF contexts remain manual. MD001 heading-increment analysis and configuration are implemented for CommonMark and GFM, including opt-in YAML/TOML title baselines. MD003 heading-style analysis and configuration are also implemented for both flavors. MD018 missing opening-hash spacing, MD019 extra opening-hash spacing, MD020 missing terminal-hash spacing, MD021 closed-heading delimiter spacing, MD022 heading blank-line analysis, MD023 heading indentation analysis, MD024 repeated heading analysis, MD025 multiple-title analysis, MD026 heading-end punctuation analysis, MD036 emphasis-as-heading analysis, MD041 opening-heading analysis, MD043 required-outline analysis and MD063 heading-capitalization analysis and configuration are implemented for both flavors. MD001, MD003, MD018, MD019, MD020, MD021, MD022, MD023, MD024, MD025, MD026, MD036, MD041, MD043 and MD063 are report-only. MD026, MD036, MD043 and MD063 are available but disabled by default. Other ordinary rule callbacks remain planned. The mandatory UTF-8 gate is implemented independently; MD094 controls attribution, not an ordinary production callback. Each linked page defines its behavior, configuration, examples, correction policy, and validation requirements. These are internal mdchk specifications; individual status text identifies implemented support. Rule IDs are stable; additions and changes to defaults require an explicit specification update.

## Areas

| Area | Rules |
| --- | --- |
| [Headings](headings/index.md) | 19 |
| [Lists](lists/index.md) | 9 |
| [Whitespace](whitespace/index.md) | 10 |
| [Formatting](formatting/index.md) | 9 |
| [Html and comments](html-and-comments/index.md) | 5 |
| [Code blocks](code-blocks/index.md) | 8 |
| [Links and images](links-and-images/index.md) | 13 |
| [Tables](tables/index.md) | 5 |
| [Footnotes](footnotes/index.md) | 3 |
| [Frontmatter](frontmatter/index.md) | 2 |
| [Content and navigation](content-and-navigation/index.md) | 3 |
| [Encoding](encoding/index.md) | 3 |

## Skipped IDs

| ID | Policy |
| --- | --- |
| MD002 | Reserved; use MD041 for the opening-heading requirement |
| MD006 | Reserved; no check assigned |
| MD008 | Reserved; no check assigned |
| MD015 | Reserved; no check assigned |
| MD016 | Reserved; no check assigned |
| MD017 | Reserved; no check assigned |

## Configuration and findings

Configure rules using their exact, case-sensitive MDxxx IDs. Each accepts `enabled` and `allow_directives`, both booleans. Default enablement is specified per rule; `allow_directives` defaults to true, subject to inherited restrictions. Options described on a rule page are nested beside these settings. Reject aliases, reserved or unknown IDs, duplicate entries, unknown options, and values outside the documented domains, including on disabled rules. An empty option mapping keeps defaults; null is invalid.

```yaml
version: 1
rules:
  MD013:
    enabled: true
    length: 100
  MD040:
    enabled: true
    allow_directives: false
```

Unless a page says otherwise, checks target the parsed Markdown body and exclude literal code, raw HTML, recognized metadata, escaped syntax, and comments. Rules that target those constructs explicitly inspect them. Container-relative spacing is measured after list and blockquote prefixes. A blank body line contains no non-whitespace body content, although container prefixes may remain.

Findings identify the numeric rule ID, a concrete message, and the smallest relevant source range. Report offending delimiters or characters for token checks, markers for indentation checks, and complete heading or reference labels for structural checks. For missing material, use a zero-width position at the insertion point. A missing opening heading is reported at the first body block or end of an empty body. Use one-based line and Unicode code-point columns with exclusive range ends; retain byte offsets internally. MD094 is the decoding exception defined on its page.

Use one violation severity initially. Default-disabled rules remain required implementation work. Their enablement is independent of availability of a format extension or resource context. Missing prerequisites produce explicit reduced-coverage information, not success or guessed violations.

Keep known-rule metadata separate from executable registration. An explicitly configured rule that is effectively enabled but unavailable fails configuration validation. Unmentioned unavailable default-enabled rules produce incomplete analysis; explicitly disabled rules and dialect-inapplicable checks do not. Preserve per-rule defaults across builds. See [capability handling](../configuration.md#initial-capability-handling) and [incomplete results](../output-formats.md#incomplete-analysis).

## Anchor resolution

Select the convention once through [`resources.anchor_style`](../configuration.md#shared-resource-settings), default `github`. MD051, MD073, and MD080 use the same policy for selected and referenced documents.

For the `github` anchor convention, obtain visible heading text by removing inline markup while retaining text and image alternative text. Lowercase it using Unicode case mapping, remove punctuation except hyphens and underscores, and replace whitespace characters with hyphens. Resolve repeated bases in document order using the lowest available numeric suffix starting at `-1`. MD080 compares bases before suffix allocation. Explicit HTML IDs and supported format-specific IDs are also targets; an explicit heading ID takes precedence over its generated ID. MD073 includes headings only; MD080 compares heading bases only, without becoming a general duplicate-ID rule.

For the `explicit` convention, accept only explicit IDs. Decode percent escapes in fragments as UTF-8 once; compare resulting IDs case-sensitively. Invalid escapes are unresolved fragments. Do not fetch remote documents. An empty fragment refers to the document itself. MD051, MD073, and MD080 share the anchor policy.

## Resource-dependent checks

Follow the [resource contract](../engineering/resources.md). Begin with parser-semantic destinations, split raw query/fragment before one path/fragment decode, retain literal plus signs, reject encoded separators and root escapes, and resolve relative to the source directory. Remote and non-file schemes are ignored without fetching. Absolute/file/drive/UNC paths, Git metadata, and every resource symlink component are blocked. Directory links try shared `resources.index_file` once, default `index.md`, without fallback or recursion.

Resource roots are independent of lint selection and config discovery. Excluded or unselected resources may supply read-only context without becoming targets. Standalone resources are seeded by submitted files and optionally supplemented by a partial or complete logical inventory; an omitted entry is unknown unless completeness or an explicit absence establishes otherwise. Never fall back to host files.

MD057 owns established missing targets; MD051 becomes inapplicable for a confirmed missing document and does not duplicate the finding. Unknown existence, unavailable content, blocked access, and uncertain parsing produce affected coverage instead. Existing files can pass existence checks without supplying content. Actual read/decode failures also produce attributed operational errors. Apply one operation flavor and anchor policy, without nested config discovery or recursively linting referenced documents. Navigation validation separately requires explicit integration context.

Fix checks use coordinated candidate and actual saved views under the [snapshot contract](../engineering/resources.md#snapshots-caching-fixes-and-limits). Resource lookup itself never creates a write target.

## Implementation contract

Use parser context and preserved source bytes. Respect the selected format, directive permissions, and the shared safe correction contract. Never guess author intent or fix uncertain syntax. Preserve line-ending conventions and unrelated bytes. Report-only rules must not produce automatic edits.

Before marking a rule implemented, verify valid and invalid cases, configuration boundaries, intentional exceptions, diagnostic ranges, and applicable formats. Fixtures must exercise interactions with protected regions and containers. Fixes require reparsing, rule-specific preservation checks, and idempotence. Pass 1 rule completion requires reusable engine analysis fixtures; it does not wait for MCP transports or later correction passes. CLI/project/standalone parity for equivalent source and context is verified in #196, #200 and #206 after the adapters exist. See the [delivery stages](../engineering/contracts.md#delivery-stages-and-prerequisites).

MkDocs navigation requires an explicit project context. This remains required work beyond the confirmed CommonMark and GFM format contracts. Metadata key sorting requires a format-aware parser and is an explicit exception to opaque metadata handling. UTF-8 validation is unconditional before parsing; MD094 controls attribution only. Invalid bytes produce one I/O error, never a duplicate rule violation. See the [UTF-8 contract](../engineering/rule-behavior.md#utf-8-validation).

See the [engineering design](../engineering.md#rules-and-diagnostics), [Markdown formats](../markdown-formats.md), and [configuration guide](../configuration.md).
