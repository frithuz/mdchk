# Parser foundation contract

`internal/parser` defines an in-memory CommonMark 0.31.2 model. It recognizes no
nonempty syntax yet. The catalog is a contract for constructed fixtures and later
providers, independent of private Go layout or the future JSON projection.

## Parse outcomes

Acquire a snapshot with `NewSource(content)`, then call `Parse(ctx, source)` with
a non-nil context. Both operations stay in memory. `Source` retains arbitrary
bytes; Parse validates strict UTF-8 before any normal Markdown work. Malformed
bytes remain available through `result.Source`, with no transcoding/replacement.
Literal U+FFFD and U+0000 are valid encoding; scanner helpers replace NUL only in
semantic contributions, never in Source. Encoding failure takes precedence over
interruption. Rule/io attribution, CLI exits and result-schema adaptation belong
to callers.

| Input/outcome | Status / AST | Reliable data and phase progress |
| --- | --- | --- |
| Nil or zero-length source | `Complete` / `Available` | Root `[0,0)`, no children, available empty reference records/lookup, both phases complete |
| Any valid nonempty source | `Incomplete` / `Partial` | Source-only root `[0,N)`, one `SyntaxUnimplemented` uncertainty over `[0,N)`, blocks pending, inlines not started; reference records/lookup unavailable |
| Invalid UTF-8 | `Failed` / `Unavailable` | Original Source, typed `Failure` with `InvalidUTF8` code and snapshot-bound byte location, nil Document, neither phase started |
| Cancellation/deadline on otherwise valid source | `Incomplete` / `Partial` | Original Source and source-only root, affected region with `Cancelled`/`DeadlineExceeded`, separate Interruption, nil Failure; unfinished work interrupted |
| Other operational failure | `Failed` / `Unavailable` | Retained Source, typed stable failure code, nil Document; synthetic model contract only at present |

Every nonempty input includes whitespace and extension/frontmatter-looking text.
Its **core interpretation is pending**; disabled extensions do not become errors.
A source-only root proves document identity and envelope, never absent syntax.
Neither nil Failure nor empty children/references proves complete parsing. No
syntax-dependent rule or fix may use uncertain regions as reliable syntax.
Interruption on empty input has the present zero-width region `[0,0)`.
Uncertainty order is start, end, reason; this foundation emits exactly one region
when incomplete. `Failure.Location` is a present, snapshot-owned zero-width point
at the first invalid scalar's leading original byte, including malformed/truncated
multibyte sequences. Its Start is the authoritative zero-based offset; byte zero
is distinct from an absent location on an unlocated/synthetic internal failure.
LineIndex supplies public line/column conversion and valid-prefix failure points;
engine/io attribution remains #123.
Failure, uncertainty and interruption have typed codes so callers
never need to parse messages. `InternalFailure` is available for constructed
failure examples; Parse currently produces only `InvalidUTF8` operational failures.

For example:

```go
source := parser.NewSource([]byte("hello\n"))
result := parser.Parse(context.Background(), source)
// result.Status == parser.Incomplete; result.Failure == nil.
// result.AST == parser.Partial; root envelope is [0,6).
// result.Uncertainty[0].Reason == parser.SyntaxUnimplemented.
// An empty child list here is not evidence of an empty Markdown document.
```

The private block/reference phase precedes inline processing. Inlines cannot run
until the block phase and effective reference lookup are complete. Empty input
completes the empty registry; nonempty input leaves it unavailable and skips
inlines. Both phases receive the same caller context. Context checks currently
occur at phase boundaries, not within UTF-8 scanning or every constructor loop.
#31 owns full cancellation checkpoints, exact work metering and budget enforcement.
Its operation budget must be shared across both phases and candidate reparses,
not reset in each phase/document. No speculative budget interface is added here.

## Snapshots, spans and ownership

`Source` copies incoming bytes; `Bytes` returns an independent copy. Copies of a
Source share its snapshot identity. Independent `NewSource` calls have different
identities even for equal bytes. The zero Source is the same empty snapshot as
other zero Source values. Identity is private ownership metadata, not a random or
pointer-based output ID.

`source.Span(start,end)` validates `0 <= start <= end <= source.Len()`. Spans are
zero-based half-open original-byte ranges. Document roots span `[0,N)`; EOF is
`[N,N)`. A zero Span is **absent**; `Present()` distinguishes it from `[0,0)`.
Start/End values are meaningful only when Present is true. `source.Text(span)`
rejects absent/foreign spans and returns an immutable string view without copying
the whole document. Retaining a view can retain the snapshot backing storage.

Public `Position` means one-based physical lines and Unicode code-point columns,
tabs and combining code points each count as one column, and range ends are
exclusive. Structural indentation uses a separate zero-based column count.

## Physical lines, positions and scanner contributions

`NewLineIndex(source)` reuses the immutable snapshot and records LF, CRLF and lone
CR boundaries. The zero LineIndex indexes the empty zero Source. `LineCount` and
one-based `Line` work on arbitrary bytes, even malformed UTF-8. PhysicalLine
separates Content and Ending spans; an unterminated line has a present empty Ending
at EOF. Empty input has zero selectable content lines. A terminal ending creates
no extra selectable line; consecutive endings still delimit actual empty lines.

On strict-valid UTF-8 sources, `Position(offset)` and `Offset(position)` convert
original-byte scalar boundaries, rejecting continuation-byte and CRLF interiors,
out-of-bounds offsets, nonpositive positions and nonexistent lines/columns with
`ErrPosition`. They never clamp, snap or alias requests. The boundary before an
entire terminator is the preceding line's end; after it is next-line column 1.
Empty EOF is (1,1); EOF after a terminal ending has a virtual next-line position.
Those valid endpoints do not create selectable content lines. Unterminated EOF
is the final content line's end. Byte Span/Text access remains broader than this
conversion domain. Line requests outside selectable content also return ErrPosition.

`EncodingFailure` returns fresh typed InvalidUTF8 metadata, retaining the first
invalid scalar's leading original-byte point. The metadata cannot mutate the
index. Ordinary conversion, semantic and indentation operations on malformed
sources return this typed failure; they do not partially parse, transcode or assign
invalid bytes a character width. `FailurePosition` alone derives that first point
from the valid prefix (or returns ErrPosition if there is no encoding failure).
Byte-line indexing and raw spans remain usable independently of decoding.

`SemanticText(span)` returns caller-owned Value/Segment slices. Exact runs retain
original views; NUL contributes U+FFFD with Normalized provenance over the original
one-byte NUL span. Literal U+FFFD stays Exact. Absent/foreign ranges return ErrRange;
scalar-interior endpoints return ErrPosition, including empty interior ranges.
Semantic byte ranges may split CRLF, independently of diagnostic conversion.

`Indentation(line)` starts a caller-owned cursor at a physical content line. Its
zero value is empty. `AdvanceMarker` advances one non-space/tab scalar selected
by the caller and one structural column; it rejects EOF or a pending partial tab.
`Consume(n)` strips up to n ASCII space/tab columns, stopping at content/EOF.
Four-column tab stops keep their phase across calls and caller-interpreted
container markers. Negative requests return ErrIndentation without mutation; zero
is a no-op. Each consumed tab contribution has PartialTab provenance over its
original one-byte span, even when a whole tab is expanded. Offset remains on a
partly consumed tab until its last column is consumed. Remaining returns the
residual spaces with that same provenance, followed by semantic content with
internal tabs kept literal. Line endings are excluded. Cursor copies advance
independently; simultaneous mutation of one cursor needs caller synchronization.
These helpers recognize no quote, list, code or other Markdown syntax.

For `>\t\tfoo`, advance `>`, consume one delimiter column, then four indentation
columns. The cursor is at structural column 6, original byte 2, with two spaces
remaining from tab [2,3), followed by Exact `foo` at [3,6). Repeating calls on the
same immutable snapshot preserves the same raw provenance.

For the seven original bytes `é\t\x00\r\nZ`, NUL [3,4) contributes U+FFFD; `Z`
remains byte 6 / diagnostic (2,1), and EOF byte 7 / (2,2). The semantic replacement
is longer in UTF-8, but shifts no original coordinates. The tab counts as one
diagnostic column; structural consumption instead uses four-column stops.

Construction is O(N) time and O(L) line-boundary storage, without another whole
snapshot copy. Position lookup is O(log L + B) and reverse lookup O(B), where B is
the selected line-prefix byte count; neither scans earlier lines. Encoding status
is cached in the immutable index. Semantic output and Remaining take O(selected
bytes) work/storage; Consume takes O(consumed bytes + columns). Repeated lookups
on a long line may repeatedly decode its prefix. Checkpoints, batching and measured
benchmarks remain optional follow-ups, with no numeric performance gate.

Character helpers follow CommonMark 0.31.2: blank content is ASCII space/tab or
empty, not an ending; Unicode whitespace is Zs plus TAB/LF/FF/CR; Unicode punctuation
is P or S; ASCII controls are U+0000–001F/U+007F and ASCII punctuation is the four
specified printable ranges. No broader whitespace predicate is substituted.

## Constructed syntax ownership

`NewNode`, `NewReferenceDefinition` and `NewDocument` validate source associations
and snapshot every mutable input layer. Their accessors return independent mutable
slices/maps/property copies and detached Node/ReferenceDefinition handles sharing
private immutable data. Replacing an exported handle cannot mutate a model that
already retained it; traversal callbacks also receive detached handles.
Use these constructors; zero Node/Document/ReferenceDefinition values are not
valid constructed models. No constructor verifies full CommonMark grammar or
claims that a constructed tree was parsed. No file/network/configuration access,
renderer, rule execution or writes occur here.

Node envelopes contain their children, markers and source-backed content/property
segments. Children, marker ranges and content segments retain source order;
equal starts keep supplied order. `Walk` uses pre-order and stops entirely when
the callback returns false. Reference definitions are separate from child traversal.

An envelope may include stripped container prefixes between content segments.
It is not permission to replace a contiguous envelope as logical text. For the
eight bytes represented by `> a\r\n> b`, the envelope is `[0,8)`, content segments
`[2,5)`/`[7,8)` select `a\r\n`/`b`, and quote prefixes occupy `[0,2)`/`[5,7)`.
The model tests construct this representation; Parse does not recognize quotes.

`Segment.Source` always selects original bytes. `Transformation` is one of:

- `Exact`: source-backed identity text; any supplied segment semantic value must
  equal that source slice.
- `Escape`, `Entity`, `Normalized`, `PartialTab`: explicit transformation from
  the source range to an optional semantic contribution; actual decoding/mapping
  is not performed by these model constructors.
- `Synthetic`, `MappingUnavailable`: explicitly synthetic content or unavailable
  mapping. A source anchor is optional; absence never claims an original slice.

`Value` retains ordered raw segments separately from optional whole semantic text.
`Optional.Present` distinguishes unknown/absent from known empty/zero values.
When all segments are Exact, a supplied whole semantic value must match their
original slices in order; differing semantic text needs explicit transformation
or unavailable/synthetic provenance. The same rule applies to node Literal/Content.
For `&amp;`, `[0,5)` selects five source bytes even when semantic text is `&`.
Never infer offsets from semantic lengths or reconstruct Markdown to locate edits.
Scanner helpers now populate partial-tab and NUL mappings; #18/#19 still own
inline recognition and escape/entity decoding.

## Core kind and property catalog

All nodes have Kind, Envelope, ordered Markers, ordered Content segments, typed
Properties and ordered Children. A nil property pointer means absent/inapplicable;
optional scalars have explicit presence. Missing properties do not assert semantic
facts. `Properties.Literal` is optional semantic text, distinct from Content.
Roles in Markers identify source spellings; each marker has its exact source span.

| Kind | Properties / marker roles and content meaning |
| --- | --- |
| `DocumentKind` | Full snapshot envelope; ordered block children. Document owns references, while ParseResult owns availability, phases, failures and uncertainty. |
| `Paragraph` | Logical-content segments and ordered inline children. |
| `Heading` | Heading Level 1–6 and Style `atx`/`setext`; marker roles `opening`, `closing`, `underline` as applicable; content/inline children. |
| `ThematicBreak` | Marker spelling/ranges use `thematic-marker`; envelope preserves intervening spaces. |
| `BlockQuote` | Ordered block children; per-line `quote-prefix` markers. |
| `List`, `ListItem` | List.Ordered; optional Start, Delimiter (`.`/`)`), Bullet (`-`/`+`/`*`), Tight when known. Item `item-marker`/`item-prefix` ranges and ordered block children. |
| `CodeBlock` | Code.Style `indented`/`fenced`; optional Fence byte/Width, raw/semantic Info Value. `opening`, optional `closing`, `indent-prefix` markers; raw Content and semantic Literal. |
| `HTMLBlock`, `InlineHTML` | HTML.BlockForm 1–7 only for block origins; protected raw source Content. |
| `Text` | Original Content segments, semantic Literal and per-segment transformation provenance. |
| `SoftBreak`, `HardBreak` | Original line-ending Content; optional Break.Origin (`line-ending`, `spaces`, `backslash`) and `hard-break-marker` as applicable. |
| `CodeSpan` | Delimiter.Marker/backtick Width; `opening`/`closing` markers, raw Content and separately normalized Literal. |
| `Emphasis`, `Strong` | Delimiter.Marker/Width, `opening`/`closing` ranges and ordered inline children. |
| `Link`, `Image` | Link.Origin `inline`/`full`/`collapsed`/`shortcut`/`autolink`; Label/Destination/optional Title Values, optional NormalizedLabel and explicit Resolution availability. `label-open`, `label-close`, `destination-open`, `destination-close`, `title-open`, `title-close`, `image-marker` roles as applicable; label children. |

Property constructors enforce recognized kinds, property-family applicability,
basic supplied heading/fence/HTML/delimiter/origin values and all source ranges.
They do not recognize syntax, normalize labels or validate complete Markdown
grammar. Unknown facts remain absent rather than fabricated. These meanings are
internal catalog contracts; #139 owns projection/schema adaptation.

## Reference records and completion inventory

`ReferenceDefinition` stores an envelope, ordered Markers, Label/Destination/
optional Title Values and optional NormalizedLabel. Keep all definitions, including
duplicates and unused definitions, in `Document.References().Definitions` source
order. Markers use the label/destination/title roles above and `definition-colon`.
These are not rendered block children. Raw spellings and semantic values remain
separate. #17/#27 later recognize, normalize and resolve them.

Records Availability and effective LookupAvailability are independent: unavailable
data never means a proved-empty registry. Lookup maps already supplied normalized
labels to the first matching definition index. NewDocument validates indices,
first-selection consistency and coverage for an Available lookup, without doing
normalization. Partial lookup may contain only known first selections; unavailable
lookup has no entries. Mutable collection copies cannot change the document.

[`foundation.json`](testdata/commonmark/0.31.2/foundation.json) explicitly lists
all 652 upstream IDs as pending, with zero supported HTML-conformance examples.
Owners #6/#18 supply the pending block/inline pipelines, with individual syntax
providers following their dependency graph. The test-only #5 adapter/runner is
available in `internal/conformance` and `tools/commonmark_conformance.py`. These
are foundational pending scopes, not a claim that each example uses every syntax
feature. This inventory is maintained separately from unmodified upstream data.
`TestFoundationCorpusAccounting` requires all IDs exactly once and confirms that
every actual retained input remains explicitly uncertain. Those checks are not
HTML passes. The runner validates this complete pass/pending ledger and rejects
supported passes becoming fail/error/pending, lost/duplicate identities and newly
passing IDs not yet recorded in its supported baseline. It never edits the ledger
or upstream expectations. Foundation/model tests remain independent; later
syntax deliveries ratchet actual supported examples. Final completion still
requires all examples without skips/exemptions.

Run the Go build/test/vet/race and corpus checks in the contributor guide.
The separate [`scanner.json`](testdata/commonmark/0.31.2/scanner.json) registry
names Tabs examples 1–11 as scanner/indentation targets. Its tests assert exact
identity, section and original input spellings, raw line reconstruction and helper
use, independently of HTML status. All 652 HTML cases retain their current pending
owners. Independent coordinate/line tables, character fixtures and actual helper
provenance tests establish scanner capabilities only. Current checks cannot
establish syntax conformance, reference normalization, full resource enforcement,
fix safety or CLI/MCP parity.
