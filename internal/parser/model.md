# Parser model and staged block contract

`internal/parser` defines an in-memory CommonMark 0.31.2 model and a staged
paragraph/block lifecycle with indented/fenced-code, HTML-block, thematic-break,
ATX, Setext, mixed block-quote/list-item recognition and continuation. It completes documents accounted
for by code with established info semantics, opaque HTML blocks, thematic breaks,
empty ATX headings and ASCII blanks, including empty/inline-free quotes and lists. It retains
paragraph/heading source content and constructs source-preserving Text/CodeSpan/break/autolink children. ListItems retain
first-line metadata, nested block children, continuation and closure. Compatible
siblings acquire List parents and independently derived tightness.
Leading reference definitions are recognized and normalized document-wide; inline
text, code spans, breaks and angle autolinks are recognized; reference-use resolution remains pending. The model
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
| Indented/fenced code with known info semantics, HTML blocks, thematic breaks, empty ATX headings and ASCII blanks, including quotes and lists of empty items or established inline-free blocks, nested in either kind of container | `Complete` / `Available` | Root `[0,N)` with source-bound blocks, established empty references/lookup, both phases complete, no uncertainty |
| Supported Text/CodeSpan/break/autolink core blocks | `Complete` / `Available` | Text/CodeSpan preserve raw contributions and semantic Literal; breaks retain origin, original ending and hard-break markers; autolinks retain resolved label/destination provenance; both phases complete |
| Text needing undelivered inline handlers | `Incomplete` / `Partial` | Source-bound blocks and available reference records/lookup; Blocks complete, Inlines pending; whole-document `SyntaxUnimplemented` uncertainty |
| Definition-only input with established destination/title semantics (including inside otherwise inline-free containers) | `Complete` / `Available` | All records retained, first-definition lookup available; no definition block children; both phases complete |
| Definition destination/title or fence info contains escapes/entities, with no other missing syntax | `Complete` / `Available` | Known decoded values and explicit raw transformations, available reference lookup and complete phases |
| Invalid UTF-8 | `Failed` / `Unavailable` | Original Source, typed `Failure` with `InvalidUTF8` code and snapshot-bound byte location, nil Document, neither phase started |
| Cancellation/deadline on otherwise valid source | `Incomplete` / `Partial` | Original Source and partial root with any already-closed prefix blocks, affected region with `Cancelled`/`DeadlineExceeded`, separate Interruption, nil Failure; unfinished work interrupted |
| Other operational failure | `Failed` / `Unavailable` | Retained Source, typed stable failure code, nil Document; InternalFailure if block-model construction violates invariants |

Input needing undelivered core handlers retains pending interpretation; disabled
extensions/frontmatter follow ordinary core syntax and do not become errors. ASCII-blank input
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
// result.Status == parser.Complete; result.Failure == nil.
// result.AST == parser.Available; root envelope is [0,6).
// Both phases are complete; result.Uncertainty is empty.
// Root contains one Paragraph with a Text child selecting original [0,5).
// Text Literal is "hello"; the paragraph envelope retains the terminal newline.
```

The private block/reference phase precedes inline processing. Inlines cannot run
until the block phase and effective reference lookup are complete. The block pass
publishes the complete source-ordered registry and first-selection lookup only
following a successful whole-document scan/finalization. Ordinary paragraphs and nonempty headings finish their block structure before
inline scanning. Supported text completes; missing inline handlers keep Inlines
pending and whole-document uncertainty. Definition destination/title and fence-info
semantics are now supplied by the shared decoder; interruption still prevents a
completed block/inline handoff. A finished definition-only/inline-free document with
known semantics has both phases complete. Constructed nodes or supplied registry
data cannot establish the private block-recognition handoff.

Both phases share the caller context. Checks occur at phase boundaries, before
indexed block lines/EOF, during reference recognition (bounded labels and long
components), and during bounded inline scanning. Interrupted collection invalidates global registry availability and
discards unfinished containers rather than finalizing them as normal EOF. Already
closed root prefixes survive. Strict UTF-8 failure still precedes interruption.
#31 retains full work metering, index/UTF-8 scan checkpoints and budget enforcement.

## Staged paragraph lifecycle

A private stack retains the open document, nested quotes and paragraph. Blank
lines close a paragraph; EOF finalizes its last content line. Source-backed segments accumulate
privately and nodes are validated/frozen once on closure. One LineIndex is reused.
The envelope starts after the first line's enclosing container prefixes (including
remaining leaf indentation) and ends after the last content line's original terminator, or EOF.
Blank separators are excluded. Logical content omits each line's leading ASCII
space/tab indentation, preserves interior endings and trailing spaces, and omits
the last line's ending and paragraph-final spaces/tabs. Omitted bytes remain in
Source. NUL has explicit Normalized provenance; other retained bytes stay Exact.
The block pass supplies no paragraph Literal; the inline pass constructs Text children. Paragraph indentation is
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
fence, HTML, Setext, thematic and ATX dispatch, grammar-aware list classification
precedes the remaining start hooks and paragraph continuation. Invalid list
candidates, including ordinary punctuation/numeric text, follow the paragraph path.
Reference-looking starts follow the paragraph path. Closure removes only a valid
leading sequence of definitions; invalid candidates and ordinary remainders stay
source-preserving paragraph content. Definitions never interrupt existing prose,
including lazy quote/item prose. A definition-only paragraph cannot acquire lazy
outside text. Setext promotion extracts leading definitions before deciding whether
heading text remains; otherwise the underline is reprocessed normally. The former
conservative reference guards in ordinary dispatch and quote interruption are gone.
Pending inline interpretation still retains whole-document uncertainty and does
not authorize syntax-dependent rules or corrections.

Container laziness applies to paragraph text, never missing definition
components. At the first unmatched prefix, a read-only recognition probe settles
the leading candidate against following logical lines up to the next real block
boundary. It neither changes container builders nor publishes records. A title
scan retains its offset, delimiter and fallback state as lines arrive, avoiding
repeated scans of a growing title. An invalid/unfinished candidate that remains
ordinary prose keeps genuine paragraph laziness. A definition that would need an
unmatched prefix instead closes the affected container before that line; matched
outer containers are preserved. Accumulated lines also retain lazy provenance,
and extraction independently refuses any definition spanning a lazy line.

`TestReferenceComponentsRequireContainerPrefixes` checks incomplete labels,
destinations and titles in quotes/items, fully prefixed counterparts, nested partial
container matches, exact paragraph ownership/source and pending versus complete
outcomes. Optional next-line-title fallback and genuine lazy prose have separate
fixtures. Deterministic cancellation sweeps include lookahead and resumed titles.
`TestReferenceLazyTitleWorkIsLinear` bounds context-check growth when inputs double;
the accompanying benchmark covers 1k/2k/4k/8k closed/unfinished missing-prefix titles
and fully prefixed controls without a machine-specific latency threshold.

`TestParagraphUpstreamTargets` consumes unmodified examples 219–227 with
independent expected grouping, byte envelopes and content segments. Examples
219–224 establish paragraph progress; 226 retains interior hard-break spaces;
225 retains its code and following paragraph with independent source expectations.
Example 227 now retains both its
paragraph and ATX heading, with independent source expectations shared with
the ATX targets. All paragraph cases 219–227 now pass HTML, including multiline breaks.
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

Thematic recognition precedes list-marker recognition and paragraph
continuation. It closes a preceding paragraph and permits a following one without
blank separators. A qualifying contiguous dash underline after an open paragraph
promotes that paragraph to Setext before thematic dispatch. Spaced dashes cannot
be a Setext underline and may interrupt the paragraph as a break. Deeply indented
continuation stays paragraph content. Container starts use their established
grammar rather than manufacturing nested breaks.

`TestThematicUpstreamTargets` checks original examples 43–61 plus newly supported
11/98 with independent node order, envelopes and exact marker ranges. Actual
Parse/Evaluate now also renders text-bearing 44–46, 49 and 57–61. Case 55
remains conservatively pending for delimiter classification and 56
needs emphasis (#24/#25). Code/break-only passes remain intact.
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
by ASCII space/tab. Escaped/nonclosing hash sequences remain raw block content;
the inline decoder interprets eligible escapes after classification. Block
classification does not perform semantic escape decoding. Missing closers
are absent, not synthetic zero-width markers. Leading/trailing ASCII space/tab
and valid closing runs are excluded from logical content but remain in Source.
Unicode whitespace is not trimmed. Content has original-byte Exact segments,
with explicit Normalized provenance only for semantic NUL replacement. No inline
children or semantic Literal are invented.

Empty content established by recognition can join supported breaks/blanks in a
complete document; supported nonempty headings now complete with Text children.
Missing handlers retain whole-document uncertainty and adapter-pending outcomes.
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
their boundary. Leading definitions are removed before promotion, and recognized
items dispatch nested blocks and continuation. Fences and HTML have their own
recognition and continuation before ordinary dispatch.

The `underline` marker selects only the contiguous run. The envelope spans the
first content line's physical start through the underline's ending or EOF.
Promotion acquires the delayed final paragraph contribution once and freezes
one node, then resumes scanning. Content uses the paragraph's original-byte
segments and ASCII trimming: interior endings/spaces remain; final content-line
spaces/tabs, its ending and the underline are omitted. A final backslash remains
raw content without a fabricated hard break. Omitted bytes remain in Source.
Unicode/NUL provenance, snapshot ownership and LF/CRLF/CR/EOF remain unchanged.

Setext cannot be empty. Supported text-bearing headings complete with Text
children; multiline content or other missing providers retain Incomplete/Partial
and whole-document uncertainty. Strict UTF-8, recognition and interruption guards
remain intact.

`TestSetextUpstreamTargets` exercises all original 80–106 and thematic 59 with
independent actual-Parse node order, level/style, envelope, content and marker
ranges. Nineteen Setext-section cases establish structural/fallback progress;
98 stays a supported pair of breaks; 85/100 now pass with indented code and
following breaks. Quotes 92/101 now contain paragraphs followed by outside
thematic breaks; 93 retains lazy paragraph text without Setext promotion. List cases 94/99 now render their text and outside breaks. Example 91's initial
heading/paragraph and invalid multiline-tag suffix have established ordinary
Setext/paragraph text and now pass; no code span or inline HTML is recognized.
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
with available references and vacuous inlines, including definition-only quotes
with known semantics. Supported text/nonempty headings now complete; undelivered handlers retain
pending inlines, while unknown
required info/definition semantics keep block work pending. Interruption
discards unfinished nested containers rather than finalizing them as normal EOF;
already-closed root children survive. Strict encoding failure still takes precedence.
Full depth/node/work limits and cancellation hardening remain #31.

`TestQuoteUpstreamTargets` asserts actual trees, child order, envelopes, markers and
content ranges for all unchanged 228–252 independently of HTML. Cases 236/239/240
newly pass HTML; existing literal-code case 231 remains supported. List case 235 now renders its quoted item and outside sibling. Other supported
single-line and multiline quote paragraphs/headings also render when their other
inline providers are available. `TestQuotePartialTabUpstream6` independently verifies original tab/code
provenance. Reference cases 214/218 remain pending for #27.
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
Thirty-five HTML-section cases now pass actual Parse/Evaluate, including text
around opaque blocks. Remaining mixed cases need inline HTML (#23)
or emphasis (#24/#25), as recorded in foundation.json. Coverage owners are not
reverse blockers on this syntax stage. Separate fixtures cover grammar near misses,
case/EOF/fixed-form boundaries, code and paragraph precedence, opaque payload,
LF/CRLF/CR/NUL/Unicode/tab provenance, immutable accessors, encoding precedence and
deterministic interruption, including at EOF finalization. Actual full-corpus
normalized/strict gates now record 355 pass and 297 pending, preserving all 331
passes established before autolinks; full conformance remains false.

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
now render their actual item paragraphs/nested items with Text; 114/115 also
render, including multiline 113 with its soft break.
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

Info retains ASCII-trimmed raw source segments separately from known semantic
text; empty info is known empty. The shared decoder supplies NUL normalization,
ASCII punctuation escapes and strict named/numeric references with explicit
Escape/Entity source contributions. Unknown/malformed references stay literal;
generated characters are never decoded a second time. Payload remains literal.
Supported fence-only documents complete empty references and vacuous inline work;
mixed input needing another syntax provider stays
partial. Cancellation/deadline discards an unfinished fence, including at EOF
finalization, while preserving closed prefix blocks and separate interruption.
Full metering/checkpoint hardening remains #31.

`TestFencedUpstreamTargets` independently asserts actual Parse kinds/order,
opening/closing/indentation spans, physical envelopes, payload and info for every
unchanged 119–147 input plus newly passing cross-feature 19. All 29 fenced
section cases pass actual Parse/Evaluate, including surrounding supported text.
Cases 121, 138 and 145 now pass through code-span and external-break recognition.
Info decoding is available.
Extra fixtures cover empty/unclosed/blank/EOF, malformed openers/closers, literal
payload and test-renderer escaping, semantic-info availability, original
LF/CRLF/CR, synthetic LF, tabs/NUL/Unicode, ownership and interrupted prefixes.

## List markers, continuation and staged grouping

List classification recognizes `-`, `+`, `*`, or one to nine ASCII digits followed
by `.`/`)`, with a following ASCII space/tab or end of line. Zero and leading-zero
ordinals are valid; signed, Unicode-digit and ten-digit candidates are ordinary
paragraph text. Up to three leading structural columns are permitted. An ordered
item interrupts a paragraph only at numeric start 1, and no empty item interrupts
a paragraph. Normal dispatch and quoted lazy-paragraph interruption use the same
classifier. Setext/thematic precedence and code/HTML opacity remain intact; quoted
example 238 still has lazy text rather than an item.

A recognized start opens a `ListItem` in its actual document, quote or item
parent. The item retains ordered block children, not uninterpreted raw Content.
The first payload is dispatched recursively, so same-line nested items, quotes,
headings and code are recognized without a stripped copy of Source. Subsequent
lines match the item's effective ContentIndent before leaf continuation, retaining
physical tab phase through mixed containers. Paragraph-only lazy lines may omit
some or all prefixes. Dedents/interruptions close containers before outside
blocks or siblings are dispatched; compatible empty/ordered siblings can end an
item paragraph without imposing the new-list paragraph-interruption restriction.
An item with a blank opening line can accept its next indented nonblank line, but
a second blank closes an otherwise empty item.

The envelope starts before the item's first-line indentation and ends at its last
owned line, including owned trailing blanks. Leaves start after their enclosing
prefixes; their segmented Content excludes later matched prefixes and trims only
paragraph whitespace as documented. `item-prefix`/`item-marker` preserve first-line
facts. Each matched nonempty indentation removal has an `item-indent` source span,
including a shared tab byte where removal splits it. Every matched blank has an
`item-blank` span from that line's cursor before item indentation through its
original ending (or EOF). Both markers can start at the same byte. Lazy lines have
no invented indentation marker. These markers and children preserve downstream
boundary evidence; blanks inside a code/quote child do not by themselves decide
list tightness. The initially blank marker line remains identifiable from the
first-line prefix, original line ending and first child position.

Parent closure groups compatible sibling items once, recursively within document,
quote and item parents. Bullet changes, ordered-delimiter changes, type changes and
intervening blocks split groups. Later ordered numbers do not change the List's
initial numeric Start; every item retains its original ordinal spelling. Ordered
siblings with a changed delimiter can end an ordered item even when the new number
is not 1; the new-list interruption restriction still protects ordinary prose.

A List envelope runs from the first item's start through the last item's end,
including separating gaps and owned trailing blanks. It contains only ListItems,
has empty Content/Markers, and carries Ordered, Bullet or Delimiter, initial Start
for ordered lists, and Tight when established. Item-only source/indent fields never
move to List. Item/leaf envelopes and source segments are unchanged by wrapping;
paragraph nodes remain present regardless of HTML suppression.

Tightness uses separation between sibling items or directly contained item blocks.
Private trailing-blank summaries propagate only through a final nested list/item
boundary, never from an arbitrary loose descendant, quote or opaque leaf. Marked
quote blanks and code-internal blanks do not loosen an outer list. Trailing blanks
alone do not loosen the last item. Empty items can close before an unowned blank;
a whole physical-line gap between established siblings still separates them. Each
sibling run is frozen once, each item's marker/child evidence is scanned in order,
and unowned gaps use binary searches in the shared line index. There is no repeated
construction of growing Lists or per-ancestor walk of all descendant subtrees.

Lists of empty items, definition-only items with known semantics, or established
inline-free blocks complete with available references and vacuous inline work.
Supported text-bearing lists now complete; multiline or other undelivered inline
interpretation retains pending inlines.
Non-rendered definition spans contribute direct private item boundaries, so their
removal does not erase blank separation or invent rendered children. Example 317
has all three items, one document-wide definition and outer Tight=false. Empty
quotes/items and later siblings survive. Local inner/outer tightness stays
independent. Cancellation/deadline discards unfinished items/quotes, retains
already-closed prefixes (wrapped in Lists where appropriate), invalidates global
registry availability and preserves strict UTF-8 failure precedence.

`ListProperties` adds optional, item-only facts:

- `MarkerWidth`: ASCII marker width (1 for a bullet, 2–10 for ordered markers).
  The `item-marker` range retains spelling such as `003.` independently of start 3.
- `Padding`: effective first-line padding, 1–4 structural columns. With more than
  four nonblank padding columns, only one column is consumed and the residual
  indentation stays content. An empty first line has effective padding one,
  independent of absent or trailing source whitespace.
- `ContentIndent`: leading indentation + marker width + effective padding, relative
  to the enclosing container's structural cursor (2–17 columns). It is neither an
  original byte count nor a public diagnostic column. Tabs keep their original
  physical tab-stop phase across enclosing quote prefixes and markers.
- `Prefix`: a source-backed Value for actual consumed leading indentation, marker
  and padding contributions. `item-prefix` selects their original-byte envelope,
  preceding `item-marker` in source-start order. Tabs retain explicit consumed
  PartialTab contributions; a residual contribution in Content may share that
  original byte. Empty items retain all actual trailing whitespace in Prefix and
  no content. Virtual effective padding has no fabricated source byte/segment;
  Prefix's actual semantic width can therefore differ from ContentIndent.

Prefix values and nested segments are validated and detached at construction and
access. Present widths/padding/indentation must satisfy their bounds and, when all
are supplied, imply 0–3 leading columns. Item-only fields are rejected on List.
Missing fields remain unknown on existing constructed fixtures; no new fact is
inferred from their zero values. Constructors still do not recognize grammar.

`TestListMarkerUpstreamTargets` independently asserts all 48 unchanged 253–300:
40 inputs with established item/block ownership, seven non-item cases and one
complete code case. Opening marker facts remain independently asserted by the
retained marker table; `TestListContinuationUpstreamTargets` separately asserts
full trees, child order, exact envelopes and logical leaf segments for every case.
The per-case remaining owners below also appear in foundation.json; they are
completion/coverage owners, not reverse prerequisites on this structural increment.

| Cases | Current recognition and remaining full-case work |
| --- | --- |
| 253, 285 | Paragraph/code/quote introduction or noninterrupting empty markers; supported breaks |
| 261, 266, 269, 272, 275 | Ordinary nonmarkers or paragraph/code examples; supported HTML |
| 254, 286–288, 290–293 | Established items with multiline/code/quote children and lazy ownership; supported breaks |
| 255–260, 262–265, 267–268, 270–271, 273–274, 276–283, 294–300 | Established item/nested/sibling/outside ownership and leaf content; supported HTML |
| 284 | Complete empty item in a tight List; supported HTML |
| 289 | Existing complete indented code; unchanged HTML pass |

Independent fixtures cover all bullets/delimiters, digit bounds, padding boundaries,
empty defaults, raw ordinal spelling, leading/quote/tab phases, shared tab bytes,
paragraph interruption/fallback, opaque payloads, UTF-8/NUL/endings/EOF, source and
model immutability and deterministic interruption. The bounded list-marker fuzz
target checks source/ordinal/provenance invariants, not complete list grammar.
Earlier list-dependent quote/Setext/code/HTML/thematic fixtures now assert actual
item children and following siblings/outside blocks. Invalid punctuation candidates also retain
ordinary paragraph structure where the former conservative hook stopped.

`TestListGroupingUpstreamTargets` independently asserts the complete available
List/item/block hierarchy, exact envelopes/segments, original marker ranges and
per-list properties for every unchanged Lists input 301–326. All now render,
including multiline 304/312. Cases 307/319 distinguish
loose inner lists from tight parents, 318 protects code blanks, 320/321 cover quote
and consecutive-block context, and 324–326 distinguish direct separation and nested
tightness. Constructed renderer tests separately check mixed inner/outer tightness
and quoted paragraphs; they do not establish recognition. Complete actual-Parse
source assertions and HTML checks promote Tabs 7 and empty item 284, preserving all
84 previous passes at that increment. Definition-only 207 now adds a pass,
at that increment. Code spans raised the baseline from 254 to 279 passes;
physical breaks raised it to 331 passes; angle autolinks now retain those passes
and raise it to 355 pass, 297 pending. Boundary fixtures cover blank ownership,
opaque trailing blanks, empty-item gaps in quotes, all endings/EOF and
nested reference definitions. FuzzListGroupingBoundaries independently generates
marker changes, sibling counts and blank boundaries. Full grammar/budgets/rules
and CLI/MCP remain later work.

## Inline text and staged providers

A finished block pass sets private recognition evidence separately from the
inline-free flag. Model constructors, empty children or available references alone
cannot establish this proof. With complete blocks and effective reference lookup,
the inline pass traverses document/quote/list/item children and consumes logical
paragraph/heading Content. Code and raw HTML are returned unchanged.

Text runs around recognized code, breaks and autolinks refine the block's ordered Content into raw Exact/Normalized/
PartialTab contributions and explicit Escape/Entity transformations. Its semantic
Literal is assembled without changing block Content or source bytes. Its envelope starts at the first
content byte and ends at the last, omitting boundary prefixes/markers. Interior
stripped prefixes remain gaps between source segments, never logical text. NUL's
one source byte contributes semantic U+FFFD without changing later byte positions.
No child is created for empty content. Blocks retain their envelopes, markers,
references and hierarchy. Published handles remain immutable; changed trees are
built privately and published once after scanning, with cancellation checkpoints
at nodes, contributions and bounded byte/punctuation runs.

CodeSpan recognition indexes raw maximal runs by width once per block and selects
the first subsequent equal-width raw closer with monotonic per-width cursors.
Unmatched runs stay in surrounding Text and do not prevent subsequent valid spans.
An advancing logical Content cursor splits identity segments at syntax boundaries;
NUL/partial-tab contributions retain their original ranges. Recognized code bypasses
ordinary decoding and interior provider guards. Its envelope includes independent
opening/closing markers; Content includes the full original payload, including
trimmed spaces and physical endings. Literal normalizes LF/CRLF/CR to spaces, then
removes one ASCII space at both edges only when both qualify and the result is not
all spaces. Explicit Normalized contributions account for changed semantics;
Exact identity validation remains intact. No generic whitespace trimming, entity
or backslash decoding occurs inside code. Block/reference Content is unchanged.

Angle autolinks are recognized atomically before ordinary Text decoding or
interior delimiter dispatch. URI recognition uses the pinned 2–32-character ASCII
scheme grammar, not URL registration/validation. Email recognition uses the pinned
local-part/domain grammar. Raw backslashes, entity spellings and punctuation stay
literal inside accepted links. Generated/escaped opening angles never create links;
code spans and opaque blocks take precedence when opened first. Invalid autolinks
resume ordinary interpretation. A conservative HTML-opener guard keeps possible
inline HTML pending for #23; it does not claim HTML recognition. Unmatched angles
and provably non-HTML rejected forms can complete as ordinary text.

Autolinks use resolved Link origin `autolink`, Available resolution, Label and
Destination Values, and one literal Text label child. Content and Label retain the
original interior ranges and NUL provenance. `destination-open` and
`destination-close` markers select the exact `<` and `>` bytes; the Link envelope
includes both delimiters. URI destinations preserve label semantics. Email
Destination prepends a source-absent Synthetic `mailto:` contribution to the same
address facts; neither label nor source acquires that prefix. Stored destinations
remain unencoded. Only the test adapter URL-encodes and then HTML-escapes them,
independently of label escaping. The advancing Content cursor preserves offsets
through surrounding Unicode/NUL and stripped container prefixes. Grammar scans
stop at forbidden bytes or nested openers; new loops retain cancellation checks.
Bare URI/email forms remain ordinary core Text, with no GFM extension enabled.

Dispatch retains per-block delimiter-run and bracket/image-opener state. Repeated
star/underscore runs and paired brackets defer to later providers. Valid ordinary
escapes/entities consume original spelling atomically without becoming new syntax.
States never cross paragraphs/headings or calls. Pending blocks may contain
provisional Text/CodeSpan/break/autolink mappings but retain whole-document
uncertainty and pending Evaluate outcomes. No Emphasis or InlineHTML is invented;
#23–#28 own remaining interpretation. The adapter's affirmative completion checks
are unchanged. Code-span 334 completes with its outside soft break; 346 completes
with its opaque autolink; 344 still needs #23. No interior punctuation introduces
reverse prerequisites. Final conformance requires all 652 without exemptions.

`TestAutolinkUpstreamAcceptanceAndNewPasses` consumes unchanged 594–612 and newly
unlocked cross-feature cases, pairing actual Parse/Evaluate HTML with independent
node, label/destination, synthetic prefix, marker and payload assertions. Invalid
HTML fallback 618 is explicitly checked as text. Grammar tests cover scheme/email
bounds, controls/space, Unicode, empty URI payloads and malformed forms. Source and
ownership tests cover both heading styles, nested/lazy containers, all endings,
NUL, original offsets, immutable accessors and repeat parsing. Deterministic
cancellation/deadline sweeps withhold unfinished trees. The inline/list fuzz
consumers independently verify autolink grammar and provenance alongside prior
Text/code/break assertions; broad numeric work/depth budgets remain #31.

Independent upstream 327–349 and newly unlocked 17/35/121/525/537/640/641 checks
assert raw payload, markers, literal and node kinds alongside original HTML.
Existing block/source assertions remain intact as obsolete stage expectations
advance. Nested/lazy containers, headings, endings/EOF, Unicode/NUL, all-space and
non-ASCII whitespace, mixed decoding, ownership and local-state fixtures protect
source consumers. Partial-tab logical-content preservation has a separate model
fixture; paragraph producers still strip their leading indentation. Deterministic
inline cancellation/deadline sweeps withhold unfinished trees and preserve encoding
precedence. FuzzInlineTextProvenance independently reconstructs Text, code, break and autolink
semantics from original contributions. Full numeric work/depth budgets remain #31.

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
Scanner helpers populate partial-tab/NUL mappings; inline Text preserves their
original-byte provenance. The shared decoder adds Escape/Entity contributions
without converting semantic lengths into original positions.

## Escape and character-reference interpretation

The shared source-backed decoder is used by inline Text, definition destinations/
titles and fence info. It reads parser-owned segments, accumulates private output
and returns detached segments for immutable publication. It splits ordinary runs
around each transformation, preserving NUL, physical endings, partial tabs and
omitted-prefix gaps. It checks caller context at contributions and bounded runs;
interruption withholds the unfinished value and cannot publish a completed phase.

ASCII punctuation escapes consume two original bytes once. Strict named references
require exact case-sensitive semicolon names from the 2125-entry generated map;
its authoritative source, license and offline regeneration are retained in
[testdata/html5](testdata/html5/README.md). Decimal references accept 1–7 digits;
hexadecimal references accept x/X and 1–6 digits. Zero, surrogates and values above
U+10FFFF become U+FFFD; other Unicode scalars are preserved without HTML browser
numeric remapping. Unknown/missing-semicolon/overlong references remain literal.
Lookahead is bounded by that grammar and the longest authoritative name.

Neither generated punctuation nor generated whitespace is redispatched as syntax.
Escaped ampersands and decoded ampersands do not initiate another decoding pass;
raw reference-label identity is unchanged. Code/raw HTML payloads are opaque;
unsupported inline contexts retain pending interpretation. Physical escaped line
endings are consumed by the break handler before ordinary Text decoding. Complete table/numeric tests, independent actual Parse/source/
phase assertions, deterministic interruption and the inline provenance fuzz oracle
verify these boundaries separately from HTML comparisons.

## Hard and soft line breaks

After block/reference completion, eligible physical endings produce SoftBreak
(origin `line-ending`) or HardBreak (origin `spaces` or `backslash`). Two or more
original ASCII spaces or an unescaped original backslash immediately before the
ending form the complete `hard-break-marker`; Content contains only the original
LF, CRLF or CR ending. The envelope covers marker plus ending for hard breaks and
only the ending for soft breaks. Break nodes have no Literal or inline children.
The shared advancing Content cursor preserves exact source ranges, container gaps
and original ownership without using semantic lengths as physical offsets.

The one trailing ASCII space before a soft break stays in the preceding Text's
Content with Normalized empty semantics. It is not an Escape/Entity contribution
or a hard-break marker. Next-line indentation and terminal block endings/whitespace
are already omitted from logical Content by block recognition; no terminal break
is fabricated. All original bytes remain in Source and block envelopes/content.
Code spans consume their internal endings opaquely, while raw code/HTML blocks are
unchanged. Original escapes/entities are consumed atomically; generated endings,
spaces and backslashes never enter break dispatch. Unsupported inline HTML and
other providers retain whole-document uncertainty even beside provisional breaks.

`TestBreakUpstreamAcceptance` consumes unchanged 633–649 with independent kind,
origin, marker, ending and Text assertions as well as actual Parse/Evaluate. Cases
633–637 and 644–649 render; 640–641 retain code opacity, 638–639 await emphasis and
642–643 await inline HTML (#23), correcting stale link ownership. Case 334 now
renders with its external soft break. Independent fixtures cover LF/CRLF/CR,
Unicode/NUL/scalar coordinates, zero/one/many spaces, mixed tabs, escaped
backslashes, generated characters, nested/lazy containers, both heading styles,
EOF and opaque payloads. Detached-accessor and deterministic checkpoint sweeps
protect immutable publication and encoding/interruption precedence. Existing block
oracles retain original envelopes, markers and segments while accepting mixed
inline children; affected inline/list fuzz consumers check those new kinds.

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
| `List`, `ListItem` | List.Ordered; optional Start, Delimiter (`.`/`)`), Bullet (`-`/`+`/`*`), Tight when known. Item-only MarkerWidth/Padding/ContentIndent and source-backed Prefix; `item-prefix`/`item-marker` ranges. Recognized items have ordered block children, empty raw Content, and per-line `item-indent`/`item-blank` evidence; grouping and independently derived Tight belong to the List parent. |
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
separate. Parse recognizes and normalizes definitions; #27 later resolves uses.

Recognition uses a private logical paragraph view with an advancing byte cursor
and original-line mappings; each definition is frozen once. It handles multiline
labels/destinations/titles, escaped delimiters, balanced bare destinations, angle
destinations, and optional quoted/parenthesized titles. A failed same-line title
invalidates that definition; failed next-line titles retain a destination-only
record and preserve the following prose. Only leading valid sequences are removed.
Labels retain raw escapes/entities. Matching uses full default Unicode 17.0.0 folding
(C + F, excluding Turkic T), trims/collapses only space/tab/CR/LF, and counts the
999-character bound before folding in Unicode scalars, not bytes. Empty/whitespace
labels and unescaped nested brackets are invalid. The shared private normalizer is
ready for #27; no visible-inline decoding enters its identity. Pinned source,
license and offline table verification live in [the Unicode provenance directory](testdata/unicode/17.0.0/README.md).

Values retain component segments with source-backed NUL/newline normalization.
Absent titles differ from present empty titles; empty angle destinations retain
zero-width ranges. Destination/title values decode ASCII punctuation escapes and
strict named/numeric references once, retaining original spans and explicit
transformations. Physical CR/CRLF normalizes to LF; entity-produced endings remain
semantic characters and are not normalized again. Source bytes never change.
Definition envelopes start after container prefixes (including remaining leaf
indentation) and end after the final consumed physical line ending or at EOF;
component segments exclude container prefixes inside multiline records.

Actual Parse assertions cover all unchanged 192–218 cases, exact definition and
marker envelopes, duplicate/first-selection behavior, paragraph/Setext remnants,
opaque code/HTML, container/endings/EOF/tab variants, 999/1000-character labels,
full-fold/raw-spelling distinctions, immutable accessors and phase semantics.
Example 207 additionally passes actual Parse/Evaluate with one definition, no block
children, completed phases and empty HTML. Deterministic cancellation/deadline
sweeps cover recognition, closure and EOF; a registry/source invariant fuzz target
retains definition order, source ownership and earliest matching selections.

Records Availability and effective LookupAvailability are independent: unavailable
data never means a proved-empty registry. Lookup maps already supplied normalized
labels to the first matching definition index. NewDocument validates indices,
first-selection consistency and coverage for an Available lookup, without doing
normalization. Partial lookup may contain only known first selections; unavailable
lookup has no entries. Mutable collection copies cannot change the document.

[`foundation.json`](testdata/commonmark/0.31.2/foundation.json) explicitly
accounts for all 652 upstream IDs: 355 supported HTML cases and 297 pending.
Autolinks add 24 demonstrated passes while preserving all prior 331: 20, 346,
480–481, 526, 538, 594–610 and 618. Other cases retain their
actual #23–#28 providers, with no undelivered #17–#22 ownership. Dispatch may defer
potential invalid candidates conservatively until a provider proves fallback.
These coverage scopes are not reverse native prerequisites on this increment.
The test-only adapter/runner remains in internal/conformance and
tools/commonmark_conformance.py, without production rendering.
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
general inline/link conformance, full resource enforcement,
fix safety or CLI/MCP parity.
