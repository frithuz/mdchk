# Supported Markdown formats

mdchk's confirmed format scope is CommonMark, GitHub Flavored Markdown (GFM), and Pandoc Markdown, with independently optional frontmatter support. This documents the intended support contract; parser implementation and conformance verification are still pending.

## CommonMark is the foundation

[CommonMark](https://spec.commonmark.org/) defines the base Markdown syntax and interpretation. CommonMark defines headings, paragraphs, lists, block quotes, code, links, images, emphasis, HTML, and other standard constructs.

Select it with `flavor: commonmark` when the document should follow CommonMark without GFM extensions:

```yaml
version: 1
flavor: commonmark
```

CommonMark being the foundation does not change the existing configuration default: omitted `flavor` selects `gfm`.

## GitHub Flavored Markdown

Select GFM with `flavor: gfm`:

```yaml
version: 1
flavor: gfm
```

The [GFM specification](https://github.github.io/gfm/) extends CommonMark with tables, task list items, strikethrough, extended autolinks, and disallowed raw HTML handling. These extensions belong to the GFM support scope. In CommonMark mode, the same source follows CommonMark interpretation rather than automatically enabling extensions.

GFM support refers to the specified dialect. GitHub's website also performs additional rendering, post-processing, and sanitization; matching every GitHub website feature is outside this format contract.

## Pandoc Markdown

Pandoc Markdown is also approved for support. Its reference is the [Pandoc User’s Guide](https://pandoc.org/MANUAL.html#pandocs-markdown). Select it with `flavor: pandoc`:

```yaml
version: 1
flavor: pandoc
```

CommonMark remains mdchk’s foundation; Pandoc Markdown requires its own dialect interpretation rather than being assumed to match CommonMark or GFM. The precise Pandoc extension set and compatibility target remain to be defined. Approval adds the format to the support scope without defining an implementation plan.

## Optional frontmatter headers

Frontmatter recognition is a separate option from Markdown flavor. It must be possible to enable or disable it with CommonMark, GFM, or Pandoc Markdown, and with any format supported in the future. Selecting a flavor does not itself enable frontmatter.

When enabled, a recognized metadata header is kept separate from the Markdown body and preserved as opaque content. Markdown style rules and inline directives do not interpret its contents. When disabled, the source follows the selected Markdown flavor's ordinary interpretation; for example, `---` can be Markdown syntax rather than a metadata delimiter.

The existing engineering proposal includes YAML and TOML headers. The exact configuration setting, default, accepted delimiters, and handling of malformed or unterminated headers remain undecided. No frontmatter configuration syntax is specified yet.

## Consistent interpretation

Lint, fix, inspect, and both MCP modes must use the same selected flavor and frontmatter option. Format selection controls parsing; rule settings separately control which checks run.

Support claims must describe verified syntax and known gaps. Format conformance must be backed by specification examples. Uncertain parsing reduces rule coverage and prevents automatic fixes in affected regions. See the [engineering design](engineering.md#markdown-support-and-parsing) for the parser and correction contract.
