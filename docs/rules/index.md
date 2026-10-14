# Rules to implement

We target the implemented rule list from `rumdl` because it covers markdownlint's built-in rules and adds further checks. The individual rule specifications define mdchk's behavior internally. We may expand this ruleset with additional custom rules in the future.

The mdchk ruleset contains 89 rules: MD001 through MD095, excluding MD002, MD006, MD008, MD015, MD016, and MD017. Preserve these reserved gaps; do not implement checks under those IDs.

Every rule is planned for implementation. Each linked page defines its behavior, configuration, examples, correction policy, and validation requirements. These are internal mdchk specifications, not claims of an implemented engine. Rule IDs are stable; additions and changes to defaults require an explicit specification update.

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

Configure rules using their MDxxx IDs. Each accepts `enabled` and `allow_directives`, both booleans. Default enablement is specified per rule; `allow_directives` defaults to true, subject to inherited restrictions. Options described on a rule page are nested beside these settings. Reject unknown options and values outside the documented domains. Descriptive aliases in older configuration examples remain preliminary and need an explicit mapping before implementation.

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

## Anchor resolution

For the `github` anchor convention, obtain visible heading text by removing inline markup while retaining text and image alternative text. Lowercase it using Unicode case mapping, remove punctuation except hyphens and underscores, and replace whitespace characters with hyphens. Resolve repeated bases in document order using the lowest available numeric suffix starting at `-1`. MD080 compares bases before suffix allocation. Explicit HTML IDs and supported format-specific heading IDs are also targets; an explicit heading ID takes precedence over its generated ID.

For the `explicit` convention, accept only explicit IDs. Decode percent escapes in fragments as UTF-8 once; compare resulting IDs case-sensitively. Invalid escapes are unresolved fragments. Do not fetch remote documents. MD051 and MD073 share this resolution logic.

## Resource-dependent checks

For local links, remove query and fragment components, decode URL path escapes once, and resolve relative to the source document directory. Ignore remote schemes, protocol-relative URLs, and data URIs. Reject traversal outside the permitted project root. Do not follow symlinks outside that root. A blocked or unavailable path is reduced coverage, while an accessible nonexistent path is a finding. Directory links resolve to a configured index filename, initially `index.md`.

Project MCP reads only within its configured root. Standalone MCP must receive a logical source path and a supplied resource map for cross-file checks. Without that context, report the affected checks as unavailable. CLI checks use the established project boundary. Navigation validation also requires a supplied or readable project configuration. No rule may perform network requests.

## Implementation contract

Use parser context and preserved source bytes. Respect the selected format, directive permissions, and the shared safe correction contract. Never guess author intent or fix uncertain syntax. Preserve line-ending conventions and unrelated bytes. Report-only rules must not produce automatic edits.

Before marking a rule implemented, verify valid and invalid cases, configuration boundaries, intentional exceptions, diagnostic ranges, and applicable formats. Fixtures must exercise interactions with protected regions and containers. Fixes require reparsing, rule-specific preservation checks, and idempotence. CLI and MCP must produce equivalent findings for equivalent source and context.

Quarto chunk checks require an explicit syntax extension; MkDocs navigation requires an explicit project context. Both remain required work beyond the confirmed CommonMark, GFM, and Pandoc format contract. Metadata key sorting requires a format-aware parser and is an explicit exception to opaque metadata handling. MD094 operates before ordinary parsing and reports one attributed file error for invalid UTF-8.

See the [engineering design](../engineering.md#rules-and-diagnostics), [Markdown formats](../markdown-formats.md), and [configuration guide](../configuration.md).
