# Pandoc compatibility and corpus contract

Status: approved on 2026-10-14. Parser implementation, corpus tooling, and mdchk
conformance verification remain pending. This defines the target for
[#162](https://github.com/frithuz/mdchk/issues/162) and implementation in #163–#165.

## Compatibility profile

`flavor: pandoc` selects a fixed profile of Pandoc 3.12.1's `markdown` reader.
It includes footnotes, attributes, containers/lists, pipe tables with captions,
inline formatting, citations, and recognized math/raw content. Simple, multiline,
and grid tables are excluded. No extension switches are exposed to users.

The profile is distinct from Pandoc's default reader settings, `commonmark_x`,
and mdchk's CommonMark/GFM profiles. It does not claim full Pandoc renderer parity.
Frontmatter remains independent; automatic Pandoc heading IDs and implicit heading
references are disabled. Shared anchor policy follows the
[resource contract](resources.md), with `github` and `explicit` choices.

Use the internal Go parser with original source ownership. Pandoc is an optional
development oracle, not a runtime dependency. Updates require an explicit
compatibility review rather than following a newer executable automatically.

## Authority and source pins

The [3.12.1 release](https://github.com/jgm/pandoc/releases/tag/3.12.1) was published
2026-10-08. Freeze its manual and source alongside the exact profile.

| Artifact | Pin |
| --- | --- |
| Source revision | `e51c9c6054c8f4ec5c3209d5abe10939dfe2963e` |
| Source archive SHA-256 | `1526b4b898de8419ba7b5a46eb4513ebebda81db485ec983a991e63aca806856` |
| Official macOS ARM64 ZIP SHA-256 | `c2146cf33a583f3ac93eccddf1bb662371d5cfcce1aad1c87ad860cca1435d46`, verified against release asset digest |
| Version-matched `MANUAL.txt` SHA-256 | `1d12bfb86e515bff05f56dab8dbfcfa409303cab65224e9fc4a037794ae09e60` |
| Markdown reader SHA-256 | `7c4a62e5f9ddec61823d52e34160d1b2ff6fa14fc925eef672b51e65f393a2c7` |

Authority order: the adopted mdchk profile and its explicit differences; the
pinned Pandoc manual for intended behavior; the pinned reader and reviewed
oracle observations for ambiguities and enumerated manual discrepancies. Do not
blindly regenerate expectations from a newer executable or turn every oracle
warning into an mdchk diagnostic. The relevant sources are the
[pinned manual](https://github.com/jgm/pandoc/blob/e51c9c6054c8f4ec5c3209d5abe10939dfe2963e/MANUAL.txt),
[reader](https://github.com/jgm/pandoc/blob/e51c9c6054c8f4ec5c3209d5abe10939dfe2963e/src/Text/Pandoc/Readers/Markdown.hs),
and [reader tests](https://github.com/jgm/pandoc/blob/e51c9c6054c8f4ec5c3209d5abe10939dfe2963e/test/Tests/Readers/Markdown.hs).

## Extension set

The [profile manifest](pandoc-profile.json) freezes every switch reported by
`--list-extensions=markdown`, including upstream defaults and the full explicit
comparison-reader argument. It contains all 74 switches: 37 enabled and 37 disabled.
This is an internal compatibility revision, not a new user configuration setting.
The source and consumer requirements below form the implementation checklist.

The 37 enabled extensions are:

| Area | Extensions |
| --- | --- |
| Core interpretation | `all_symbols_escapable`, `escaped_line_breaks`, `intraword_underscores`, `shortcut_reference_links`, `blank_before_blockquote`, `blank_before_header`, `space_in_atx_header` |
| Code and attributes | `backtick_code_blocks`, `fenced_code_blocks`, `fenced_code_attributes`, `inline_code_attributes`, `header_attributes`, `link_attributes`, `bracketed_spans` |
| Containers and lists | `fenced_divs`, `definition_lists`, `line_blocks`, `fancy_lists`, `startnum`, `example_lists`, `task_lists` |
| Footnotes | `footnotes`, `inline_notes` |
| Tables | `pipe_tables`, `table_captions`, `table_attributes` |
| Inline syntax | `strikeout`, `subscript`, `superscript`, `citations` |
| HTML | `raw_html`, `markdown_in_html_blocks`, `native_divs`, `native_spans` |
| Protected foreign syntax | `raw_tex`, `raw_attribute`, `tex_math_dollars` |

All are enabled in upstream 3.12.1. The profile disables ten other
upstream defaults:

| Disabled defaults | Reason |
| --- | --- |
| `simple_tables`, `multiline_tables`, `grid_tables` | The current table rules specify pipe tables. These other layouts introduce separate recognition, nested-cell, span, and correction contracts. Defer them explicitly. |
| `yaml_metadata_block`, `pandoc_title_block` | Flavor must not enable metadata recognition. In particular, Pandoc YAML blocks can occur mid-document, beyond the separately planned frontmatter boundary. |
| `auto_identifiers`, `implicit_header_references` | Keep anchor generation and heading-reference expansion out of the initial parsing profile; retain explicit IDs and ordinary explicit link references. |
| `smart` | Preserve punctuation rather than transform it before rules such as MD088 inspect it. |
| `latex_macros` | Recognize raw TeX/math without expanding macros or changing source content. |
| `implicit_figures` | Keep image paragraphs as images; figure promotion is outside this initial syntax/consumer contract. |

All 27 upstream-disabled extensions remain disabled. This includes bare-URI
autolinking, alerts, emoji, wikilinks, abbreviations, alternate metadata forms,
alternate math delimiters, alternate automatic-ID schemes, and reader path
rebasing. Syntax excluded from the profile follows ordinary interpretation;
it does not by itself imply incomplete analysis. The full list is in the manifest.

This is broader than a minimal footnotes/IDs subset because generic lint rules
must understand common Pandoc containers and protected regions. It remains
smaller than upstream defaults and avoids an arbitrary extension combination
matrix. Supporting all defaults or exposing switches would enlarge both the
implementation and the compatibility promises substantially.

## Core parsing and source preservation

Use Pandoc `markdown` semantics for this flavor, not `commonmark_x` and not
CommonMark with a few inline extensions. Shared parser infrastructure is still
useful, but interruption, indentation, delimiters, escaping, and HTML behavior
must branch where required. The probes confirmed that a heading, block quote,
or bullet list directly following paragraph text without a blank line can remain
paragraph content under the selected profile.

Two manual statements require explicit exceptions in the adopted contract:

- The manual excludes footnote definitions inside block elements, but the pinned
  reader accepts definitions inside quotes and lists, including references from
  outside those containers. It also accepts a definition nested in another
  definition. Adopt this observed document-wide binding behavior while preserving
  physical nesting and source spans; add regressions instead of copying the
  manual restriction.
- The manual describes blank lines around fenced code as required, but the
  pinned reader recognizes the tested backtick fences adjacent to prose. Adopt
  recognized code boundaries; leave spacing findings to MD031. Broader fence
  variants need fixtures before claiming equivalent coverage.

The reader rejects `markdown+sourcepos` with exit 23. Its JSON representation
also erases information needed by a linter: note labels and source placement,
original table widths, task-marker spelling, example-reference syntax, and some
HTML origins. The Go parser must retain original bytes, byte spans, container
prefixes, raw attributes, and semantic nodes together. An AST conversion or
external Pandoc invocation cannot substitute for that work.

## Footnote contract and rule changes

- Compare valid named-note labels exactly, case-sensitively, with no case folding,
  whitespace normalization, Unicode normalization, or Markdown unescaping.
  `a` and `A`, and composed/decomposed Unicode spellings, are different labels.
  Labels exclude spaces, tabs, line breaks, `^`, `[` and `]`. Invalid note-like
  syntax can become ordinary reference-link syntax; do not force it into MD066.
- Preserve a document-wide index of **all** definitions and eligible references,
  including unused, unresolved, duplicate, and empty records. Pandoc leaves
  unresolved references as text, discards unused definitions from its AST, and
  uses the last duplicate definition. Match the binding semantics while reporting
  the original offending source through MD066. Never discard earlier duplicates.
- Keep references within named-note bodies literal, as the reader does when
  resolving notes. They do not establish a semantic use for MD066 or ordering
  for MD067. Named references inside inline notes can resolve; count those as
  body references in source order. Do not copy Pandoc's unused-note warnings:
  its internal marker bookkeeping can count text that stays literal in a note.
- MD067 orders named definitions by first eligible body reference. Anonymous
  inline notes have no separately movable definition and are excluded. Never
  reorder across container boundaries or when duplicate/missing bindings,
  nested definitions, attached comments, or multi-block ownership make a move
  ambiguous. Preserve full blocks and verify binding/meaning after any fix.
- MD068 applies its empty-content check to anonymous inline notes such as `^[]`
  and whitespace-only `^[  ]`, alongside named definitions. Preserve the existing
  non-whitespace-content definition and report-only policy; do not invent text.

These requirements make MD066–MD068 useful without depending on writer-assigned note
numbers or diagnostic strings. Code, raw content, escaped syntax, and comments
must not manufacture footnote references.

## Pipe tables and existing rules

Pandoc and GFM need different cell scanners. In the pinned Pandoc reader,
recognized code spans, math, raw HTML tokens, escaped characters, and raw TeX
can contain pipes that stay within a cell. GFM's unescaped code-span pipes split
cells under the already approved contract. Preserve the selected parser's
separator positions; rules must never split normalized text themselves.

Pandoc takes table width from the delimiter row and accepts shorter or longer
headers. It pads or truncates semantic cells, including header cells. It also
accepts `+` between delimiter segments (the orgtbl form), and the probe confirmed
single-dash delimiter segments. Retain every original cell even when absent from
the semantic output.

| Rule | Pandoc behavior |
| --- | --- |
| MD055 | Check/add/remove only actual outer syntax pipes; retain inner code/math/raw pipes and original cells. |
| MD056 | Check body rows and recognized Pandoc header rows whose raw width differs from the delimiter row. Report only. A GFM header mismatch still prevents table recognition and belongs to MD075. |
| MD058 | Treat an attached caption and its attributes as part of the table extent for surrounding blank-line checks. |
| MD060 | Adjust source padding while preserving delimiter lengths, alignment, separators, attributes, all cells, and inline content. Never manufacture padded cells or discard excess ones. |
| MD075 | Diagnose qualifying pipe-row candidates outside parsed tables; do not double-report a Pandoc header mismatch as a missing header. |

Keep captions and `table_attributes` together; recognized table IDs feed shared
explicit-anchor lookup. `--columns=80` and `--tab-stop=4` are fixed oracle settings.
Pandoc's calculated table column proportions are rendering-oriented data; preserve
raw delimiter lengths and validate semantic structure without confusing those
proportions with MD060's source padding policy.

## IDs, containers, HTML, and remaining consumers

Preserve explicit attributes on headings, code blocks, inline code, links,
images, spans, divisions, and captioned tables. Store raw occurrences as well as
the effective ID; the duplicate-ID-attribute probe selects the last `#id`.
Do not silently drop source duplicates during parsing. Valid explicit IDs on
these nodes are eligible MD051 targets, alongside explicit HTML IDs.
The shared [anchor contract](../rules/index.md#anchor-resolution) includes
these supported format-specific IDs.
MD073 still builds a TOC from headings only; MD080 still compares heading bases
only. A general duplicate-ID diagnostic would require separate rule design.

The existing `github` anchor choice remains an explicit mdchk convention, not a
promise that Pandoc generates the same slugs. Explicit heading IDs retain their
precedence. A future `pandoc` anchor style needs a deliberate compatibility and schema
decision; it is not enabled implicitly here. Fragments retain the
existing once-only UTF-8 percent decoding and case-sensitive comparison.

Other necessary consumer clarifications:

- MD029's numeric-prefix correction applies to decimal numbered lists. Preserve
  alphabetic/Roman markers and example labels; do not convert them to decimal.
  Shared indentation and marker-spacing rules still need their parser spans.
- MD040 obtains a fenced-code language from the bare language or first class
  in Pandoc attributes. An ID or key/value alone supplies no language. Raw-format
  fences are raw content, not language-less code. MD048 can inspect their fence
  spelling while keeping the raw payload protected and verifying meaning.
- `markdown_in_html_blocks` and native HTML containers expose permitted Markdown
  children to ordinary rules. Raw tags stay protected; script/style/pre/textarea
  payloads stay literal. Retain HTML source origin so native Div/Span conversion
  does not hide tags from MD033. MD091 applies only to eligible content the
  selected parser actually leaves raw.
- Math, raw TeX, raw-format payloads, citation control syntax, and attribute
  syntax need explicit protection. Ordinary textual children remain lintable.
  Citations require no bibliography lookup, filters, or citeproc execution.
- Quarto remains #174 work. In the tested ordinary Pandoc reader, a `{python}`
  chunk-style opener did not become a fenced code block. Do not treat Pandoc
  attributes as permission to recognize or execute Quarto chunks. Existing
  syntax still determines ordinary protection until that extension is adopted.

## Metadata, configuration, and unavailable support

`flavor: pandoc` does not consume document-start YAML, mid-document YAML, percent
title blocks, or TOML as metadata. With frontmatter unavailable/disabled, those
bytes follow the selected reader's ordinary body interpretation. A leading YAML
example became a thematic break and setext heading in the selected-profile probe.
Do not protect it as metadata merely because it resembles a header.

#166–#169 will decide the independently selected, document-start frontmatter
contract; #178 governs format-aware metadata edits. Do not invent delimiter,
malformed-header, or configuration defaults in this stage.

Valid intended configuration once the selected capabilities are implemented:

```yaml
version: 1
flavor: pandoc
resources:
  anchor_style: explicit
```

`flavor: pandoc+footnotes` is an invalid flavor value. A top-level
`pandoc_extensions` list or `pandoc` options mapping is an unknown key.
`resources.anchor_style: pandoc` is not an adopted option. The shared
[resource contract](resources.md#shared-settings-and-anchor-consistency) owns
anchor policy; MD051 and MD080 no longer have per-rule `anchor_style` options. Frontmatter settings remain
rejected until their separate schema is approved. The existing directive
capability checks still apply; early runs need effective directives disabled
until implemented.

Selecting an unavailable Pandoc profile fails capability validation before
analysis, without fallback. The public capability must only be advertised once
the complete selected profile is delivered and verified. Unexpected parser gaps
in an advertised capability produce incomplete coverage and protect affected
regions from fixes. Intentionally excluded syntax and unavailable rule
implementations retain #124's separate applicability/coverage treatment.

## Corpus, licenses, and completion requirements

Start the mdchk fixture corpus with original inputs and independently reviewed
semantic expectations. Retain raw oracle outputs separately from expectations.
Record each case's profile, source hash, upstream reference or original authorship,
expected structure, source spans, protected regions, and rationale for differences.
Use the exact reader argument, empty data directory, sandbox, fixed columns/tab
settings, and no filters/citeproc. Ordinary tests must run offline without
requiring Pandoc; regeneration/comparison is an optional development task.

Pandoc's package declares GPL-2.0-or-later, and its Markdown Haskell tests carry
that notice explicitly. Preserve `COPYING.md`, `COPYRIGHT`, and per-file notices
for retained upstream references. Do not copy Haskell implementation/test code
into the Go product or relabel upstream fixtures as permissive. The initial
contract needs no vendored upstream code. Any selected upstream corpus imports
must have separately recorded provenance and licenses; this contract does
not choose mdchk's overall software license.

Required verification beyond these research probes:

1. For **every selected extension**, positive, ordinary-fallback/malformed,
   escaped, code/raw/metadata protection, nested-container, LF/CRLF, Unicode,
   original-byte-span, and precedence tests as applicable. Map cases to all
   37 enabled switches and intentional exclusions; no silent skip accounting.
2. Independent source/semantic assertions for unresolved/unused/duplicate notes,
   all table cells, raw attributes, HTML origins, task markers, and example
   references. Oracle JSON cannot verify those source records.
3. Flavor-isolation regressions against the approved mdchk CommonMark/GFM
   corpora, especially tables, tildes, HTML, paragraph interruption, and metadata.
4. Consumer fixtures for the affected rules, explicit IDs/resources, inspection,
   directive eligibility, and consistent CLI/project-MCP/standalone behavior.
5. Fix preservation: source bounds, unchanged opaque bytes and unedited content,
   reparsing, binding/structure checks, idempotence, and refusal on uncertainty.
   Run appropriate Go tests/vet/fuzz when the implementation exists.

## Delivery and verification gates

[#163](https://github.com/frithuz/mdchk/issues/163) owns selected block/container
recognition and source records; [#164](https://github.com/frithuz/mdchk/issues/164)
owns selected inline/reference extensions; [#165](https://github.com/frithuz/mdchk/issues/165)
verifies every adopted extension, source span, and consumer. Enumerate all 37
enabled switches and their precedence/protection interactions in the checklist.
Do not advertise the flavor until the complete profile is delivered and verified.
Approval of this contract establishes neither implementation nor conformance.

Frontmatter recognition and metadata edits remain #166–#169/#178 work; Quarto
and MkDocs integration remain #174 work. Explicit IDs feed the shared resource
and anchor services without adding an automatic Pandoc slug convention.
