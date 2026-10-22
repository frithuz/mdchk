# GFM compatibility and corpus contract

Status: approved on 2026-10-14. The [original published corpus and offline
extraction tooling](../../internal/parser/testdata/gfm/0.29-gfm/README.md) are
retained and verified. The [executable profile fixture contract](../../internal/parser/testdata/gfm/profile/README.md)
now supplies adaptations and independent source expectations. A private parser
stage now recognizes source-preserving tables, task items, strikethrough and
extended autolinks and shares the core lifecycle and inline machinery. The integrated profile, its conformance
adapter and mdchk conformance verification remain pending. Public parser APIs
remain CommonMark-only; configuration still rejects unavailable GFM. This contract
defines the target for [#158](https://github.com/frithuz/mdchk/issues/158), building
on the [verified core parser contract](https://github.com/frithuz/mdchk/issues/32).

## Compatibility profile

`flavor: gfm` selects CommonMark 0.31.2 plus all five extensions from the pinned
published GFM specification: tables, task list items, strikethrough, extended
autolinks, and disallowed raw HTML handling. The CommonMark and GFM modes share
the same core semantics. No individual extension switches are defined in the
configuration schema.

The published specification identifies itself as `0.29-gfm (2019-04-06)`.
mdchk's approved profile deliberately uses the newer CommonMark base and must
document that distinction in support claims. It does not claim exact historical
`0.29-gfm` conformance or parity with GitHub website rendering and sanitization.
Footnotes, GitHub alerts, emoji expansion, math, and automatic frontmatter
recognition are outside this profile. Syntax outside the selected dialect follows
ordinary interpretation and does not itself reduce coverage.

The configuration default remains `gfm`. A build without that capability rejects
the selected flavor before analysis, including selection by omission. It must not
fall back to CommonMark. Frontmatter remains independently optional, with settings
rejected by the initial schema until its separate contract and implementation are
available. See [Markdown formats](../markdown-formats.md) and
[initial capability handling](../configuration.md#initial-capability-handling).

For base syntax, CommonMark 0.31.2 governs. For extension syntax, the pinned
published GFM text and examples govern, subject to the documented base adaptation
and extension interactions below. An upstream implementation is a comparison
tool, not an authority that can silently change the target. Resolve any further
text/example ambiguity explicitly in the compatibility ledger before claiming
coverage for it.

## Source pins and attribution

| Artifact | Exact pin | Purpose |
| --- | --- | --- |
| [Published GFM HTML](https://github.github.io/gfm/) | Retrieved 2026-10-14; SHA-256 `b153d814fdfc8624bb6da7449162c1cd707a637f7d1c1b636eb44b9cf63fa220` | Normative extension snapshot and original examples |
| [CommonMark specification](https://github.com/commonmark/commonmark-spec/blob/9103e341a973013013bb1a80e13567007c5cef6f/spec.txt) | Version 0.31.2; commit `9103e341a973013013bb1a80e13567007c5cef6f`; `spec.txt` SHA-256 `257c41ad946f7a1414a499aca402a1aa8fdac3678532266611348c1cf54f4b80` | Core syntax and the 652-case corpus required by [#2](https://github.com/frithuz/mdchk/issues/2) |
| [cmark-gfm](https://github.com/github/cmark-gfm/tree/587a12bb54d95ac37241377e6ddc93ea0e45439b) | Release `0.29.0.gfm.13`; commit `587a12bb54d95ac37241377e6ddc93ea0e45439b` | Development comparison only |
| [cmark](https://github.com/commonmark/cmark/tree/eec0eeba6d31189fd828314576494566d539b1e3) | Release `0.31.2`; commit `eec0eeba6d31189fd828314576494566d539b1e3` | Core development comparison only |

The published page's matching source commit has not been established. Its content
digest is the snapshot revision; do not substitute the cmark-gfm commit or rely
on the `0.29-gfm` label alone. Vendor the exact HTML bytes before implementing
ingestion. Tests and regeneration must use retained inputs without network access.
A newer page, release, or equivalent source representation requires a reviewed
update with source and behavioral differences recorded.

The provenance manifest must include each artifact's origin URL, repository and
commit where established, retrieval date, byte count, SHA-256, license, and local
role. Record extractor/tool versions and generated-artifact hashes separately.
Do not attribute an unknown source revision to a comparison implementation.

Retain source notices and distinguish their licenses: the specification text and
derived examples are CC-BY-SA 4.0; the upstream Python test software has BSD-style
terms; normalization code includes MIT-derived material. Preserve the relevant
[CommonMark license](https://github.com/commonmark/commonmark-spec/blob/9103e341a973013013bb1a80e13567007c5cef6f/LICENSE)
and [cmark-gfm notices](https://github.com/github/cmark-gfm/blob/587a12bb54d95ac37241377e6ddc93ea0e45439b/COPYING),
including attribution and modification notices for derived data. Do not assign
one blanket software license to every imported artifact.

Comparison programs and an HTML test adapter are development tools. They add no
runtime parser dependency and do not require a production HTML renderer.

## Corpus ingestion and expectations

The pinned published snapshot contains 677 consecutive examples: 649 base cases
and 28 extension cases.

| Extension | Published example IDs | Count |
| --- | --- | --- |
| Tables | 198–205 | 8 |
| Task list items | 279–280 | 2 |
| Strikethrough | 491–493 | 3 |
| Extended autolinks | 622–635 | 14 |
| Disallowed raw HTML | 657 | 1 |

Extract from the retained HTML example containers, preserving both Markdown and
expected HTML code fields. Decode HTML entity presentation once, remove only
presentation markup such as space-highlighting spans, and convert the documented
arrow notation to tabs. Retain whitespace and terminal newlines. Preserve the
upstream example number, heading/section, source HTML line range, and input hash.
Fail ingestion if a code field is absent, an ID is missing or duplicated, the
expected counts change, or source digests differ.

Stable identity consists of the source snapshot revision plus upstream example
identity. Never match cases between editions by example number alone. Carry an
input hash and section mapping to make renumbering and content changes visible.
An original fixture retains its upstream expected HTML unchanged; derived
profile expectations belong in separate metadata. Regeneration must be offline,
deterministic, and byte-identical for the same source and tool pins.

Keep three concepts distinct in the fixture metadata:

- The original example and the scope it demonstrates: base syntax or one named
  extension. Internal section-scoped tests do not expose partial GFM profiles to
  users.
- The applicable CommonMark 0.31.2 interpretation, with a cited base-version
  adaptation wherever the original older expectation differs.
- The integrated GFM expectation with all five extensions active, including
  extension interactions that deliberately change base-example output.

Each adaptation records the original case identity, reason, governing spec
section, expected profile output, and relevant AST/span assertions. Every case
must have an explicit disposition and executable expectation in its applicable
scope. A blanket skip, expected-failure list, or comparison against an oracle's
output is not an accepted replacement for this accounting.

Use deterministic test-adapter serialization that matches expected HTML where
possible. A narrowly defined comparator may canonicalize irrelevant HTML
serialization, such as checkbox attribute order, while retaining raw differences.
It must not normalize away text, nesting, whitespace semantics, or link targets.
Pair output assertions with independent source and structure assertions.

The pinned cmark-gfm release corpus is not a substitute for the published
snapshot. Its `test/spec.txt` contains 672 fenced cases, while its extractor emits
670 because it omits two `disabled` task-list cases. Its additional extension
suite has 30 cases, includes footnotes, and contains an `<IGNORE>` expected value
that its runner treats as success. Import only explicitly mapped supplemental
cases, with actual expectations and attribution; inherit neither omission nor
automatic-success behavior.

## Base-version differences and extension interactions

Use the complete CommonMark 0.31.2 corpus. Passing most older inputs does not
establish behavior on the newer specification's additional cases.

| Area | Approved behavior | Consequence for consumers |
| --- | --- | --- |
| Inline HTML comments | CommonMark 0.31.2 recognizes some forms rejected by the older GFM base, including published examples 649–650 | Use the new HTML/prose boundaries and the shared directive-recognition contract |
| Literal HTML blocks | `textarea` belongs to the literal-block group; blank lines do not expose its contents as Markdown | Preserve protected content even when tag filtering escapes an HTML delimiter |
| HTML block names | Use the newer known-block list, including `search` and excluding `source` from that list | Follow the newer paragraph-interruption and HTML-block classification |
| Declarations | Use the newer declaration syntax, including lowercase forms | Preserve the corresponding raw HTML ranges |
| Whitespace and controls | Use the newer explicit rules for spaces, tabs, line endings, and controls rather than the older generic whitespace class | Test link, HTML, and block boundaries involving VT/FF and other controls |
| Unicode punctuation | Include the newer Unicode symbol categories in emphasis flanking | For example, `*£*bravo.` must follow the newer core interpretation |
| Other clarified core cases | Retain newer reference-label case folding, link grouping/destination, entity, and list fixtures | Do not copy historical implementation quirks into the shared core |

The [0.30 release notes](https://github.com/commonmark/commonmark-spec/releases/tag/0.30)
and [0.31.0 release notes](https://github.com/commonmark/commonmark-spec/releases/tag/0.31.0)
describe the intervening changes. The pinned spec text remains authoritative.

The initial adaptation ledger includes these published cases:

| Published case IDs | Adaptation | Integrated profile requirement |
| --- | --- | --- |
| 649–650 | Newer core HTML-comment recognition | Use CommonMark 0.31.2 raw HTML structure and retain source spans |
| 140–142, 145, 147 | Tag filtering changes original base-example output | Preserve raw HTML/protected ranges; the HTML test adapter escapes affected `<` characters |
| 611, 615, 617, 620, 621 | Extended autolinking changes original base-example output | Recognize the specified bare links and preserve exact visible-source/destination boundaries |

For example, base example 620 treats a bare web URL as text, while integrated GFM
recognizes it as an autolink. Run both relevant expectations in their declared
scopes. Do not rewrite the upstream fixture or count the intended difference as
an ignored failure. Additional interaction cases need the same explicit treatment.

Reference runs during the investigation are evidence about upstream programs,
not mdchk conformance. cmark matched 652/652 CommonMark cases and 647/649 published
GFM base cases; the two differences were 649–650. With section-scoped extensions,
cmark-gfm matched 661/677 published cases exactly and 663/677 after upstream HTML
normalization. The normalized result does not approve a broad comparator.

The remaining scoped differences identify why cmark-gfm cannot be the sole
authority: it flattens nested strong emphasis in 398, 426, 434–436, 473–475, and
477; differs on numeric entity case 324 and `xmpp:` cases 634–635; and uses newer
comment behavior for 649–650. The entity, strong-emphasis, and `xmpp:` differences
are not approved mdchk deviations. Follow the selected specification semantics.

## Extension source contract

Use the core parser's original source bytes and half-open byte ranges. Keep
semantic values separate from source spelling, retain LF/CRLF and container
boundaries, and expose sufficient information for diagnostics and safe edits.
Public positions follow the shared line/Unicode-column convention. Never infer
editable spans from rendered HTML or normalized inline text.

| Extension | Required representation | Additional validation cases |
| --- | --- | --- |
| Tables | Table/header/delimiter/body roles; alignment markers; original row/cell spans; actual separator positions; escaped-pipe information; raw cell counts including excess cells; semantic padding distinct from source cells | Escaped and unescaped pipes inside code/emphasis; unequal header/delimiter counts; short/long body rows; empty cells; optional outer pipes; lists/quotes; tabs, UTF-8, LF/CRLF |
| Task lists | Metadata on the list item and first paragraph; checked state; exact bracket/marker/separator spans; original `x`/`X` spelling; list marker and container spans | Unchecked/lowercase/uppercase checked markers; invalid markers; tight/loose/nested lists; non-list text; spacing; code and raw HTML |
| Strikethrough | Inline node, opening/closing delimiter spans and arity, original content spans; unmatched tildes retained | Single/double tildes; three-or-more literal tildes; mixed arities; escapes; paragraph boundaries; interactions with emphasis, links, and code |
| Extended autolinks | Link source kind distinguishing bare GFM, angle-delimited, inline, and reference forms; visible source separate from destination; injected destination prefixes have no source bytes | `www.`, HTTP(S), email, `mailto:`, `xmpp:` resource rules; punctuation, parentheses, entities, Unicode boundaries; nested-link prevention; code/HTML exclusions |
| Disallowed raw HTML | Preserve raw HTML nodes and protected regions; identify affected opening/closing tag ranges case-insensitively; expose the ranges needed by the HTML test adapter | All nine names; mixed case; exact tag boundaries and near misses; inline/block HTML, comments, and code |

The filtered tag names are `title`, `textarea`, `style`, `xmp`, `iframe`,
`noembed`, `noframes`, `script`, and `plaintext`. Filtering affects the relevant
`<` character in test-adapter output, not the original Markdown bytes. It does
not sanitize arbitrary HTML, reclassify raw HTML contents as prose, or extend
where directives are recognized. Link recognition does not fetch destinations.

Preserve all source cells even when GFM rendering pads a short body row or drops
excess cells. A padded semantic cell has no original cell bytes; an excess source
cell still needs a range and original content. Unequal header/delimiter widths
prevent GFM table recognition altogether. An unescaped pipe splits a GFM row even
between backticks; cell-content pipes must be escaped in that context.

For multiple immediately preceding backslashes, mdchk explicitly chooses
odd/even grouping: an odd run escapes the pipe, while an even run leaves a
structural separator. This technical compatibility interpretation follows the
core rule that an escaped backslash does not escape the next character
([retained CommonMark 0.31.2, Backslash escapes](../../internal/parser/testdata/commonmark/0.31.2/upstream/spec.txt),
lines 485–552). The [retained GFM table section](../../internal/parser/testdata/gfm/0.29-gfm/upstream/spec.html)
requires escaped pipes even inside code (example 200), but does not expressly
order longer backslash runs. This is an explicit mdchk choice, not an
unequivocal upstream multi-run mandate or a comparison-renderer oracle. The
source parser and profile validator share this interpretation. Existing original
examples and the 93 authored source fixtures remain unchanged; independent local
controls cover longer runs without adding profile consumer units or passes.

### Strikethrough pairing interpretation

The private syntax stage recognizes `Strike` using the shared inline delimiter
scanner, scoped pairing and noncrossing tree construction. The retained
[published section 6.5](../../internal/parser/testdata/gfm/0.29-gfm/upstream/spec.html)
requires matching one/two-tilde pairs, paragraph termination and literal inline
runs of three or more. It does not expressly specify every flanking condition,
mixed-run search order or partial-consumption rule. mdchk explicitly chooses
CommonMark 0.31.2 star-style Unicode left/right flanking on original logical
neighbors, including intraword eligibility. Underscore restrictions and the core
modulo-three rule do not apply to tildes.

Only maximal eligible unescaped original runs of length one or two can pair,
with exactly equal arity and whole-run consumption. Three-or-more runs stay
wholly literal; ordinary block tilde fences retain block-first precedence.
Source-order closers select the nearest compatible opener within the current
bracket scope and retire intervening candidates, preventing crossing pairs.
Failed searches have independent arity classes. Thus `~a~~` is literal, while
`~a~~b~` pairs the outside single tildes with raw payload `a~~b`; in
`~a ~~b~ c~~` the first single closer wins and retires the inside double opener.
These are explicit technical interpretations of incomplete upstream detail,
consistent with the approved mixed fixture, not comparison-renderer parity.
Escapes/entities and opaque syntax use the existing dispatch; decoded tildes
are never redispatched. Original fixtures and consumer accounting are unchanged.

### Extended-autolink interpretation

The private stage recognizes all published 622–635 forms, including the published
`xmpp:` resource semantics. It uses the existing structural inline scanner first,
then recognizes bare candidates in eligible original logical Text before final
emphasis/Strike pairing. Successful explicit links and opaque code/HTML retain
priority. URL interior delimiters remain literal; excluded suffix delimiters can
still pair around a Link. Image labels retain core semantics; only labels outside
successful explicit Links receive bare recognition, using the same raw boundary
rules. Neither `[` nor an image-label start adds an opening exception. Unresolved
brackets release prose but do not change the web opening whitelist.

The pinned extension's global whitespace class is exactly ASCII TAB, LF, VT, FF,
CR and SPACE. It permits a web opening and terminates its path; `<` also terminates
paths. Opening otherwise requires logical line start or `*_~(`. NBSP/U+0085
neither open nor stop a path. Other controls do not gain an added exclusion.
Composing section 6.9's plain “non-space” with that global class is an explicit
mdchk interpretation; selected-core Unicode whitespace predicates are unchanged.
Prefixes are case-sensitive literals. Ambiguous “alphanumeric” means ASCII letters
and digits in domain, email, resource and entity-resemblance tokens; Unicode path
bytes remain original content. No DNS, IDNA, URL validation or network lookup runs.
The domain following `www.` must itself contain a period: `www.a` is ordinary,
`www.a.b` recognizes. Labels are nonempty and have no imposed DNS length bound.

Web trimming repeats, in order, trailing `?!.,:*_~`, excess final parentheses
using whole-candidate counts, and one trailing `&` plus ASCII alphanumerics plus
`;`, until stable. Numeric entity spellings do not match that last pattern.
Interior surplus parentheses remain when no closing parenthesis is at the end.
These combined-suffix details are technical interpretations beyond the isolated
published examples. Email uses maximal raw local/domain tokens, trims terminal
periods and rejects invalid maximal domains without salvaging their suffixes.
Email and `mailto:`/`xmpp:` follow the more specific “within any text node” wording
rather than the web opening whitelist. An optional `xmpp:` resource contains one
slash and at least one ASCII alphanumeric, `@` or `.`; its terminal dot stays
included, and a second slash remains outside. These emptiness/dot composition
choices are explicit interpretations, not expanded RFC grammar.

Approved base 611 exposes the bare HTTP candidate after an actual failed raw
angle/HTML opener. This is a specific adaptation, not a general `<` whitelist
addition; an escaped `<` does not grant it. Approved 615 selects the raw maximal
`+@bar.example.com` over the competing core backslash escape, preserving `<foo\`
as preceding Text. The pinned extension does not unequivocally order email
recognition against escapes. This source-priority choice remains explicit and
uncertain upstream; core 615 still retains its independent escape interpretation.
Generated escape/entity syntax is never recognized as a new candidate.

`LinkProperties.Origin` adds only `bare-gfm`; existing `autolink` still means
angle-delimited. Visible envelope/Content/Label remain source-owned, separate from
Destination. Added `http://` and `mailto:` are spanless Synthetic destination
contributions; source protocols occur once with real provenance. The supplied
prefix retains the selected web/email association: a local part beginning www.
can still select mailto when web recognition fails or cannot open. The constructor
checks coherent supplied contributions rather than inferring the winner from
www spelling alone. Changed surrounding Text preserves the shared scan's
source-owned empty normalization before SoftBreak while reconstructing ordinary
escapes/entities once and retaining 615's raw priority. Bare FTP remains
ordinary prose under section 6.9; future MD034 must also inspect eligible FTP
prose and source context rather than treating every Link as exempt. Angle FTP
retains its ordinary core origin. MD054 still has exactly five styles and maps
both recognized origins to its existing autolink style. No rule implementation,
public partial profile, adapter or completed GFM consumer claim is added here.

### Disallowed-HTML interpretation

The private syntax stage adds immutable `HTMLProperties.FilteredTags` on existing
raw owners. The pinned section 6.11 lists the nine names and leading-angle
escaping, but does not spell out all malformed-prefix and ownership composition
cases. The following are explicit mdchk technical interpretations, consistent
with the approved fixtures, rather than unequivocal upstream prose or website
sanitization behavior. The retained supplied-fact validator is not an exhaustive
recognizer and its permissive slash check does not define lone-slash recognition.

A candidate is original logical `<`, optionally one immediate `/`, followed by
exactly one of the nine names with ASCII case folding only. Immediately after
the name require TAB, LF, CR, SPACE, `>`, or `/` immediately followed by `>`.
EOF immediately after the name, VT/FF, Unicode whitespace, NUL and name suffixes
fail. `<script/>` qualifies; `<script/x>`, `<script/` and `<script` at EOF do
not. In established raw payload `<script ` qualifies even without a complete
tag. `</script/>` qualifies as a prefix with no complete core closing-tag claim.
The optional full Tag range comes from the shared HTML lexer; it is never a
guessed scan to the next angle. Prefix and original affected angle remain
independent source facts when full grammar fails. Filtering cannot create a raw
owner from Text or reparse its interior as Markdown.

Ordinary InlineHTML tests only its construct start: comments, PI, CDATA,
declarations and quoted attribute interiors gain no nested facts. HTMLBlock
visits its whole logical raw payload, including quoted/PI/CDATA/declaration and
literal/comment-looking substrings. Only existing actual confirmed comments are
excluded through their real close or EOF. A form-2 comment's physical closing
line can contain subsequent eligible tags. Existing private htmlTail comments
remain exclusions; tail tags contribute once to the enclosing HTMLBlock.
Comment-looking strings inside opaque form-1/6/7 interiors do not acquire new
comment/directive authority merely to suppress filtering. This owner-sensitive
composition preserves both the approved comment-protection witness and case657's
inner xmp facts. Core classification, textarea blank-line opacity, rawTag
transitions, references, literal/protected inventories and directive eligibility
remain unchanged. Code and generated/escaped angles gain no filter facts.

No public selector, renderer, source fix, sanitizer or production GFM admission
is added. Private recognition of five syntax extensions does not establish full
profile conformance; the adapter, activation and actual consumer gates remain
separate, with all 2,117 actual profile consumer units still NOT-RUN.

## Rule and operation integration

- [MD055](../rules/tables/md055.md) and [MD060](../rules/tables/md060.md) use
  parser-owned separators and original cell spans. Edits preserve content,
  escapes, counts, and excess source cells, and are checked by reparsing.
- [MD056](../rules/tables/md056.md) compares original body-cell counts with the
  recognized table width before padding or truncation. It remains report-only.
- [MD075](../rules/tables/md075.md) checks unrecognized pipe-row candidates,
  including header/delimiter mismatches when its candidate criteria apply. It
  does not invent a parsed table or duplicate MD056 on recognized body rows.
- [MD034](../rules/links-and-images/md034.md) still reports eligible bare URLs
  recognized as GFM autolinks. Explicit angle, inline, and reference markup
  remains exempt.
- [MD054](../rules/links-and-images/md054.md) maps bare and angle-delimited
  autolinks to its existing `autolink` style while preserving their source kinds.
  It retains five configuration styles.

Lint, fix, inspect, and both MCP modes must use the same parser and source
classification. Test consistent findings, source ranges, fix eligibility, and
inspection attribution. Uncertain regions reduce coverage for affected rules and
prohibit automatic fixes. Lint/fix preserve available findings and report
`incomplete` with exit 15 unless an operational error takes precedence, following
the shared [result contract](../output-formats.md).

## Verification and completion gates

Before claiming the selected profile, require:

1. All 652 CommonMark 0.31.2 examples passing in CommonMark mode, with the core
   source/span contract verified.
2. All 28 published GFM extension examples passing in their applicable scope and
   all 677 published cases accounted for with original and reviewed profile
   expectations. Every integrated-profile expectation must pass; accounting does
   not excuse known failures.
3. Paired CommonMark/GFM fixtures proving ordinary core interpretation when
   extensions are absent, plus integrated tests with all five extensions active.
4. Independent AST, original-byte, source-span, protected-region, and container
   assertions covering the extension matrix above. Include adversarial inputs and
   resource-limit behavior from the core parser contract.
5. Rule and operation integration checks, including preserved bytes on lint,
   safe-fix reparsing/idempotence, partial coverage, and independently controlled
   frontmatter when that capability becomes available.
6. Offline byte-identical fixture regeneration, verified source/tool hashes,
   complete attribution, and no silently skipped or automatic-success cases.

[#158](https://github.com/frithuz/mdchk/issues/158) records the compatibility and
corpus contract and retains its formal dependency on
[#32](https://github.com/frithuz/mdchk/issues/32).
[#159](https://github.com/frithuz/mdchk/issues/159) implements tables and task lists;
[#160](https://github.com/frithuz/mdchk/issues/160) implements the remaining
extensions; [#161](https://github.com/frithuz/mdchk/issues/161) validates conformance
and consumer integrations. Approval of this contract does not establish parser
completion or justify closing those implementation gates.
