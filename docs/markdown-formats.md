# Supported Markdown formats

mdchk's confirmed format scope is CommonMark, GitHub Flavored Markdown (GFM), and Pandoc Markdown, with independently optional frontmatter support. This documents the intended support contract; parser implementation and conformance verification are still pending.

## CommonMark and the GFM foundation

[CommonMark 0.31.2](https://spec.commonmark.org/0.31.2/) defines the base Markdown syntax and interpretation. CommonMark defines headings, paragraphs, lists, block quotes, code, links, images, emphasis, HTML, and other standard constructs.

Select it with `flavor: commonmark` when the document should follow CommonMark without GFM extensions:

```yaml
version: 1
flavor: commonmark
```

Omitted `flavor` selects `gfm`, preserving the existing configuration default.

A build that cannot parse the selected dialect must reject it as unavailable before analysis, including when GFM was selected by omission. It must not silently fall back to CommonMark. Before directive processing is available, development runs also need effective `directives.enabled: false`; see [initial capability handling](configuration.md#initial-capability-handling).

## GitHub Flavored Markdown

Select GFM with `flavor: gfm`:

```yaml
version: 1
flavor: gfm
```

mdchk's approved GFM compatibility profile combines CommonMark 0.31.2 with all five extensions from a pinned snapshot of the [published GFM specification](https://github.github.io/gfm/): tables, task list items, strikethrough, extended autolinks, and disallowed raw HTML handling. Strikethrough includes single and double tildes. Extended autolinks include the specified email, `mailto:`, and `xmpp:` forms as well as web links. The extensions are selected together by `flavor: gfm`; no individual extension switches are defined.

The published specification is labeled `0.29-gfm`. mdchk deliberately uses the newer CommonMark base, including its HTML recognition and character-class behavior. This profile is therefore not a claim of exact legacy `0.29-gfm` conformance. The [GFM compatibility contract](engineering/gfm-compatibility.md) records the source hashes, base-version differences, extension semantics, and verification requirements. Specification updates require an explicit compatibility review.

In CommonMark mode, the same source follows CommonMark 0.31.2 interpretation without GFM extensions. The GFM profile does not add footnotes, GitHub alerts, emoji expansion, math, or automatic frontmatter recognition. Syntax outside the profile follows ordinary interpretation. GitHub website rendering, post-processing, and sanitization are outside this contract; disallowed raw HTML handling does not make mdchk an HTML sanitizer.

## Pandoc Markdown

Pandoc Markdown is also approved for support. Its reference is the [Pandoc User’s Guide](https://pandoc.org/MANUAL.html#pandocs-markdown). Select it with `flavor: pandoc`:

```yaml
version: 1
flavor: pandoc
```

The approved target is Pandoc 3.12.1's `markdown` reader with a fixed set of 37 enabled and 37 disabled extensions. It has its own block and inline interpretation, including lists, footnotes, pipe tables, attributes, math, and raw content. It is not the `commonmark_x` reader. The [compatibility contract](engineering/pandoc-compatibility.md) and [machine-readable profile](engineering/pandoc-profile.json) pin every extension state and the source revision. No individual extension switches are exposed.

Automatic Pandoc identifiers, implicit header references, smart typography, LaTeX macro expansion, implicit figures, metadata/title blocks, and non-pipe table forms are disabled. Shared `resources.anchor_style` controls heading anchors; supported explicit IDs remain targets. Frontmatter is independently optional and still needs its own configuration contract. Pandoc attributes do not automatically enable Quarto chunk interpretation.

All enabled features and their source-preservation requirements must be verified before a build advertises this flavor. The contract does not add a renderer, filters, citeproc, a runtime Pandoc dependency, or compatibility with every Pandoc extension.

## Optional frontmatter headers

Frontmatter recognition is a separate option from Markdown flavor. It must be possible to enable or disable it with CommonMark, GFM, or Pandoc Markdown, and with any format supported in the future. Selecting a flavor does not itself enable frontmatter.

When enabled, a recognized metadata header is kept separate from the Markdown body and preserved as opaque content. Markdown style rules and inline directives do not interpret its contents. When disabled, the source follows the selected Markdown flavor's ordinary interpretation; for example, `---` can be Markdown syntax rather than a metadata delimiter.

The existing engineering proposal includes YAML and TOML headers. The exact configuration setting, default, accepted delimiters, and handling of malformed or unterminated headers remain undecided. No frontmatter configuration syntax is specified yet.

The initial configuration schema therefore rejects frontmatter settings. Until recognition is implemented, source follows ordinary flavor interpretation; explicit enabled-rule options requiring unavailable metadata interpretation fail capability validation. YAML configuration support does not itself enable YAML metadata headers.

## Consistent interpretation

Lint, fix, inspect, and both MCP modes must use the same selected flavor and frontmatter option. Format selection controls parsing; rule settings separately control which checks run.

Support claims must describe verified syntax and known gaps. Format conformance must be backed by specification examples. Uncertain parsing reduces rule coverage and prevents automatic fixes in affected regions. Lint/fix report that deficit as `incomplete` with exit 15 unless an operational error takes precedence. Syntax intentionally outside a selected dialect follows ordinary interpretation and does not by itself create missing coverage. See the [engineering design](engineering.md#markdown-support-and-parsing) for the parser and correction contract.
