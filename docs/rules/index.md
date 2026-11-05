# Rules to implement

[MD057 missing local link target analysis](links-and-images/md057.md) is
available for CommonMark and GFM, enabled by default with `check_images: true`.
Local Link and Image destinations whose target authoritatively does not exist,
or exists with the wrong type, produce report-only findings through the shared
read-only resource service in `lint`, `inspect` and `fix`.

[MD075 pipe rows without a table header analysis](tables/md075.md) is
available for GFM only, enabled by default with no rule-specific options. Runs
of pipe rows in paragraphs that did not become a table produce report-only
findings.

[MD060 table column layout analysis](tables/md060.md) is available for GFM
only, disabled by default with `style: aligned`. When enabled, parsed
pipe-table cells whose padding does not match the aligned, compact or tight
layout produce report-only findings.

[MD056 mismatched cell count analysis](tables/md056.md) is available for GFM
only, enabled by default with no rule-specific options. Parsed pipe-table body
rows whose original source cell count differs from their table's width produce
report-only findings.

[MD058 blank lines around tables](tables/md058.md) is available for GFM only,
enabled by default with no rule-specific options. A parsed pipe table without a
blank body line directly before or after it, away from document and container
edges, produces report-only findings.

[MD055 outer table pipe analysis](tables/md055.md) is available for GFM only,
enabled by default with `style: consistent`. Parsed pipe-table rows whose
leading or trailing pipe differs from the configured convention, or in
consistent mode from their table's header row, produce report-only findings.

[MD059 uninformative link label analysis](links-and-images/md059.md) is
available for CommonMark and GFM, enabled by default with the `phrases` option.
Links whose normalized visible label equals a configured phrase, compared with
Unicode simple case folding, produce report-only findings.

[MD053 unneeded-definition analysis](links-and-images/md053.md) is available
for CommonMark and GFM, enabled by default with `ignored_labels: []`.
Definitions no parsed reference uses, and later definitions shadowed by an
earlier one with the same label, produce report-only findings.

[MD054 link and image representation style analysis](links-and-images/md054.md)
is available for CommonMark and GFM, disabled by default. When enabled, each
link and image whose style is not in `allowed_styles` produces a report-only
finding. Angle and bare GFM autolinks both use the `autolink` style.

[MD062 whitespace-in-destination analysis](links-and-images/md062.md) is
available for CommonMark and GFM, enabled by default with no rule-specific
options. Flat unparsed inline link and image candidates whose destination holds
literal space or tab characters produce report-only findings.

[MD052 references-without-definitions analysis](links-and-images/md052.md) is
available for CommonMark and GFM, enabled by default with no rule-specific
options. Explicit full and collapsed link and image references whose label
misses the completed definition collection produce report-only findings.

[MD051 unresolved fragment analysis](links-and-images/md051.md) is available
for CommonMark and GFM, enabled by default with `cross_file: false`. Same-document
fragments are checked against generated heading IDs and HTML IDs; distinct-document
fragments resolve through the shared resource service in `lint`, `inspect` and
`fix` when `cross_file` is true (and for normalized references back to the current
source regardless). Findings remain manual.

[MD045 image alternative text analysis](links-and-images/md045.md) is available
for CommonMark and GFM, enabled by default with `allow_decorative: false`.
Findings remain manual; ambiguous HTML retains located incomplete coverage.

[MD042 links without destinations analysis](links-and-images/md042.md) is available
for CommonMark and GFM, enabled by default with no rule-specific options. Actual
inline Links with an empty or lone-fragment semantic destination produce manual
findings at original destination points or token ranges; Images are excluded.

[MD039 link-label-whitespace analysis](links-and-images/md039.md) is available
for CommonMark and GFM, enabled by default with no rule-specific options. Only
raw visible label edges on actual inline/resolved reference Links are checked;
findings remain permanently manual with NoFix metadata.

[MD034 bare URL analysis](links-and-images/md034.md) is available for CommonMark
and GFM, enabled by default with no rule-specific options. Findings are manual.

[MD011 swapped link delimiter analysis](links-and-images/md011.md) is available
for CommonMark and GFM, enabled by default, with manual findings.

[MD070 nested Markdown example analysis](code-blocks/md070.md) is available for
CommonMark and GFM, disabled by default with no rule-specific options. Explicit
marker extents bound actual payload fence recognition under the original budget.
Findings retain original ranges and remain manual; correction is future work.

[MD048 fence delimiter analysis](code-blocks/md048.md) is available for CommonMark
and GFM with manual opening-marker findings.

[MD046 code-block representation analysis](code-blocks/md046.md) is available for
CommonMark and GFM, enabled by default with `style: consistent`. The first eligible
original block establishes a document-wide style before suppression and focus.
Mismatches use opening-fence or intrinsic-indentation markers, retaining atomic
tabs. Unknown necessary context withholds the inventory. Findings remain manual;
conditional conversion is future work.

[MD040 fenced-language analysis](code-blocks/md040.md) is available for CommonMark
and GFM, disabled by default and permanently report-only. The exact
`allowed_languages` string list admits nonempty literal labels; an absent or
leading-ASCII-brace first token is missing language. Findings use original
opening-end points or token source ranges, and necessary unknown context
withholds the complete inventory. No attribute grammar or correction is added.

[MD031 fenced-block blank-line analysis](code-blocks/md031.md) is available for
CommonMark and GFM, enabled by default with no rule-specific options. Actual
owner boundaries and immediate blank body lines determine independent before/after
findings. Missing blanks have zero-width original points; findings remain manual,
and unknown necessary context withholds the complete inventory.

[MD014 shell-prompt analysis](code-blocks/md014.md) is available for CommonMark
and GFM, enabled by default with the existing `languages` string-list option.
Actual selected fenced payload is classified as a whole before suppression and
inspection focus. Unknown necessary context withholds the complete inventory.
Findings are manual; `PlannedFix` remains future metadata. MD047 alone supplies
current corrections, and MCP parity remains downstream.

[MD092 conflict-marker analysis](html-and-comments/md092.md) is available for
CommonMark and GFM, enabled by default, with no rule-specific options and
permanent report-only support. Maximal original column-one ASCII runs use one
document-wide Boolean state; full-source analysis precedes directive suppression
and inspection focus. Actual marker-byte protection and original owned HTML raw
lifetimes remain authoritative. Unknown required protection withholds the entire
inventory with reduced coverage. MD047 remains the sole current correction
producer; MD092 emits no edits or previews. MCP parity remains downstream.

[MD087 unused-suppression analysis](html-and-comments/md087.md) is available for CommonMark and GFM,
disabled by default, with no rule-specific options and permanent report-only support.
Each allowed disable target is judged using its actual owned interval and shared
original-source observations. Unknown observations retain incomplete coverage.

[MD086 unmatched-comment analysis](html-and-comments/md086.md) is available for CommonMark and GFM,
enabled by default with no rule-specific options and permanently report-only.
Actual eligible HTML owners and their tails are analyzed once; matching ends at
the owner/container boundary. Each unmatched opener reports only its original
four bytes. Code, metadata, whole Images, raw payload and opaque HTML tokens stay
protected; ordinary unowned Text is excluded. Unavailable owned named-token
context withholds the complete MD086 inventory with explicit coverage. MD047
remains the sole current correction producer; MCP parity remains downstream.

[MD033 embedded-HTML analysis](../rules/html-and-comments/md033.md) is available for CommonMark and GFM,
disabled by default and permanently report-only. `allowed_tags: []` accepts a
string list and matches whole names with ASCII case folding, without trimming.
Each disallowed complete named tag has its own full original range. Opaque HTML
tokens and actual raw payload stay protected; unavailable owned-tag context
withholds the entire MD033 inventory with explicit coverage. No MD033 edits or
previews are produced; MD047 remains the sole current fixer.


We target the implemented rule list from `rumdl` because it covers markdownlint's built-in rules and adds further checks. The individual rule specifications define mdchk's behavior internally. We may expand this ruleset with additional custom rules in the future.

The mdchk ruleset contains 87 rules: MD001 through MD095, excluding MD002, MD006, MD008, MD015, MD016, MD017, MD078, and MD079. Preserve these reserved gaps; do not implement checks under those IDs.

MD047 analysis, configuration, safe previews and native application are implemented for
CommonMark. Apply works on Linux, macOS and Windows; other operating systems
refuse writes with `13`, and protected EOF contexts remain manual. MD001
heading-increment analysis and configuration are implemented for CommonMark and GFM,
including opt-in YAML/TOML title baselines. MD003 heading-style analysis and
configuration are also implemented for both flavors. MD004 bullet-marker, MD005
sibling-list alignment, MD007 bullet nesting indentation, MD029 numbered-list prefix and
MD030 list-marker separator, MD032 list-boundary blank-line, MD069 repeated-list-marker
and MD076 list-item spacing, MD077 list-continuation indentation, MD009 trailing-whitespace, MD010 tab-character, MD012 blank-body-run and MD013 maximum-line-width and MD014 shell-prompt and MD031 fenced-block blank-line and MD040 fenced-language and MD046 code-block representation and MD048 fence-delimiter and MD011 swapped-link-delimiter and MD034 bare-URL and MD039 link-label-whitespace and MD042 links-without-destinations and MD051 unresolved-fragment and MD052 references-without-definitions and MD053 unneeded-definition and MD062 whitespace-in-destination and MD054 link-and-image-representation-style and MD057 missing-local-target and MD059 uninformative-link-label and MD045 image-alternative-text and MD070 nested-example and MD027 blockquote-separator analysis, MD028 blockquote-gap analysis, MD064 repeated-prose-space and MD085 paragraph-continuation, MD089 CJK/Latin-spacing, MD035 horizontal-rule-style and MD037 emphasis-whitespace and MD038 inline-code-whitespace and MD044 configured-name-capitalization and MD061 configured-prohibited-vocabulary and MD083 encoding-mismatch-sequence and MD049 italic-delimiter-style and MD050 bold-delimiter-style and MD065 horizontal-rule-blank-line and MD081 paragraph-emphasis analysis and MD088 ASCII-punctuation analysis and MD033 embedded-HTML analysis and MD086 unmatched-comment analysis and
configuration are implemented for both flavors. MD018 missing opening-hash spacing,
MD019 extra opening-hash spacing, MD020 missing terminal-hash spacing, MD021
closed-heading delimiter spacing, MD022 heading blank-line analysis, MD023 heading
indentation analysis, MD024 repeated heading analysis, MD025 multiple-title analysis,
MD026 heading-end punctuation analysis, MD036 emphasis-as-heading analysis, MD041
opening-heading analysis, MD043 required-outline analysis, MD063 heading-capitalization
analysis, MD080 heading-anchor collision analysis, MD082 empty-section analysis, MD090
thematic-break-before-heading analysis, MD093 inline-formatting-in-headings analysis and
MD094 invalid-UTF-8 analysis and configuration are implemented for both flavors. MD055 outer-table-pipe analysis,
MD056 mismatched-cell-count analysis, MD058 blank-lines-around-tables analysis, MD075 pipe-rows-without-header analysis and their configuration are implemented for GFM only, enabled by default. MD060 table-column-layout analysis and configuration are implemented for GFM only, disabled by default. MD001, MD003, MD004, MD005, MD007,
MD029, MD030, MD032, MD069, MD076, MD077, MD009, MD010, MD012, MD013, MD014, MD031, MD040, MD046, MD048, MD011, MD034, MD039, MD042, MD045, MD051, MD052, MD053, MD062, MD054, MD057, MD059, MD055, MD056, MD058, MD060, MD075, MD070, MD027, MD028, MD064, MD085, MD089, MD035, MD037, MD038, MD044, MD061, MD083, MD049, MD050, MD065, MD081, MD088, MD033, MD086, MD087, MD018, MD019, MD020, MD021, MD022, MD023, MD024, MD025, MD026,
MD036, MD041, MD043, MD063, MD080, MD082, MD090, MD093 and MD094 are report-only. MD013, MD026, MD036,
MD040, MD043, MD044, MD061, MD083, MD063, MD070, MD080, MD082, MD085, MD089, MD090, MD081, MD088, MD033, MD054, MD060, MD087 and MD093 are available but disabled by default. Other
ordinary rule callbacks remain planned. The mandatory UTF-8 gate is implemented
independently; MD094 is enabled by default and controls attribution, not an ordinary production callback. Each
linked page defines its behavior, configuration, examples, correction policy, and
validation requirements. These are internal mdchk specifications; individual status text
identifies implemented support. Rule IDs are stable; additions and changes to defaults
require an explicit specification update.

[MD088 ASCII-punctuation analysis](formatting/md088.md) is available for CommonMark and GFM,
disabled by default with sole string option `style: ascii`. Invalid styles and
unknown `replacements` reject even when disabled. Findings are manual in Pass 1;
PlannedFix retains future correction scope. No MD088 edits or previews are emitted.

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

MD009 uses whole physical trailing ASCII-space/TAB suffixes as a documented
exception to container-relative spacing; actual prefixes determine blankness.
