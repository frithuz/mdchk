# Parser model and staged block contract

`internal/parser` defines an in-memory CommonMark 0.31.2 model and a staged
paragraph/block lifecycle with indented/fenced-code, HTML-block, thematic-break,
ATX, Setext and nested block-quote recognition. It completes documents accounted
for by code with established info semantics, opaque HTML blocks, thematic breaks,
empty ATX headings and ASCII blanks, including empty/inline-free quotes. It retains
paragraph/heading source content for later inline parsing. Other block providers,
reference definitions and inline text remain pending. The model
catalog also supports constructed fixtures; it does not promise recognition of
every listed kind.

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
| ASCII-blank source (space/tab/LF/CR only) | `Complete` / `Available` | Root `[0,N)`, no children, established empty references/lookup, both phases complete |
| Indented/fenced code with known info semantics, HTML blocks, thematic breaks, empty ATX headings and ASCII blanks, including empty/inline-free quotes | `Complete` / `Available` | Root `[0,N)` with source-bound blocks, established empty references/lookup, both phases complete, no uncertainty |
| Other valid source | `Incomplete` / `Partial` | Root `[0,N)` with staged source-bound blocks where recognized, one `SyntaxUnimplemented` uncertainty over `[0,N)`, blocks pending, inlines not started; references/lookup unavailable |
| Invalid UTF-8 | `Failed` / `Unavailable` | Original Source, typed `Failure` with `InvalidUTF8` code and snapshot-bound byte location, nil Document, neither phase started |
| Cancellation/deadline on otherwise valid source | `Incomplete` / `Partial` | Original Source and partial root with any already-closed prefix blocks, affected region with `Cancelled`/`DeadlineExceeded`, separate Interruption, nil Failure; unfinished work interrupted |
| Other operational failure | `Failed` / `Unavailable` | Retained Source, typed stable failure code, nil Document; InternalFailure if block-model construction violates invariants |

Mixed/unsupported input, including extension/frontmatter-looking text, retains
pending interpretation; disabled extensions do not become errors. ASCII-blank input
is affirmatively established to contain no blocks or references. NBSP and other
nonblank Unicode content are never discarded as blank lines.
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
never need to parse messages. `InternalFailure` also reports an invariant failure
while constructing block nodes; valid supported inputs should not reach that path.

For example:

```go
source := parser.NewSource([]byte("hello\n"))
result := parser.Parse(context.Background(), source)
// result.Status == parser.Incomplete; result.Failure == nil.
// result.AST == parser.Partial; root envelope is [0,6).
// result.Uncertainty[0].Reason == parser.SyntaxUnimplemented.
// Root contains one Paragraph with raw content [0,5), but no inline children.
// Its [0,6) envelope retains the terminal newline. Inline semantics are pending.
```

The private block/reference phase precedes inline processing. Inlines cannot run
until the block phase and effective reference lookup are complete. The block pass
establishes an empty registry and a private inline-free handoff only after every
logical line has been recognized as blank or quote prefixes and established
inline-free leaves: indented/fenced code with known info semantics, opaque HTML,
a thematic break or an empty ATX heading. Inline work is then
vacuously complete. Paragraphs, nonempty headings, unknown suffixes, unestablished
info and interrupted work never establish that handoff. Supplying
complete phases/references or constructed break/heading children cannot bypass
the guard; model constructors do not prove recognition.
Both phases share the caller context. Checks occur at phase boundaries and before
each indexed block line and before EOF finalization, not inside UTF-8/index scans or every constructor loop.
#31 retains full work metering, cancellation checkpoints and budget enforcement;
no speculative budget API is added here.

## Staged paragraph lifecycle

A private stack retains the open document, nested quotes and paragraph. Blank
lines close a paragraph; EOF finalizes its last content line. Source-backed segments accumulate
privately and nodes are validated/frozen once on closure. One LineIndex is reused.
The envelope starts after the first line's enclosing quote prefixes (including
remaining leaf indentation) and ends after the last content line's original terminator, or EOF.
Blank separators are excluded. Logical content omits each line's leading ASCII
space/tab indentation, preserves interior endings and trailing spaces, and omits
the last line's ending and paragraph-final spaces/tabs. Omitted bytes remain in
Source. NUL has explicit Normalized provenance; other retained bytes stay Exact.
There is no paragraph Literal or invented inline child. Paragraph indentation is
fully consumed, so no residual partial tab is emitted into its logical content;
internal tabs stay literal. Indented starts of four columns (including a tab
reaching that boundary) open code only without an existing paragraph; deep
indentation continues an open paragraph. Diagnostic code-point columns are not
used for that distinction.

Existing quote containers match before active HTML/fences consume opaque/literal
payload and ordinary leaf dispatch. A quote can also start after indented code
dedents. Valid fence
openers close preceding paragraphs; invalid backtick/tilde candidates follow
ordinary paragraph handling unless another unavailable provider applies. After
fence, HTML, Setext, thematic and ATX dispatch, start hooks precede paragraph
continuation. A conservative hook defers lines below four columns whose first
nonblank byte is a digit or one of `*`, `_`, `+` or `[`, or a possible
dash list marker (a dash followed by ASCII space/tab or end of line). Failed equals/dash
underlines otherwise take the ordinary paragraph path. This is not a grammar
recognizer: ordinary punctuation/numeric lines may also be deferred. A
deferred line might start an unknown container or other block, so its open
paragraph is discarded, previously closed blocks are retained, and the remaining
suffix stays source-only. No boundary inside that
suffix is invented. Mixed/unsupported results retain whole-document
uncertainty, including retained paragraphs; these nodes are progress for later
parsing, not reliable permission for syntax-dependent rules or fixes. Full
providers will refine these guards.

`TestParagraphUpstreamTargets` consumes unmodified examples 219–227 with
independent expected grouping, byte envelopes and content segments. Examples
219–224 establish paragraph progress; 226 retains interior hard-break spaces;
225 retains its code and following paragraph with independent source expectations.
Example 227 now retains both its
paragraph and ATX heading, with independent source expectations shared with
the ATX targets. All nine remain HTML-pending for their later providers.
Focused Parse fixtures cover blank/EOF closure, LF/CRLF/CR, Unicode/NUL/tab
provenance, immutable ownership and repeatability. Private transition tests
cover hook precedence and interrupted open work.

## Thematic breaks and precedence

Below four structural indentation columns, a line containing at least three
matching `*`, `-` or `_` bytes and only ASCII spaces/tabs between or after them is
a thematic break. Leading tabs reach the four-column boundary and do not qualify.
Validate the full original line before allocating marker metadata. Each maximal
contiguous marker run has a `thematic-marker` span excluding whitespace; the node
envelope includes the entire physical line, original indentation, inter-marker and
trailing whitespace, and LF/CRLF/CR ending or unterminated EOF. Breaks contain no
inline children or semantic payload. Original bytes remain unchanged.

Thematic recognition precedes generic list-start deferral and paragraph
continuation. It closes a preceding paragraph and permits a following one without
blank separators. A qualifying contiguous dash underline after an open paragraph
promotes that paragraph to Setext before thematic dispatch. Spaced dashes cannot
be a Setext underline and may interrupt the paragraph as a break. Deeply indented
continuation stays paragraph content. Unsupported container starts still defer
their whole suffix rather than manufacturing nested breaks.

`TestThematicUpstreamTargets` checks original examples 43–61 plus newly supported
11/98 with independent node order, envelopes and exact marker ranges. Actual
Parse/Evaluate reproduces HTML for 11, 43, 47, 50–54 and 98. Example 58 retains
paragraph/break/paragraph progress but remains inline/reference-pending. Example
59 retains a Setext heading and following paragraph; 48 now passes as literal
indented code, and 57/60/61 await lists/containers.
Negative cases never acquire break nodes or false completeness. Independent
fixtures cover tabs, indentation boundaries, Unicode/NUL/extra characters,
endings/EOF, blank-separated breaks, ownership, repeatability and interruption.

## ATX headings and inline handoff

Below four structural indentation columns, one to six opening hashes followed
by ASCII space/tab or end of line form an ATX heading. A valid heading closes and
retains an open paragraph; scanning resumes after the heading. Failed hash-start
candidates follow ordinary paragraph handling when no other provider applies;
there is no blanket hash-start deferral. Four-column starts open code without an
existing paragraph,
and deeply indented lines can continue paragraphs. Unsupported containers and
other unsupported starts keep their conservative boundaries.

Heading Level and Style (`atx`) use the existing property catalog. The envelope
retains the complete original physical line, indentation and LF/CRLF/CR ending or
EOF. The `opening` marker selects just the initial hash run. An optional `closing`
marker selects a terminal hash run preceded by ASCII space/tab and followed only
by ASCII space/tab. Escaped/nonclosing hash sequences remain raw content for #19;
block classification does not perform semantic escape decoding. Missing closers
are absent, not synthetic zero-width markers. Leading/trailing ASCII space/tab
and valid closing runs are excluded from logical content but remain in Source.
Unicode whitespace is not trimmed. Content has original-byte Exact segments,
with explicit Normalized provenance only for semantic NUL replacement. No inline
children or semantic Literal are invented.

Empty content established by recognition can join supported breaks/blanks in a
complete document; nonempty headings retain conservative pending block/reference
and inline handoff, whole-document uncertainty and adapter-pending outcomes.
Empty children alone never prove empty content or completion. Actual
Parse/Evaluate renders unchanged example 79, including its empty heading with a
closer. All 18 original examples 62–79 have independent actual Parse assertions:
levels, invalid openers, escapes, indentation, closing classification, thematic
ordering, paragraph interruption and empty content. Additional fixtures cover
original endings/EOF, tabs, non-ASCII whitespace, Unicode/NUL, mixed completion,
unsupported suffixes, ownership and repeatability. Phase fixtures reject
constructed empty-heading handoffs and retain closed headings on interruption.

## Setext headings and promotion

Below four structural indentation columns, a nonempty contiguous equals/dash
run with only trailing ASCII space/tab promotes the whole eligible open
paragraph to Heading level 1/2, style `setext`. Single markers qualify. This
dispatch precedes thematic/list handling; without a paragraph, equals lines are
ordinary text and standalone breaks keep thematic precedence. Malformed
underlines continue paragraphs when no other provider applies; four-column
underline-like continuation stays text. Blank lines prevent promotion across
their boundary. Genuine unresolved list/HTML/container starts still defer; fences have their own
recognition and continuation before ordinary dispatch.

The `underline` marker selects only the contiguous run. The envelope spans the
first content line's physical start through the underline's ending or EOF.
Promotion acquires the delayed final paragraph contribution once and freezes
one node, then resumes scanning. Content uses the paragraph's original-byte
segments and ASCII trimming: interior endings/spaces remain; final content-line
spaces/tabs, its ending and the underline are omitted. A final backslash remains
raw content without a fabricated hard break. Omitted bytes remain in Source.
Unicode/NUL provenance, snapshot ownership and LF/CRLF/CR/EOF remain unchanged.

Setext cannot be empty. These headings remain Incomplete/Partial, blocks pending,
inlines not started, references/lookup unavailable, with whole-document syntax
uncertainty and pending adapter output. No inline children or HTML are invented;
empty-ATX completion does not apply. Existing complete input, strict UTF-8 and
interruption guards remain intact.

`TestSetextUpstreamTargets` exercises all original 80–106 and thematic 59 with
independent actual-Parse node order, level/style, envelope, content and marker
ranges. Nineteen Setext-section cases establish structural/fallback progress;
98 stays a supported pair of breaks; 85/100 now pass with indented code and
following breaks. Quotes 92/101 now contain paragraphs followed by outside
thematic breaks; 93 retains lazy paragraph text without Setext promotion. Only
list cases 94/99 (#14–#16) remain entirely source-only. Example 91 retains its initial
heading and paragraph after invalid fence classification; its invalid multiline
complete-tag suffix now also establishes Setext/paragraph structure. Inline
reference/text/code-span/HTML classification remains pending (#17/#18/#20/#23).
Source-only deferral is not heading recognition. Focused fixtures cover single
markers, negative/mixed/extra characters, tabs/indentation, blank separation,
following blocks, final whitespace/backslash, Unicode/NUL, original endings,
immutable accessors and interruption before/after promotion.

## Block quotes and container handoff

The lifecycle matches existing quote prefixes before continuing any leaf. Each
prefix permits up to three structural indentation columns, `>` and one optional
space/tab column. The cursor carries original tab-stop phase through nested
prefixes and code indentation. New quotes can interrupt paragraphs. Closed
children attach to their actual parent, and outside lines resume normal dispatch
after unmatched containers close. An unmarked blank separates quotes; a marked
blank can close a paragraph while retaining its quote. Consecutive marked lines
remain one quote.

Only existing paragraph continuation can omit outer or nested markers. Lazy
lines add no synthetic prefix markers. A markerless equals underline remains
paragraph text; a thematic break ends the quote, whereas a marked eligible
Setext underline promotes its paragraph. Code and HTML never borrow paragraph
laziness. A quote boundary closes otherwise unclosed code/HTML before outside
content is processed. Remaining `>` bytes inside active leaf payload stay opaque.

BlockQuote has ordered children, an original-byte envelope from the start of its
first prefix through its last owned line, and ordered per-line `quote-prefix`
markers. A nested quote starts after its enclosing prefixes. Leaf envelopes start
after prefixes too, but may span later omitted prefix bytes; only Content segments
represent logical text. HTML segments exclude all enclosing prefixes. A delimiter
splitting a tab shares that original byte with residual PartialTab content; code
Literal normalizes endings/NUL independently of its exact source spans. Original
Source identity and immutable accessor ownership remain unchanged.

Empty quotes and quotes containing only established inline-free leaves complete
with known-empty references and vacuous inlines. Text, nonempty headings, unknown
info and unavailable list/reference/inline providers remain partial. Interruption
discards unfinished nested containers rather than finalizing them as normal EOF;
already-closed root children survive. Strict encoding failure still takes precedence.
Full depth/node/work limits and cancellation hardening remain #31.

`TestQuoteUpstreamTargets` asserts actual trees, child order, envelopes, markers and
content ranges for all unchanged 228–252 independently of HTML. Cases 236/239/240
newly pass HTML; existing literal-code case 231 remains supported. List case 235
retains #14–#18. Other quote cases retain #17/#18, plus #21 for multiline text.
`TestQuotePartialTabUpstream6` independently verifies the newly supported tab/code
case. Earlier Setext 92/93/101, fence 128 and HTML 174 now assert quote structure;
214/218 remain pending for reference/text/resolution providers #17/#18/#27.
Boundary fixtures cover all seven HTML forms, prefix-free payload, marked Setext,
code transitions, partial tabs, Unicode/NUL/endings/EOF, immutable ownership and
deterministic interruption with closed-prefix retention. These are staged
recognition assertions, not general inline or full CommonMark conformance.

## HTML blocks and opaque source handoff

The parser recognizes all seven CommonMark 0.31.2 HTML-block forms. It classifies
starts below four indentation columns using the pinned ASCII tag-name/attribute
grammar. Forms 1–6 can interrupt a paragraph; form 7 requires a complete tag alone
on its physical line and cannot interrupt. Invalid starts take ordinary paragraph
handling. Fenced/indented code keeps precedence after quote matching; unavailable
list providers retain their genuine boundaries. No browser HTML parser or sanitizer is involved.

One private fixed-form state consumes each physical line before normal dispatch.
Forms 1–5 close on their specified delimiter and include the entire terminating
line, even trailing Markdown-looking bytes. Form 1 accepts any of its four closing
tags, independent of its opener. Forms 6–7 end before a blank line; apparent nested
HTML cannot change the active form. Normal unclosed EOF is valid. Container-end
handling closes the active HTML block before processing outside content. Interrupted unfinished HTML is
discarded, while previously closed prefix blocks remain available.

HTMLBlock stores HTML.BlockForm 1–7 and an original-byte envelope covering all
owned lines after the first prefix. Logical Content is acquired once per line,
coalescing adjacent Exact ranges without copying earlier payload, then frozen by
NewNode. Top-level content remains contiguous except NUL normalization; quoted
content omits prefixes and can retain residual leading PartialTab spaces. Original
LF/CRLF/CR and internal tabs remain raw, with no synthetic source ending. The
test-only serializer accepts only Exact, validated NUL normalization and validated
leading residual-tab contributions for block HTML; it preserves logical raw
spelling except mandatory NUL replacement and its final LF when absent. Source
bytes and their ownership never change. Payload stays opaque: no inline children,
reference definitions, directive interpretation, execution or resource acquisition.

HTML-only documents and combinations of established inline-free blocks can complete
with available empty references and vacuous inline phases. Surrounding unsupported
syntax retains whole-document uncertainty and pending adapter output. Constructed
HTML nodes cannot manufacture the private recognition/handoff proof.

TestHTMLUpstreamTargets independently asserts form, node order, physical envelopes
and content ranges for all unchanged 148–191 inputs plus cross-feature passes 21/31.
Twenty-five HTML-section cases now pass actual Parse/Evaluate. Nineteen remain
pending: 174 retains its quote and outside paragraph (#17/#18), 175 needs lists
(#14–#16), and other mixed cases
need reference/text (#17/#18), breaks (#21), inline HTML (#23) or emphasis
(#24/#25) as recorded in foundation.json. These are coverage owners, not reverse
blockers on this syntax stage. Separate fixtures cover grammar near misses,
case/EOF/fixed-form boundaries, code and paragraph precedence, opaque payload,
LF/CRLF/CR/NUL/Unicode/tab provenance, immutable accessors, encoding precedence and
deterministic interruption, including at EOF finalization. Actual full-corpus
normalized/strict gates record 84 pass and 568 pending, preserving all 80
pre-quote passes; full conformance remains false.

## Indented code and literal handoff

Four structural indentation columns open code only when no paragraph is open.
Code continuation precedes heading/thematic dispatch; Markdown-looking content
at code indentation remains literal. A nonblank line below four columns closes
code and is reprocessed normally. Interior blank runs are acquired only when
another code line follows; leading/trailing blanks are excluded. Unsupported
container starts still defer their entire suffix rather than exposing nested
indentation as reliable top-level code.

The code envelope starts at the first nonblank code line's physical start and
ends after the last nonblank code line's original terminator or EOF. Each nonempty
removed indentation prefix has an `indent-prefix` marker selecting its original
bytes. Content retains the remainder and each included ending in source order;
interior blanks remove up to four columns, retaining any excess. Internal tabs,
extra indentation and trailing spaces/tabs remain literal. Four-column removal
from column zero ends at a tab stop; it does not expand later literal tabs.
The existing cursor supports partial-tab provenance when a consumer splits a tab.

Code.Style is `indented`; fence/width/info and inline children are absent. Literal
concatenates semantic contributions once at closure. Unchanged contributions and
LF endings are Exact; NUL and CR/CRLF endings use Normalized contributions (U+FFFD
and LF respectively). An unterminated nonblank EOF appends a Synthetic LF anchored
at the present zero-width EOF span. No semantic length supplies an original offset,
and normalization never rewrites Source. Raw content and markers omit trailing
blanks while Source and the document envelope retain them.

Recognized code can join blanks, breaks and empty ATX headings in a complete,
reference-free/inline-free document. Reference-looking content inside code cannot
define references. Nonempty paragraphs/headings, unknown suffixes and interrupted
open code do not acquire that handoff. An interruption discards the unfinished
code builder and its deferred blanks while retaining already-closed prefix nodes.
Full work metering/checkpoint hardening remains #31.

`TestIndentedUpstreamTargets` reads unchanged 107–118 and independently asserts
node ordering, raw envelopes/content/indentation ranges and semantic values.
107, 110–112 and 116–118 match actual Parse/Evaluate HTML; list cases 108/109
remain source-only, while 113–115 retain available structure and pending inlines.
Cross-feature tests now retain actual code in 48, 69, 85, 100 and 225. Additional
upstream fixtures verify newly rendered 1–3, 8, 18, 36, 134, 231 and 289 without
claiming their named section's syntax providers are generally implemented.
Focused fixtures cover literal syntax, tabs/whitespace, blank ownership, dedent,
original/normalized endings and EOF, Unicode/NUL, immutable source/accessors,
repeatability, encoding precedence and complete/pending/interrupted guards.

## Fenced code and info availability

Backtick/tilde runs of at least three bytes below four structural indentation
columns open a fenced CodeBlock. Backtick info cannot contain a backtick;
invalid candidates take ordinary paragraph handling where no other provider
applies. Valid openers close and retain an existing paragraph. Active-fence
continuation precedes other dispatch: payload remains literal regardless of
reference, HTML, heading, list, entity or directive-looking text. A closer has
the same character, at least the opening width, fewer than four leading columns
and only trailing ASCII spaces/tabs. Failed closers stay payload; a valid closer
resumes normal dispatch. Four-column starts retain indented-code semantics.

Code.Style is `fenced`; Fence/Width describe the opening token. Exact `opening`
and optional `closing` markers omit indentation, info, trailing whitespace and
endings. The envelope spans the opener's physical start through the closer's
ending/EOF, or normal document EOF when unclosed. Missing closers are absent,
not synthetic zero-width markers. Opener-only and empty closed fences have a
known empty Literal and no payload segments. Unclosed fences are valid syntax;
a quote boundary also closes an otherwise unclosed fence.

Each payload line removes up to the opening indentation using the existing
structural cursor; excess whitespace, internal tabs and blank lines remain
literal. `indent-prefix` selects removed original bytes. When removal splits a
tab, that marker and the residual PartialTab segment share the tab's original
byte; neither is an independent contiguous edit instruction. NUL and physical
CR/CRLF normalize semantically with original provenance. An unterminated payload
line gains a Synthetic LF anchored at EOF. Source never changes. One private
builder accumulates source-backed segments and assembles Literal/finalizes once.

Info retains ASCII-trimmed raw source segments separately from optional semantic
text; empty info is known empty. Ordinary info and NUL normalization are available.
ASCII punctuation escapes and syntactically plausible semicolon-terminated
named/numeric references retain a MappingUnavailable raw segment and no semantic
value pending #19, including names whose recognition is not yet established.
Literal backslashes and ampersands without such candidates remain available.
No escape/entity decoder or fabricated raw-as-decoded value is supplied here.
Unestablished info keeps blocks/whole-document parsing incomplete; the adapter
reports pending rather than guessing a language. Supported fence-only documents
can complete empty references and vacuous inline work; mixed unknown syntax stays
partial. Cancellation/deadline discards an unfinished fence, including at EOF
finalization, while preserving closed prefix blocks and separate interruption.
Full metering/checkpoint hardening remains #31.

`TestFencedUpstreamTargets` independently asserts actual Parse kinds/order,
opening/closing/indentation spans, physical envelopes, payload and info for every
unchanged 119–147 input plus newly passing cross-feature 19. Twenty-three fenced
section cases pass actual Parse/Evaluate, including existing indented-code 134.
128 retains its quote-contained fence and outside paragraph (#17/#18);
121 needs references/inline/code spans
(#17/#18/#20), 138/145 additionally soft breaks (#21), and 140/141 retain actual
fence/paragraph/heading structure with surrounding reference/inline work pending.
Extra fixtures cover empty/unclosed/blank/EOF, malformed openers/closers, literal
payload and test-renderer escaping, semantic-info availability, original
LF/CRLF/CR, synthetic LF, tabs/NUL/Unicode, ownership and interrupted prefixes.

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
Model fixtures and actual quote Parse tests independently verify this representation.

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

[`foundation.json`](testdata/commonmark/0.31.2/foundation.json) explicitly
accounts for all 652 upstream IDs: 84 supported HTML cases and 568 pending.
Supported IDs are 1–3, 6, 8, 11, 18–19, 21, 31, 36, 43, 47–48, 50–54, 69, 79,
85, 98, 100, 107, 110–112, 116–120, 122–127, 129–137, 139, 142–144, 146–147,
150–151, 153–154, 156–166, 171, 173, 178, 181, 183–184, 186, 189–191, 231,
236, 239–240 and 289.
These are recognized inline-free documents, not implementation of every syntax feature in their named sections.
Indented-code 108/109 retain list/container/reference/inline owners #14–#18;
113 retains #17/#18/#21, and 114/115 retain #17/#18. Paragraph 225 now retains
code and its following paragraph, with only #17/#18 pending. Thematic cases
retain list/container (#14–#16), reference/inline (#17/#18), break (#21) and
emphasis (#24/#25) owners where relevant. Setext targets retain #17/#18 for
92/101 and additionally #21 for 93, #14–#16 for 94/99 and #17/#18/#20/#23
for 91. Recognized Setext/fallback cases and 59 retain #17/#18, with #21 for multiline content, #19 for escapes
in 102/106 and #24/#25 for emphasis in 80–82. ATX cases name #17/#18 for
text/reference handoff, #19 for escapes in 65/66/76, #24/#25 for emphasis in 66
and #21 for multiline content in 70. Paragraph cases retain #17/#18 and #21
where multiline/break processing is required. The fenced-code section retains
#17/#18 for 128, #17/#18/#20 for 121, additionally #21 for 138/145, and
#17/#18 for 140/141. Info decoding remains #19. Other examples use broad remaining
inline/core-completion scopes #18/#29. These are coverage owners, not reverse
native prerequisites on staged syntax deliveries. The test-only #5 adapter/runner
is available in `internal/conformance` and `tools/commonmark_conformance.py`.
These are foundational pending scopes, not a claim that each example uses
every syntax feature. This inventory is maintained separately from unmodified
upstream data. `TestCoreCorpusAccounting` requires all IDs exactly once and
confirms that actual retained inputs agree with supported versus uncertain
outcomes. The runner independently compares actual HTML, validates this
complete ledger and rejects supported passes becoming fail/error/pending,
lost/duplicate identities and newly passing IDs not yet recorded in its
supported baseline. It never edits the ledger or upstream expectations.
Foundation/model tests remain independent; later syntax deliveries ratchet
actual supported examples. Final completion still requires all examples
without skips/exemptions.

Run the Go build/test/vet/race and corpus checks in the contributor guide.
The separate [`scanner.json`](testdata/commonmark/0.31.2/scanner.json) registry
names Tabs examples 1–11 as scanner/indentation targets. Its tests assert exact
identity, section and original input spellings, raw line reconstruction and helper
use, independently of HTML status. Example 11's HTML pass comes from thematic
recognition, not scanner helper checks. Independent coordinate/line tables,
character fixtures and actual helper provenance tests establish scanner
capabilities only. Current checks cannot establish syntax conformance,
reference normalization, full resource enforcement,
fix safety or CLI/MCP parity.
