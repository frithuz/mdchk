# Supported Markdown formats

mdchk's confirmed format scope is CommonMark and GitHub Flavored Markdown (GFM), with independently optional frontmatter support. The internal CommonMark 0.31.2 core is delivered and verified: all **652/652** unchanged specification examples pass normalized and strict comparisons without exemptions. Direct upstream runs in both modes report 652 passed and zero failed, errored or skipped. CLI lint/fix/inspect integrate this core and directive suppression, and MD047 is the first production ordinary rule and correction. GFM and independently selected opaque YAML/TOML frontmatter integrate this pipeline. Source-mapped YAML/TOML metadata decoding, MD001 heading-increment, MD003 heading-style, MD018 missing opening-hash spacing, MD019 extra opening-hash spacing, MD020 missing terminal-hash spacing, MD021 closed-heading delimiter spacing, MD022 heading blank-line analysis, MD023 heading indentation analysis, MD024 repeated heading analysis, MD025 multiple-title analysis, MD026 heading-end punctuation analysis, MD036 emphasis-as-heading analysis, MD041 opening-heading analysis, MD043 required-outline analysis, MD063 heading-capitalization analysis, MD080 heading-anchor collision analysis, MD082 empty-section analysis, MD090 thematic-break-before-heading analysis and MD093 inline-formatting-in-headings analysis are implemented. Other ordinary callbacks, MCP integration and production rendering remain separate deliveries.

The current Go `Parse` API has no flavor or configuration option. CLI `flavor: commonmark` selection is delivered; it does not add a switch to that Go API. GFM is available, including selection by omission. Enabled YAML/TOML frontmatter is recognized and protected in shared lint/fix/inspect; MD001, MD025 and MD041 can consume a semantic title when their `frontmatter_title` options is enabled. CommonMark alone still leaves missing default-rule coverage (`15`); use the [current reduced setup](index.md#get-started-with-this-build) for a complete MD047-only run. See the [delivered core contract](engineering.md#delivered-commonmark-core) for APIs, source guarantees, UTF-8/resource outcomes and [reproduction commands](engineering.md#reproduce-the-core-delivery).

GFM uses CommonMark 0.31.2 and all five pinned extensions: 2,117 approved
comparison units pass, including every published case. Default and explicit GFM
selection share the real lint, fix and inspection pipeline, including source
facts, directives, protected regions and candidate reparsing. MD047 is the first
production correction; MD001, MD003, MD018, MD019, MD020, MD021, MD022, MD023, MD024, MD025, MD026, MD036, MD041, MD043, MD063, MD080, MD082, MD090 and MD093 analysis are also available. MD026, MD036, MD043, MD063, MD080, MD082, MD090 and MD093 are available but disabled by default. Other named rules remain unavailable. See the [parser verification record](engineering/gfm-verification.md).

## CommonMark and the GFM foundation

[CommonMark 0.31.2](https://spec.commonmark.org/0.31.2/) defines the base Markdown syntax and interpretation. CommonMark defines headings, paragraphs, lists, block quotes, code, links, images, emphasis, HTML, and other standard constructs.

Select it with `flavor: commonmark` when the document should follow CommonMark without GFM extensions:

```yaml
version: 1
flavor: commonmark
```

Omitted `flavor` selects `gfm`, preserving the existing configuration default.

A build that cannot parse the selected dialect must reject it as unavailable before analysis, including when GFM was selected by omission. It must not silently fall back to CommonMark. CommonMark lint supports directive processing with inherited permissions; see [initial capability handling](configuration.md#initial-capability-handling).

## GitHub Flavored Markdown

Select GFM with `flavor: gfm`:

```yaml
version: 1
flavor: gfm
```

mdchk's approved GFM compatibility profile combines CommonMark 0.31.2 with all five extensions from a pinned snapshot of the [published GFM specification](https://github.github.io/gfm/): tables, task list items, strikethrough, extended autolinks, and disallowed raw HTML handling. Strikethrough includes single and double tildes. Extended autolinks include the specified email, `mailto:`, and `xmpp:` forms as well as web links. The extensions are selected together by `flavor: gfm`; no individual extension switches are defined.

The published specification is labeled `0.29-gfm`. mdchk deliberately uses the newer CommonMark base, including its HTML recognition and character-class behavior. This profile is therefore not a claim of exact legacy `0.29-gfm` conformance. The [GFM compatibility contract](engineering/gfm-compatibility.md) records the source hashes, base-version differences, extension semantics, and verification requirements. Specification updates require an explicit compatibility review.

In CommonMark mode, the same source follows CommonMark 0.31.2 interpretation without GFM extensions. The GFM profile does not add footnotes, GitHub alerts, emoji expansion, math, or automatic frontmatter recognition. Syntax outside the profile follows ordinary interpretation. GitHub website rendering, post-processing, and sanitization are outside this contract; disallowed raw HTML handling does not make mdchk an HTML sanitizer.

## Optional frontmatter headers

The internal `ParseWithOptions` API recognizes opaque YAML/TOML headers before
body parsing, retaining exact source ranges and an explicit incomplete outcome
for unterminated headers. CLI lint, fix and inspection share the same effective
recognition settings, header protection and source-backed inspection records.
MD001, MD025 and MD041 decode metadata values on demand when their `frontmatter_title` options is enabled.

Frontmatter is disabled by default, independently of `commonmark` or `gfm`. Set `frontmatter.enabled: true`; `frontmatter.formats` defaults to
`[yaml, toml]`. JSON headers are outside the initial contract.

The opener is on the first line after an optional preserved UTF-8 BOM, with no
preceding blank or comment. YAML uses `---` to open and `---` or `...` to close;
TOML uses `+++` for both. Delimiter-only lines are unindented and may have trailing
spaces/tabs. Empty headers are valid. An unterminated header remains protected
through EOF, with affected incomplete coverage and no edits in that region.

Ordinary Markdown rules and directives cannot interpret recognized header content.
Metadata-aware checks decode on demand under the [metadata contract](engineering/frontmatter.md).
With recognition disabled, source follows ordinary flavor parsing. Selecting YAML
configuration does not enable headers. Explicitly enabled but unavailable
recognition is a configuration error, never a silent fallback.

## Consistent interpretation

Lint, fix, inspect, and both MCP modes must use the same selected flavor and frontmatter option. Format selection controls parsing; rule settings separately control which checks run.

Support claims must describe verified syntax and known gaps. Format conformance must be backed by specification examples. Uncertain parsing reduces rule coverage and prevents automatic fixes in affected regions. Lint/fix report that deficit as `incomplete` with exit 15 unless an operational error takes precedence. Syntax intentionally outside a selected dialect follows ordinary interpretation and does not by itself create missing coverage. See the [engineering design](engineering.md#markdown-support-and-parsing) for the parser and correction contract.

## Removed format

Pandoc support has been canceled. `flavor: pandoc` is rejected as an invalid flavor; select `commonmark` or `gfm`.
