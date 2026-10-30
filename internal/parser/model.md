# Parser model and staged block contract

`internal/parser` defines an in-memory CommonMark 0.31.2 model and a staged
paragraph/block lifecycle with indented/fenced-code, HTML-block, thematic-break,
ATX, Setext, mixed block-quote/list-item recognition and continuation. All core
block and inline families are recognized, including links and images. It retains
paragraph/heading source content and constructs source-preserving inline children. ListItems retain
first-line metadata, nested block children, continuation and closure. Compatible
siblings acquire List parents and independently derived tightness.
Leading reference definitions are recognized and normalized document-wide; inline
text, code spans, breaks, raw inline HTML, angle autolinks and nested
emphasis/strong and inline/full/collapsed/shortcut links and images are recognized. The model
catalog also supports constructed fixtures; it does not promise recognition of
every listed kind.

## Parse outcomes

### Optional opaque frontmatter

`ParseWithOptions(ctx, source, budget, ParseOptions{...})` is additive: its zero
options match `ParseWithBudget`, and legacy entry points continue with headers
disabled. `ParseOptions.GFM` selects the complete approved GFM profile independently
of `Frontmatter`. `NewFrontmatterOptions(enabled, formats)` constructs an immutable
selection; nil formats means `[yaml, toml]`, while an explicit empty, duplicate or
unsupported selection is rejected even when disabled. No caller slice is retained.
The parser imports no configuration adapter and does not enable CLI capabilities.

Recognition runs over admitted original physical lines before block/reference
recognition. `Document.Frontmatter()` returns an immutable typed header record and
a presence boolean. This record is separate from the ordinary body AST, like the
non-rendered reference inventory: `Root` still spans `[0,N)`, but its children and
`Walk` visit only Markdown body syntax. Header contents contribute no inline nodes,
reference definitions or comment facts. Later body delimiters are ordinary Markdown.
No YAML/TOML semantic decoding, validation or rewriting occurs.

The record's `Format`, `Span`, `Opener`, `Content`, `Closer` and `BodyOffset`
accessors retain original snapshot ownership. `Span` includes optional BOM and
physical delimiter endings. Marker spans omit BOM/endings and retain trailing
ASCII spaces/tabs. `Content` excludes delimiter lines and can be present empty;
absent `Closer` means unterminated, not an empty header. `BodyOffset` equals the
header envelope's exclusive end. Body parsing keeps the same Source, line index
and original coordinates. Replacing a returned record cannot mutate its Document.
Only parsing establishes this record; `NewDocument` does not infer it from an
unoccupied prefix in a constructed tree.

A complete header and complete body report normal `Complete`/`Available`. An
unterminated header consumes through EOF and retains its reliable record, with
`Incomplete`/`Partial` and `unterminated-frontmatter` uncertainty over its envelope.
Its block/inline traversal can be complete without establishing the private
completed-parse handoff; `Interruption` remains empty unless work was interrupted.
Existing completed-only comment/protection APIs retain their guards. Consumer
protection of complete and incomplete header envelopes, inspection and CLI
availability are the separate #168 integration. Structured metadata is later work.
Size/UTF-8/context precedence and shared operation accounting are unchanged.

### Core source and outcome guarantees

Acquire a snapshot with `NewSource(content)`, then call `Parse(ctx, source)` with
a non-nil context. Both operations stay in memory. `Source` retains arbitrary
bytes; Parse validates strict UTF-8 before any normal Markdown work. Malformed
bytes remain available through `result.Source`, with no transcoding/replacement.
Literal U+FFFD and U+0000 are valid encoding; scanner helpers replace NUL only in
semantic contributions, never in Source. Document-size admission precedes
validation; validation itself is work-budgeted.
Once established, encoding failure takes precedence over cancellation/deadline.
Prior exhaustion or insufficient validation work can stop before encoding is
established; see [parser accounting](../../docs/engineering/parser-limits.md).
Rule/io attribution, CLI exits and result-schema adaptation belong to callers.

| Input/outcome | Status / AST | Reliable data and phase progress |
| --- | --- | --- |
| Nil or zero-length source | `Complete` / `Available` | Root `[0,0)`, no children, available empty reference records/lookup, both phases complete |
| ASCII-blank source (space/tab/LF/CR only) | `Complete` / `Available` | Root `[0,N)`, no children, established empty references/lookup, both phases complete |
| Indented/fenced code with known info semantics, HTML blocks, thematic breaks, empty ATX headings and ASCII blanks, including quotes and lists of empty items or established inline-free blocks, nested in either kind of container | `Complete` / `Available` | Root `[0,N)` with source-bound blocks, established empty references/lookup, both phases complete, no uncertainty |
| Supported Text/CodeSpan/break/autolink core blocks | `Complete` / `Available` | Text/CodeSpan preserve raw contributions and semantic Literal; breaks retain origin, original ending and hard-break markers; autolinks retain resolved label/destination provenance; both phases complete |
| Definition-only input with established destination/title semantics (including inside otherwise inline-free containers) | `Complete` / `Available` | All records retained, first-definition lookup available; no definition block children; both phases complete |
| Definition destination/title or fence info contains escapes/entities, with no other missing syntax | `Complete` / `Available` | Known decoded values and explicit raw transformations, available reference lookup and complete phases |
| Invalid UTF-8 | `Failed` / `Unavailable` | Original Source, typed `Failure` with `InvalidUTF8` code and snapshot-bound byte location, nil Document, neither phase started |
| Resource budget exhausted | `Incomplete` / `Partial` | Copied `EffectiveLimits`, detached typed `Limit` (`Name`, `Observed`, `Allowed`), whole-source `limit-exceeded` uncertainty and interruption; unfinished phase interrupted, reference availability withheld |
| Cancellation/deadline on otherwise valid source | `Incomplete` / `Partial` | Original Source and partial root with any already-closed prefix blocks, affected region with `Cancelled`/`DeadlineExceeded`, separate Interruption, nil Failure; unfinished work interrupted |
| Other operational failure | `Failed` / `Unavailable` | Retained Source, typed stable failure code, nil Document; InternalFailure if block-model construction violates invariants |

The complete rows require admitted size and sufficient runtime budgets; an
oversized malformed snapshot reports document-size admission first. See the
[accounting specification](../../docs/engineering/parser-limits.md) for
validation-budget and interruption precedence.

All core inline handlers are delivered. Disabled extensions/frontmatter follow
ordinary core syntax and do not become errors. ASCII-blank input
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
inline scanning. All core inline handlers now complete; historical staged providers described below
are delivered. Runtime interruption still produces explicit uncertainty. Definition destination/title and fence-info
semantics are now supplied by the shared decoder; interruption still prevents a
completed block/inline handoff. A finished definition-only/inline-free document with
known semantics has both phases complete. Constructed nodes or supplied registry
data cannot establish the private block-recognition handoff.

Both phases share the caller context. Checks occur at phase boundaries, before
indexed block lines/EOF, during reference recognition (bounded labels and long
components), and during bounded inline scanning. Interrupted collection invalidates global registry availability and
discards unfinished containers rather than finalizing them as normal EOF. Already
closed root prefixes survive. Established UTF-8 failure precedes cancellation/deadline,
subject to size admission and validation budgets above.
Full work metering, index/UTF-8 checkpoints and budget enforcement are defined
in [parser accounting](../../docs/engineering/parser-limits.md).

The staged histories below retain intermediate fixture/provider terminology.
References to missing handlers or pending syntax describe those earlier stages;
all core block/inline providers are now delivered. Current partial outcomes arise
from interruption, not missing CommonMark syntax.

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
now completes through literal delimiter fallback and 56 through single-level
emphasis. Code/break-only passes remain intact.
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
and deeply indented lines can continue paragraphs. All core containers and start
handlers are now delivered.

Heading Level and Style (`atx`) use the existing property catalog. The envelope
retains the complete original physical line, indentation and LF/CRLF/CR ending or
EOF. The `opening` marker selects just the initial hash run. An optional `closing`
marker selects a terminal hash run preceded by ASCII space/tab and followed only
by ASCII space/tab. Escaped/nonclosing hash sequences remain raw block content;
the inline decoder interprets eligible escapes after classification. Block
classification does not perform semantic escape decoding. Missing closers
are absent, not synthetic zero-width markers. The original-operation `Headings`
projection preserves `Style` as `atx`/`setext` and exposes `HeadingFact.Closed`
as an owned boolean for a retained ATX closing marker, including empty headings.
Marker visits share the original work budget before publishing facts. Leading/trailing ASCII space/tab
and valid closing runs are excluded from logical content but remain in Source.
Unicode whitespace is not trimmed. Content has original-byte Exact segments,
with explicit Normalized provenance only for semantic NUL replacement. No inline
children or semantic Literal are invented.

Empty content established by recognition can join supported breaks/blanks in a
complete document; supported nonempty headings now complete with Text children.
All core handlers are now delivered; interrupted work retains uncertainty.
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
Setext/paragraph text and now pass. The block stage itself recognizes neither
code spans nor inline HTML; the later inline pass now supplies both.
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
with known semantics. Supported text/nonempty headings now complete, including
all core inline handlers and known info/definition semantics. Interruption
discards unfinished nested containers rather than finalizing them as normal EOF;
already-closed root children survive. Strict encoding failure still takes precedence.
Finite default depth/node/work/time budgets, shared-operation accounting and
scanner checkpoints are implemented; see the [accounting specification](../../docs/engineering/parser-limits.md).

`TestQuoteUpstreamTargets` asserts actual trees, child order, envelopes, markers and
content ranges for all unchanged 228–252 independently of HTML. Cases 236/239/240
newly pass HTML; existing literal-code case 231 remains supported. List case 235 now renders its quoted item and outside sibling. Other supported
single-line and multiline quote paragraphs/headings also render when their other
inline providers are available. `TestQuotePartialTabUpstream6` independently verifies original tab/code
provenance. Reference cases 214/218 now complete through document-wide use resolution.
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
with available empty references and vacuous inline phases. Surrounding core syntax
is processed by the delivered block and inline handlers. Constructed
HTML nodes cannot manufacture the private recognition/handoff proof.

TestHTMLUpstreamTargets independently asserts form, node order, physical envelopes
and content ranges for all unchanged 148–191 inputs plus cross-feature passes 21/31.
All 44 HTML-block-section cases now pass actual Parse/Evaluate, including text
and single-level emphasis around opaque blocks, as recorded in foundation.json.
Coverage owners are not reverse blockers on this syntax stage. Separate fixtures cover grammar near misses,
case/EOF/fixed-form boundaries, code and paragraph precedence, opaque payload,
LF/CRLF/CR/NUL/Unicode/tab provenance, immutable accessors, encoding precedence and
deterministic interruption, including at EOF finalization. Actual full-corpus
normalized/strict gates now record 652 pass and 0 pending, preserving all 355
passes established before inline HTML.

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
Full work metering/checkpoint hardening is defined in
[parser accounting](../../docs/engineering/parser-limits.md).

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
Full metering/checkpoint hardening is defined in
[parser accounting](../../docs/engineering/parser-limits.md).

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
Text-bearing lists now complete, including multiline core inline interpretation.
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
and now coexist with inline HTML, nested emphasis and inline/reference links and
images at 652 pass, 0 pending. Boundary fixtures cover
blank ownership,
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
resume ordinary interpretation. Actual inline HTML recognition follows autolink
rejection. Rejected HTML and
unmatched angles resume ordinary text interpretation.

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

Dispatch records source-backed delimiter runs and bracket/image-openers per block.
Unicode flanking and bounded pairing settle stars/underscores after
opaque code/HTML/autolinks and atomic escapes/entities have been scanned. Proven
invalid/unmatched pairs become Complete Text. Nested matches construct complete
Emphasis/Strong trees and inline/reference Links and Images. States never cross paragraphs/headings or calls; the
adapter's affirmative completion checks remain unchanged.

`TestAutolinkUpstreamAcceptanceAndNewPasses` consumes unchanged 594–612 and newly
unlocked cross-feature cases, pairing actual Parse/Evaluate HTML with independent
node, label/destination, synthetic prefix, marker and payload assertions. Invalid
HTML fallback 618 is explicitly checked as text. Grammar tests cover scheme/email
bounds, controls/space, Unicode, empty URI payloads and malformed forms. Source and
ownership tests cover both heading styles, nested/lazy containers, all endings,
NUL, original offsets, immutable accessors and repeat parsing. Deterministic
cancellation/deadline sweeps withhold unfinished trees. The inline/list fuzz
consumers independently verify autolink grammar and provenance alongside prior
Text/code/break assertions; broad numeric work/depth budgets follow
[parser accounting](../../docs/engineering/parser-limits.md).

Independent upstream 327–349 and newly unlocked 17/35/121/525/537/640/641 checks
assert raw payload, markers, literal and node kinds alongside original HTML.
Existing block/source assertions remain intact as obsolete stage expectations
advance. Nested/lazy containers, headings, endings/EOF, Unicode/NUL, all-space and
non-ASCII whitespace, mixed decoding, ownership and local-state fixtures protect
source consumers. Partial-tab logical-content preservation has a separate model
fixture; paragraph producers still strip their leading indentation. Deterministic
inline cancellation/deadline sweeps withhold unfinished trees and preserve encoding
precedence. FuzzInlineTextProvenance independently reconstructs Text, code, break and autolink
semantics from original contributions. Full numeric work/depth budgets follow
[parser accounting](../../docs/engineering/parser-limits.md).

## Emphasis and strong emphasis

Original maximal star/underscore runs are classified at rune boundaries with the
pinned CommonMark whitespace (Zs plus TAB/LF/FF/CR) and punctuation (P or S)
definitions. Underscore intraword restrictions and the dual-purpose modulo-three
rule, including the both-lengths-multiple-of-three exception, apply before matching.
Original run lengths remain distinct from mutable consumed/remaining widths.

A private linked delimiter stack processes closers in source order, with failed
search floors partitioned by marker, original closer length modulo three and
opener eligibility. Stable source-order indexes remain valid floors after removal.
A live stack-bottom index (or -1 for the whole stack) bounds initialization of the
floors, searches and suffix cleanup. Earlier delimiters retain their identity and
unconsumed state for subsequent scoped calls; retired suffix records never revive.
The private handoff rejects an invalid or retired boundary. Actual bracket/link
integration remains a later provider.

Pairing emits inner pairs before their parents, in closing-source order. Each
parent consumes a suffix of a private forest once. Explicit construction frames
visit that forest in source order while one flat-node cursor consumes original
inline nodes. Completed direct children are appended once; no insertion shifts or
copies the accumulated prefix. A once-built logical segment index maps pair
boundaries and copies each node's raw payload contributions without fragmenting
Content at nested markers. Overlapping parent/child payloads remain explicit output;
segmented nesting can therefore require overlapping output segment storage.

Published Emphasis/Strong nodes have delimiter width 1/2 and exact source-backed
opening/closing markers, an envelope including those markers, raw logical payload
Content and ordered children. Parent Content retains child syntax bytes; each
child retains its own marker and payload ownership. Content excludes the node's
own markers and stripped container prefixes; NUL, partial tabs, interior source
gaps and physical endings retain provenance. Content is not the decoded aggregate
Literal. Leftovers remain original Text. Source snapshots and block Content never
change. Opaque interiors and decoded markers are never redispatched.

New scans, matching, forest construction, mapping and frame traversal retain caller
cancellation/deadline checkpoints, private completed-tree publication and strict
encoding precedence. Nested construction uses explicit frames instead of recursive
Go calls. Comprehensive numeric work/depth budgeting follows
[parser accounting](../../docs/engineering/parser-limits.md).

The fixed acceptance partition covers all 132 emphasis examples: preserve the 90
single-level passes, add the specified 36 nested cases, and six inline-link
cases now completed by #26. Thus all 132 are required passes. The exact lists and independently
counted consumed marker positions live in TestEmphasisApprovedPartitionAndUpstream;
recursive source oracles assert parent Content and child coverage. Separate tests
cover scoped/repeated stack processing, mapped nested containers and headings,
Unicode/NUL, partial tabs, physical endings/EOF, opacity, detached nested handles,
long runs and deterministic interruption. Both inline/list fuzz consumers retain
their independent source assertions with nested trees.

Nested construction raised 477 passes to 513, inline links to 575, and reference
uses to 629 passes, then images to 652 pass / 0 pending. Cases 419/433/516/530 now complete.
Delivered #25–#27 ownership is removed. Full conformance
now has all 652 cases passing.

## Inline links and bracket boundaries

Inline Link recognition reads original logical Content before decoding. Active
openers prevent links within links; an autolink also deactivates earlier openers.
A monotonic source boundary represents deactivation without revisiting the inactive
prefix. Code/HTML/autolinks consume opaque interiors; escapes/entities are atomic
and generated brackets never dispatch. A failed candidate retains ordinary text
and later constructs. Without definitions, a completed reference inventory proves
all reference fallback absent. With definitions, the reference resolver below
settles matching uses and literal misses, including image forms.

Bare-destination lookahead builds a private lexical endpoint index lazily on the
first bare candidate. One forward pass marks original escape tokens; one backward
pass composes suffix endpoints and matches parentheses within whitespace/control
boundaries. Each token is visited twice, each closer is pushed/popped at most once,
and overlapping candidates query their endpoints directly. Unbalanced candidates
do not rescan growing EOF suffixes or skip later valid constructs. This adds linear
per-block index storage while preserving escaped parentheses, arbitrarily nested
balanced destinations and existing interruption/publication behavior. It does not
introduce an independent grammar limit; the shared finite resource budgets
are specified in [parser accounting](../../docs/engineering/parser-limits.md).

Each bracket saves the last live delimiter before its opening. Successful labels
process emphasis strictly above that boundary, retiring only the suffix. Subsequent
runs append to the live prefix without resurrecting retired records; block-end
processing can then pair surrounding emphasis across sibling Links. Construction
borrows immutable raw segments, uses indexed logical slicing and advancing node
cursors, and publishes only after all scanning/mapping succeeds. Interruption
checkpoints cover candidate parsing, whitespace, values, scoped pairing and mapping.

Recognized Links have Origin `inline`, Available resolution, absent NormalizedLabel
and ordered label children. Content and Label.Segments select the raw interior
between square brackets, including nested markup and original physical endings;
Label.Semantic concatenates logical contributions without decoding escapes/entities.
The children carry interpreted label semantics. Empty labels have no children and
one exact zero-width Content/Label contribution at the closing bracket.
Destination and optional Title Values decode escapes/entities once; physical CR/CRLF
in titles normalize to LF without changing original spans. Known-empty destinations
and present-empty titles have exact zero-width ranges at their following delimiter;
an absent title is nil. Stored destinations are unencoded. Container gaps, NUL and
partial-tab mappings retain their source ownership independently of semantic width.

`label-open`/`label-close` select `[`/`]`; `link-open`/`link-close` select the outer
`(`/`)`. Angle destinations additionally carry `destination-open`/`destination-close`
for `<`/`>`; bare destinations have no such angle markers. Present titles carry
`title-open`/`title-close` for their actual quotes or parentheses. Every marker is
one original byte, ordered by source position. Components exclude their delimiters
and surrounding separator whitespace; the Link envelope includes the entire syntax.
These internal roles do not change any public inspection or adapter schema.

`TestInlineLinkApprovedPartitionAndUpstream` requires the fixed 47 sole-#26 cases
plus 567 and verifies all 14 additional proved-literal promotions. Independent
fixture component/envelope ranges accompany actual Parse/Evaluate HTML. Existing
inline/list/emphasis/decoding provenance consumers distinguish origins without
weakening autolink/source oracles. Mapped headings/containers, endings/EOF,
Unicode/NUL, empty components, raw versus decoded values, accessors, scope isolation,
malformed recovery, no nested links, scoped emphasis and deterministic interruption
have focused checks. Deterministic failed-destination/recovery checks at doubled
input sizes reject quadratic cumulative rescanning while independently verifying
literal and recovered-link source facts. A repeated benchmark records timing and
allocation tradeoffs without a machine-specific threshold. Full numeric work/depth
hardening is implemented; cumulative
bracket/candidate measurements are optional and no performance guarantee is claimed.

## Reference links and definition identity

Full, collapsed and shortcut Links resolve only after the immutable document-wide
registry completes. Inline scanners borrow that private registry across blocks;
no detached public collection copy, rebuild or definition scan occurs per use.
The shared raw-label normalizer retains full pinned Unicode folding and the
999-character pre-fold eligibility bound, with only space/tab/ending collapse.
In core parsing, escapes/entities do not become matching characters. The private
table stage supplies its narrowly mapped escaped-pipe contribution before this
shared resolver: original backslash-pipe bytes contribute logical pipe to the
lookup identifier. A valid explicit second label
precludes shortcut fallback even if its lookup misses. Failed inline recognition,
including 568, can fall back to references; valid inline 567 still takes priority.
Resolved references share scoped emphasis and earlier-opener deactivation with
inline links. Misses in a completed registry become ordinary interpreted content.
Images share this resolver and remain active independently of link openers.

A reference Link's Envelope contains only its use syntax. Content and Label select
the visible first-bracket interior before inline escape/entity interpretation;
children carry the displayed semantics. Optional `ReferenceLabel` separately maps
the second-bracket interior: raw key for `full`, a present-empty Value with one
Exact zero-width segment at the second closing bracket for `collapsed`, nil for
`shortcut` (and for inline/autolink). Source-backed components use the existing
NUL/tab/container/physical-ending mappings and stay inside the use envelope.
The table stage additionally permits Escape only for the complete original
backslash-pipe spelling with semantic pipe. A full ReferenceLabel retains those
raw bytes and that mapped semantic contribution; its aggregate must equal its
segment contributions. Other decoded escape/entity mappings remain invalid
identifier provenance, and ordinary core identifiers retain raw escape spelling.
Generic envelope ownership still rejects absent or foreign-source components.
`label-open`/`label-close` mark the first pair, `reference-open`/`reference-close`
the immediately adjacent second pair for full/collapsed. No destination/title
marker is invented at a reference use.

`NormalizedLabel` binds through the owning document's completed Lookup to its
first source-ordered Definition. Snapshot and record index establish identity,
even when duplicate records have identical destinations. The definition retains
all original destination/title spans and transformations. The Link's effective
Destination and present Title each project only the selected semantic string,
using one spanless Synthetic segment with matching semantic contribution. Synthetic
here means a use-site projection, not invented destination spelling. Provenance
consumers follow the association to the authoritative record; projections are never
editable use-site text. Known-empty destinations, absent titles and present-empty
titles remain distinct.

NewNode validates identifier shape/containment and resolved projection shape;
NewDocument also validates available local lookup and matching selected semantics
and title presence. Foreign-source definitions and unavailable/missing bindings
are rejected. General containment, inline/autolink behavior and immutable detached
accessors remain unchanged. The parser checks binding before privately publishing
the completed inline tree, with cancellation/deadline checkpoints. Constructors
still cannot manufacture recognized phases or full CommonMark support.

`TestReferenceLinkApprovedPartitionAndUpstream` couples all 53 fixed sole-#27 cases
and escaped-marker 593 to original HTML plus independent use/component/marker
ranges, origins and definition indices. Shared inline/list/emphasis/decoding
oracles verify reference syntax and definition provenance independently, with a
retained Unicode-source folding oracle. Raw-identity/999–1000-character, mapped
heading/container/endings, immutability, bad binding/projection and interruption
checks supplement those upstream cases. Images add 517/520/531 and 572–591,
bringing the ledger to 652 pass / 0 pending. No rule, CLI/MCP, production rendering
or independent resource-budget policy is added. Shared parser budgets follow
[parser accounting](../../docs/engineering/parser-limits.md).

## Images and semantic descriptions

Inline, full, collapsed and shortcut Images share the link builders and reference
resolver. Their Envelope starts at `!`; a leading `image-marker` selects exactly
that byte, followed by the existing label/component markers in source order.
Content and Label select only the first-bracket interior. Children retain its
inline structure rather than a pre-rendered alt string. Known-empty destinations,
absent/present-empty titles, escaped/entity components and mapped source provenance
follow the link contract. Reference Image projections and definition identity have
the same NewNode/NewDocument validation as reference Links.

Image opener activation is independent of the monotonic link-deactivation
boundary. Successful links and autolinks do not disable an image opener. An image
success does not deactivate earlier link openers. Link success still deactivates
earlier link openers, preserving the prohibition on links within links.

The test adapter derives alt text recursively from semantic Text/CodeSpan values,
newlines for breaks, and children of formatting, links and images. Inline HTML
contributes no alt text. Escaping happens after this flattening.

`TestImageUpstreamAcceptance` ingests unchanged 572–593 and 517/520/531 and asserts
independent image counts, envelopes, brackets, components, forms and nested AST
shapes. Mapping, semantic-alt, malformed/escaped syntax, definition folding and
first-selection tests supplement these cases. Shared fuzz provenance oracles and
deterministic interruption sweeps include images. The corpus is 652 pass/0 pending;
comprehensive work/depth hardening follows
[parser accounting](../../docs/engineering/parser-limits.md).

## Verified HTML ID attributes

`Document.HTMLIDs(ctx, budget)` derives a separate source-owned inventory from an
actual completed parse in the same operation. It does not add eager attributes to
the Markdown AST or let constructed Nodes/Documents claim recognized HTML. The
method reuses the complete CommonMark tag lexer, mapped logical content and pinned
HTML entity data. Collection shares the existing work/node/time budget. Actual
quota, cancellation or deadline errors withhold the entire inventory.

An Available collection contains source-ordered `HTMLIDOccurrence` records. Tag
selects the original physical envelope of the complete opening tag; Attribute
selects the authored id/ID spelling, optional equals/value and quotes. Value keeps
mapped original segments separately from its once-decoded HTML attribute semantics.
Present-empty and bare id attributes retain a real empty value. Later duplicate
id attributes remain occurrences with Effective=false: ASCII name case does not
change first-attribute precedence, and ID values remain case-sensitive. Empty IDs
are not usable named targets. Closing-tag attributes and legacy name anchors do
not create IDs.

Attribute decoding preserves literal backslashes and percent spellings. It uses
the attribute-specific semicolonless-name rule, numeric invalid-scalar/C1 handling,
and input CRLF/CR preprocessing; references are not decoded or preprocessed twice.
The existing 2125 strict named references remain unchanged. The 106 legacy bare
names are generated from the same pinned WHATWG fixture by
`python3 tools/html_entities.py`, and `--check` verifies both tables offline.

Extraction excludes Markdown code, opaque frontmatter, image-description HTML
(which becomes alt text), comments, declarations, CDATA/PI and quoted tag-looking
attribute text. Real nested HTML elements inside pre can carry IDs; script,
style and other HTML raw-text interiors cannot manufacture child elements. This
is a narrow static projection, not a DOM, script executor or browser-recovery
parser. An actual SVG or MathML opening enters an unsupported foreign namespace,
whose integration points cannot inherit HTML raw-text ownership from a local tag
name. Such entry immediately withholds the whole inventory and locates the
original opening-tag envelope. Protected or quoted foreign-looking text does not
enter that context. No namespace stack or tree builder is introduced.
Unsupported malformed/cross-block/script-escape contexts and filtered GFM
raw openings yield Unavailable plus an original-source uncertainty region, with
no occurrences. In particular, escaping a filtered opening does not establish
ordinary raw-text ownership or permit guessing a complete absent-ID inventory.

Each call owns its returned slices/values. Source spans still belong to the
original immutable snapshot, including mapped container gaps and transformed
entity contributions. Raw HTML heading elements are general targets, not parsed
Markdown Heading records. No format-specific heading attributes, generated slug
algorithm, fragment consumer, rule or TOC policy is introduced by this projection.

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

`TestSourceConsumerMixedDocuments` traverses independently authored mixed-document
fixtures in `testdata/source_consumers`. Expected hierarchy, original envelopes,
content/marker spellings and property/reference component ranges are retained
separately from Parse output. The same fixtures exercise LF/CRLF/CR with and
without final endings, scalar coordinates after Unicode and stripped prefixes,
partial-tab overlap, opaque code/HTML payloads, forward references and duplicate
first-selection. Inline HTML protects its raw tags; adjacent text still acquires
ordinary inline syntax and decoding. Detached accessor mutation and repeat parsing
must preserve both the retained tree and original bytes.

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
all core inline contexts now have delivered handlers. Physical escaped line
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
spaces and backslashes never enter break dispatch. Unsupported emphasis/link/image
syntax retains whole-document uncertainty even beside provisional breaks.

`TestBreakUpstreamAcceptance` consumes unchanged 633–649 with independent kind,
origin, marker, ending and Text assertions as well as actual Parse/Evaluate. Cases
633–637 and 644–649 render; 640–641 retain code opacity; 638–639 now render
with emphasis and 642–643 now complete through raw inline HTML, without creating internal breaks.
Case 334 now
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
| `Link`, `Image` | Link.Origin `inline`/`full`/`collapsed`/`shortcut`/`autolink`/`bare-gfm`; Label/Destination/optional Title Values, optional ReferenceLabel and NormalizedLabel, and explicit Resolution availability. `label-open`, `label-close`, `link-open`, `link-close`, `destination-open`, `destination-close`, `title-open`, `title-close`, `reference-open`, `reference-close`, `image-marker` roles as applicable; label children. |

Property constructors enforce recognized kinds, property-family applicability,
basic supplied heading/fence/HTML/delimiter/origin values and all source ranges.
They do not recognize syntax, normalize labels or validate complete Markdown
grammar. Unknown facts remain absent rather than fabricated. These meanings are
internal catalog contracts; #139 owns projection/schema adaptation.

## Recognized inline raw HTML

The inline pass recognizes the pinned CommonMark Raw HTML grammar on original
logical paragraph/heading content: open/closing tags and attributes, comments,
processing instructions, declarations and CDATA. Block-first recognition remains
separate, including the seven block forms and their physical-line termination.
Code spans and angle autolinks retain their precedence. Escaped/entity-generated
angles never start HTML. Rejected candidates resume ordinary text decoding and
syntax dispatch; accepted raw interiors bypass all such dispatch, including
backticks, breaks, brackets and emphasis punctuation.

Recognized nodes have Kind InlineHTML, HTML properties with absent BlockForm,
no Literal and no children. Content covers the entire raw construct, including
delimiters, using ordered snapshot-owned segments. Its envelope spans first to
last original byte; omitted container prefixes remain gaps rather than payload.
Physical endings and raw escapes/entities stay intact. NUL retains its original
byte with Normalized semantic U+FFFD. Paragraph continuation indentation is
already omitted by the block contract; internal tabs remain literal bytes.

Confirmed comments alone carry `html-comment-open` and `html-comment-close`
markers. The opener selects the original four bytes `<!--`. Ordinary closers
select `-->`; the short forms `<!-->` and `<!--->` select `>` and `->`
respectively, beginning immediately after the opener. The close marker's Start
and End provide exact original closing boundaries, including multiline comments
whose envelopes contain source gaps. Other inline HTML has no comment markers.
Comment-looking text inside quoted attributes or code is not a comment node.
These are parser facts for future consumers, not directive interpretation.
Constructed nodes still do not establish private parsing-completion evidence.

Terminator and quote positions are lazily indexed once per logical inline block;
lookups use ordered indexes rather than repeated failed suffix searches. State
does not cross blocks or calls. Indexing, grammar scanning and source mapping
retain caller-context checkpoints and private publication. Full resource budgets
follow [parser accounting](../../docs/engineering/parser-limits.md). This is no
general performance claim.

`TestInlineHTMLUpstreamAcceptanceAndNewPasses` consumes unchanged 613–632 and
all 28 newly available cases with independent original HTML-node envelopes,
raw-source grammar/provenance, comment markers and actual Parse/Evaluate HTML.
Grammar, malformed-fallback, quoted-content, block-boundary, container/heading,
Unicode/NUL, physical-ending, immutability and deterministic interruption checks
supplement the corpus. Shared inline/list fuzz consumers independently validate
InlineHTML source facts alongside their established checks. The ledger now has
652 passes and 0 pending; all prior 383 passes remain. No production
renderer,
directive processor, fix or extension is added.

## Reference records and completion inventory

`ReferenceDefinition` stores an envelope, ordered Markers, Label/Destination/
optional Title Values and optional NormalizedLabel. Keep all definitions, including
duplicates and unused definitions, in `Document.References().Definitions` source
order. Markers use the label/destination/title roles above and `definition-colon`.
These are not rendered block children. Raw spellings and semantic values remain
separate. Parse recognizes and normalizes definitions, then resolves uses against the
completed registry.

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
shared with reference-use resolution; no visible-inline decoding enters its
identity. Pinned source,
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
accounts for all 652 upstream IDs: 652 supported HTML cases and 0 pending.
Delimiter classification/pairing added 94 passes while preserving 383; nested
construction added 36 cases; inline links add 48 required and 14 proven literal
cases while preserving all 513 previous passes. Reference resolution adds 53
required cases and escaped-marker 593, retaining all 575 established passes.
All Raw HTML 613–632 pass (618 was already established literal fallback). The
23 image cases now pass too; no delivered #17–#28 ownership remains.
The test-only adapter/runner remains in internal/conformance and
tools/commonmark_conformance.py, without production rendering.
All unchanged upstream cases pass; individual cases exercise different syntax
features. This inventory is maintained separately from unmodified
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

## Internal GFM syntax stage

Public `Parse` and `ParseWithBudget` remain CommonMark-only with unchanged
signatures/defaults. The private `gfmInProgressProfile` selects implemented syntax
through their shared admission, UTF-8, budgeting, block/inline and recovery body.
`ParseGFMWithBudget` selects this exact whole profile for admitted GFM operations;
no partial profile or mutable selection is exposed. Completed status certifies
all five extensions with the same source authority and operation budget. The [tagged test adapter](testdata/gfm/profile/README.md#execute-the-actual-adapter-offline)
executes all 2,117 comparison units with separate immutable raw historical and
approved modern-target axes. Independent mixed AST/source/protection, exclusion, scope isolation, concurrency,
finite resource and semantic fuzz verification complement the complete approved
profile gate (#235). Real lint/fix/inspect integration (#161) uses this lifecycle
for original, candidate and saved snapshots. Named production rules (#236), MCP
and independently optional frontmatter remain separate work.

`Table` owns `TableProperties` (positive Width and one typed Alignment per
column), and source-ordered `TableRow` children: one header, one delimiter, then
zero or more body rows. Each row's envelope includes its original physical
ending and begins after enclosing container prefixes. Table Content retains
one exact logical source segment per row, omitting later container prefixes;
the table envelope still covers the complete original block, including gaps.
Tables and rows add no copied container payload to their descendants.

`TableRowProperties` retains Role, RawCells, Padding, every one-byte actual
separator and optional LeadingPipe/TrailingPipe spans. Original `TableCell`
children retain every field between separators, including ASCII whitespace,
actual empty zero-width fields and excess cells. Padding is a spanless count;
there are no invented source cells or truncated source children. Rendering
consumers may project Width cells and Padding empty values independently.

Cell Content trims ASCII space/tab edges. `TableCellProperties.EscapedPipes`
retains each complete original backslash-pipe spelling. An odd immediately
preceding backslash run escapes a pipe under mdchk's explicit compatibility
interpretation, chosen consistently with core punctuation-escape grouping;
GFM 4.10 does not unequivocally order longer runs. Unescaped pipes split before
inline interpretation, even between backticks or emphasis markers. Mapped Escape segments contribute `|` while owning both raw
bytes. The shared scanner, code normalization, decoder, HTML and link/reference
handlers consume those mappings without a rewritten Source or second parser.
Delimiter cells have no inline content: their Alignment is present, and exact
ordered `left-colon`, `dash-run`, `right-colon` markers select only real syntax.
Ordinary cells have absent Alignment and no delimiter markers.

Constructors validate roles, counts, width/alignment consistency, source owners,
separator/field relationships and exact delimiter markers; accessors snapshot
all nested mutable properties. Ordinary walkers, reference validation, comments,
literal spans and protected-node collection reach inlines in every cell,
including excess cells. Table, row and cell each add one syntax-depth construct.
The full operation accounting is documented in the parser limits specification.


### Task item metadata

A task retains the ordinary `List -> ListItem -> Paragraph` hierarchy and all
core list-marker, prefix, continuation, container and tightness facts. Optional
`Properties.Task` is present on the item and its first direct Paragraph only.
`TaskProperties.Paragraph` identifies that actual child by its snapshot-owned
envelope. A later paragraph or a paragraph inside an initial quote/nested list
cannot qualify the parent item. Core reference extraction happens first;
definitions remain separate inventory records, so the first remaining rendered
block qualifies only when it is a direct Paragraph. Removing task syntax never
reparses the body into new blocks or reference definitions.

`Checked` distinguishes state from exact `CheckSpelling` (`x`, `X`, or original
whitespace). `Opening`, `Check`, `Closing` and `Separator` retain the original
brackets, check token and first required following whitespace token. `Marker`
is their bracketed envelope. Ordered `Segments` coalesce adjacent marker tokens
but exclude intervening container-prefix/indentation gaps. The item list marker
(token-only `item-marker`), padded `item-prefix`, List properties and enclosing
quote/list/item context remain on their existing authoritative owners; no ancestor
payload is copied to every task property.

The extension uses its pinned ASCII whitespace class: SPACE, TAB, LF, VT, FF and
CR, never Unicode Zs/NBSP. A physical CRLF is one logical ending token whose
Check/Separator span owns both bytes. This is the explicit source-model policy
for interpreting the pinned character grammar while retaining core LF/CRLF
mapping equivalence. Marker tokens can cross logical lines and original source
gaps, but cannot cross a real core paragraph/block boundary. For example,
`- [\n  ] foo\n` has Opening[2,3), Check[3,4), Closing[6,7),
Separator[7,8), marker segments[2,4)+[6,7), and payload[8,11).
`> - [\r\n>   ] é\r\n` has Check[5,7), Closing[11,12),
Separator[12,13), segments[4,7)+[11,12), and payload[13,15).
Removed quote/item prefixes are gaps, not extra marker whitespace.

The Paragraph's raw envelope stays unchanged, including task syntax, remaining
leaf indentation, physical terminal ending and later container gaps. Logical
Content excludes the marker and only the first required separator token; any
additional original spacing remains Content subject to ordinary paragraph-final
ASCII trimming. The existing shared inline scanner handles the suffix. For
`> - [X] é\r\n`, Paragraph remains[4,12), marker[4,7), separator[7,8),
and Content/Text é[8,10); the original ending[10,12) remains in the envelope.
A required terminal space/tab or physical ending proves the separator even when
core final trimming omitted it from Content. Thus `- [x]` at EOF is ordinary,
while `- [x] ` and `- [x]\n` retain a task Paragraph with empty Content and no
fabricated Text/break. A line-ending separator before continuation payload is
consumed whole, leaving no initial SoftBreak or container-prefix payload.

Constructors validate applicability, same-snapshot containment, exact token/state/
spelling, whole ending tokens, canonical marker segments, required separator and
item/actual-first-child agreement. Nested metadata/slices are copied at construction
and access. As with other model facts, valid construction does not prove parser
recognition or grant source-edit/protection authority. Ordinary walkers, reference
binding, Comments, LiteralSpans and ProtectedNodes continue through the real
paragraph inline children. Checkbox rendering belongs to the tagged test adapter;
the product parser emits no HTML. Task metadata adds records/work, not a syntax
construct beyond the existing list/item/paragraph/inline depth.


`Document.TaskSources(ctx, budget)` collects one source-ordered record per actual
task Paragraph from a completed parse in the same operation. Each owned record
contains copied TaskProperties (including marker segments) and logical Content
contributions. Core parses return a known empty inventory. Constructed models,
partial parses and foreign operations cannot grant this authority. Traversal,
stack and record construction use that operation's work/node quotas and context.

MD047 consumers use these real facts to prevent a newly inserted EOF ending
from adopting literal task spelling. Eligibility parses a prospective ending
with the actual whole GFM profile on the same budget. Candidate validation
independently compares the actual before/after inventories, marker coordinates
and spelling, and visible Content coordinates, transformations and semantics,
alongside its exact suffix/body and common protection/directive proofs. Paragraph
envelope endings may extend, but task tokens and visible body cannot change.

### Strikethrough nodes

`Strike` (`strike`) is transparent inline formatting. It reuses
`DelimiterProperties` with Marker `~` and Width 1 or 2, exact `opening`/`closing`
Markers, ordered inline Children and original logical Content between markers.
Content retains nested markup and actual physical endings, excluding stripped
container prefixes; it has no aggregate Literal. Unmatched tildes remain Text.
Both arities add one syntax-depth construct. Public core parsing remains unchanged.

NewNode requires delimiter metadata, exactly two correctly spelled same-arity
markers at the envelope endpoints, and same-source Content/children inside the
marker interior. Nonempty interiors require Content reaching both boundaries;
container gaps between segments remain permitted. Payload segments cannot
reorder/overlap or be spanless. Construction snapshots the existing mutable
metadata, markers, Content and child handles. These checks validate supplied facts,
not Markdown flanking, recognition, completed availability or edit authority.
The [explicit pairing interpretation](../../docs/engineering/gfm-compatibility.md#strikethrough-pairing-interpretation)
records the chosen behavior where pinned extension prose is incomplete.

Walk, reference binding, Comments, LiteralSpans and ProtectedNodes reach actual
inline descendants. Strike itself grants no opaque-source authority. Code and
raw HTML remain opaque under the shared parser's ordinary boundaries. The tagged
test adapter renders Strike. Real operation consumers use its original facts;
named rule coverage remains separate from verified parser conformance.


### Bare extended links

The private stage adds resolved `LinkProperties.Origin="bare-gfm"`; the existing
string field and all legacy origins remain unchanged. Angle links retain origin
`autolink` (the fixture projection calls that origin `angle`). A bare envelope is
exactly the visible source envelope, without invented angle/bracket markers.
Content, Label and the single Text child retain the same original mapped
contributions and visible semantics. Destination is independent: a recognized
www web form prepends spanless Synthetic `http://`, plain email prepends
spanless Synthetic `mailto:`,
and explicit HTTP(S)/mailto/xmpp protocols retain source-backed spelling once.
Table pipe escapes and normalized NUL retain their atomic original provenance;
ordinary raw URL escape/entity spellings are not decoded into fresh syntax.

NewNode narrowly checks bare applicability, Available resolution, absent title,
reference binding and markers, nonempty owned visible payload, ordered nonoverlap,
aggregate semantic contributions, matching Content/Label/Text child and destination
contributions with only an allowed supplied source-absent prefix association. A
www-looking local part can carry mailto when email recognition wins; visible www
spelling alone does not select the association. Synthetic http requires www,
Synthetic mailto requires an at-sign in the visible address, and explicit source
protocols do not acquire another prefix. These checks do not recognize
Markdown grammar or confer parsed completion/protection/edit authority. Existing
immutable snapshots and detached accessors apply; equal bytes in another Source
are not ownership. NewDocument binds only the existing reference origins.

One shared structural inline scan resolves code, HTML, angles, brackets,
references and images. A dedicated raw extension pass then visits eligible flat
Text intervals before final emphasis/Strike pairing, retiring only delimiter bytes
covered by accepted links. Failed raw angle sites are recorded explicitly for
approved 611; escaped angles never gain that exception. Approved 615 rebuilds its
remaining raw prefix before decoding once, avoiding an illegal split inside the
already decoded Escape contribution. Changed nonlink remainders carry the shared
scan's terminal Normalized/empty discarded-space contributions unchanged, while
preceding raw escapes/entities decode once. Successful explicit labels remain
excluded.
Image labels retain original flat facts and live delimiter copies; only images
outside successful Links are rebuilt, in closing order, before outer pairing.
No second Markdown parser or pass over concatenated decoded Text is involved.
The [compatibility interpretation](../../docs/engineering/gfm-compatibility.md#extended-autolink-interpretation)
records primary ambiguities, exact classes, trimming and source-priority choices.

Bare links remain transparent to source consumers. Walk/reference traversal and
protection still reach real code/HTML descendants elsewhere; bare syntax itself
adds no opaque range. Only complete same-operation documents grant authority.
Public Parse/ParseWithBudget/zero/core stay CommonMark-only. The five-extension
whole-profile entry point supplies GFM to real operation consumers, without
production rendering or additional rules. The tagged test adapter executes all 2,117 comparison units
with separate raw historical and approved modern-target results; see the
[actual-run contract](testdata/gfm/profile/README.md#execute-the-actual-adapter-offline).
Independent profile/resource verification (#235) and real operation-consumer
integration (#161) complement this adapter.


### Disallowed raw HTML metadata

Existing `HTMLProperties.FilteredTags` contains source-ordered `HTMLFilteredTag`
records on the actual `HTMLBlock` or `InlineHTML` owner. No new Kind, nested
syntax, source rewriting or sanitizer is introduced. Each record has canonical
lowercase `Name`, `Closing`, an always-present mapped `Prefix` for original
`<[/]name` excluding the boundary, and one-byte original `Affected` angle.
Optional `Tag` selects a complete construct only when the shared HTML lexer
proves it. A malformed raw prefix such as `</script/>` or unfinished `<script `
can have a fact with nil Tag. EOF immediately after the name does not qualify.
Valid published fixtures retain full tag ranges, including multiline attributes.
The [explicit filter interpretation](../../docs/engineering/gfm-compatibility.md#disallowed-html-interpretation)
defines exact boundaries and actual comment ownership.

The owning raw node's unchanged envelope is the protected owner. Logical Content
and its omitted container gaps remain authoritative; a fact cannot select an
angle merely because it is inside the envelope. NewNode validates canonical
identity, physically contiguous original fixed prefix and immediate boundary,
exact angle, strict affected ordering,
and indexed same-snapshot Content contributions. Prefix contributions are Exact;
Tag contributions preserve complete atomic NUL/table Escape/PartialTab mappings
where supplied. Overlapping complete Tag ranges are allowed, independently of
strictly increasing affected positions. Constructors validate supplied coherence,
not shared-lexer recognition or private parse completion. Legacy absent/empty
HTML properties retain their existing behavior. Outer slices, Prefix/Tag segment
slices and optional Tag pointers are deeply detached at construction and access.

A private post-syntax pass adds metadata without changing Children, raw forms,
comment markers, private tail inventories, reference binding or literal/protected
consumer authority. InlineHTML is atomic; HTMLBlock visits its logical payload,
excluding only existing confirmed comments through their actual close or EOF.
Tail facts occur once on the enclosing block. Opaque comment-looking strings do
not acquire comment ownership or directive eligibility. Public Parse and
ParseWithBudget remain CommonMark-only. Independent profile/resource verification (#235) now complements the private
stage. Real consumer integration (#161) now uses the same whole profile and
source authority. The delivered tagged adapter (#234) executes
all 2,117 units with separate immutable raw and approved modern-target comparisons;
its [actual-run contract](testdata/gfm/profile/README.md#execute-the-actual-adapter-offline)
retains these boundaries and distinguishes integrity-only NOT-RUN checks.


## Derived heading facts and source identity

`Document.Headings(ctx, budget)` exposes completed-parse Markdown heading facts
for a resource index. It requires the original operation budget witness, as does
`HTMLIDs`; constructing nodes does not establish that witness. Facts preserve the
original heading envelope, level/style, parsed visible text and quote/list
ancestry. `HasRawContent` records whether parser-trimmed raw Content has a
contribution, independently of Visible. For nonempty ATX content only,
`OpeningSeparator` spans the actual opening marker end to the first raw Content
source byte. Empty ATX and Setext separators are absent. Marker and first-content
inspection and scalar/span construction share the original operation meter; no
public tree or content collection is cloned for these facts.
Text/code, formatting, labels and image alternative children contribute
once; soft/hard breaks contribute a newline. Raw tag spelling contributes none,
and protected raw text is not reinterpreted as Markdown. This API does not
allocate anchors or define TOC eligibility. Storage is detached and metered; a
stop returns no partial collection. Default parsing and node recognition are
unchanged.

Closed ATX facts also expose `ClosedOpeningGap` and `ClosedClosingGap` as
original source spans between actual markers and first/last raw Content source
contributions. NUL normalization may split contributions; the final source end
remains authoritative. Raw-empty closed ATX exposes only `ClosedOpeningGap`, the
shared marker gap. Open ATX and Setext expose neither. This leaves all existing
fields unchanged, including absent `OpeningSeparator` for raw-empty headings.
The fixed extra gap work shares the actual completed original operation's work,
node and elapsed controls; interrupted projection publishes no partial inventory.
MD021 scans only these confirmed gaps and grants no correction authority. See its
[exact spacing policy](../../docs/rules/headings/md021.md).

`Source.Token()` is a constant-size comparable opaque snapshot-owner identity.
Copies of one source share a token; separately constructed equal bytes do not.
The zero source differs from a real empty source. Tokens expose no bytes, span
construction, budget witness or content admission authority. They allow a
service to retain completed facts after a stop without hashing source contents.


## Opening-hash candidate projection

`Document.OpeningHashCandidates(ctx, budget)` returns original zero-width points
for MD018. During ordinary paragraph append, the actual blockLine retains a
bounded one-to-six-hash lexical run only below four structural indentation
columns. Final ordinary-paragraph closure publishes surviving runs after reference
extraction; Setext/table promotion discards them. The completed projection uses
one metered tree traversal and indexed run-overlap queries to exclude code/raw
HTML/literal regions and link destinations/titles before publishing points.
It does not infer line starts from normalized Content or change recognition,
AST, rendering, headings or other consumers. Construction, traversal, stack
copying and output share the original operation and source witness; a stop
returns an error and no partial inventory. Constructed or incomplete documents
and foreign budgets cannot establish authority. The engine's narrow
`RuleContext.OpeningHashCandidates` bridge additionally requires reliable syntax
and observes both callback and original operation controls.

### Terminal ATX candidate projection

`Document.ClosingHashCandidates` requires the actual completed parse and its
original operation. It projects zero-width original points before maximal raw
terminal hash runs in open ATX headings, with no closing-width cap or matching
requirement. Raw first/last Content bounds survive normalization contributions.
Odd escape parity excludes the whole maximal run conservatively. Genuine closing
markers and whole-run protected ownership exclude candidates. The shared hash
protection traversal covers code, raw HTML/comments, link literal values and
active raw-text regions without cloning public trees. Every scan, overlap and
retained construction shares original work/node/time controls; interruption
publishes no partial inventory. Existing HeadingFact fields are unchanged.

## Physical heading separation projection

`Document.HeadingSpacing(ctx, budget, index)` requires the completed original
parse, its original operation budget and a physical LineIndex bound to the same
Source token. It returns detached source-ordered HeadingSpacingFact records,
without changing HeadingFact or existing semantic heading/anchor consumers.
Each record has zero-width points at first physical Content.Start and last
physical Content.End, adjacent consecutive blank body counts and actual-document
edge flags. A terminator contributes no extra selectable content line. The
exclusive heading envelope end is not interpreted as a following heading line.

One bounded traversal reuses only actual quote-prefix, item-prefix and item-indent
markers. Per-line maximum prefix ends handle shared partial-tab bytes; residual
tab columns are ASCII whitespace. Lazy prefixes and marker-looking prose are
never synthesized. Each original body suffix is tested only for ASCII space/tab;
NBSP and NUL are content. Finite forward/backward line passes compute adjacent
runs across container transitions, independent of options. Opaque header/source
lines remain physical neighbors. No source, parse, budget or public tree clone
is created. Reservations precede line storage, traversal, marker lookup, byte
scans, amortized output growth and records (including both possible sides); loops checkpoint the original and
caller controls. Failure, including the final check, withholds the inventory.
The MD022 callback separately retains reliable earlier side findings on a later
interruption. See [MD022 policy](../../docs/rules/headings/md022.md) and
[accounting](../../docs/engineering/parser-limits.md).

MD023 retains a private positive leaf-indentation aggregate while the block parser
still owns each line's actual post-matched-container cursor, including partial
tabs, lazy prefixes and deep Setext continuations. Leading reference extraction
recomputes the aggregate from surviving retained paragraph parts. Only actual
heading dispatch retains one original opening/underline marker; paragraph and
table ownership publishes none. Existing HeadingFact and MD022 facts are unchanged.

Private aggregate updates use the existing metered line visits; retained markers
use metered append reservations. The narrow IndentedHeadings projection requires
the completed original parse, source and operation. It charges each copied marker,
record and amortized slice growth before publication and withholds the inventory
on failure. RuleContext joins caller and originating controls; the callback
charges each finding and retains reliable prior findings with a later error.
There is no new parse, source, allowance, whole-document rescan or public-tree copy.
This is finite application-work accounting, not a process memory or throughput
guarantee. No correction provider or write authority is added. See [MD023 policy](../../docs/rules/headings/md023.md).

MD024 adds a raw Label span to HeadingFact without changing existing fields.
First/last original Content source bounds preserve NUL mappings and multiline
Setext gaps; raw-empty ATX uses opening.End. The original-operation HeadingKey
bridge collapses parser.IsUnicodeWhitespace and uses pinned Unicode17 FoldCase
without decoding again. All scans, builders and fold expansion reserve work and
construction before allocation, with long-loop checkpoints. The callback reserves
map capacity for the complete finite heading inventory before construction, so
ordinary insertions do not grow the map or rehash prior variable-length keys;
whole-key lookup/comparison, key retention, fixed outline state and output growth
are separately charged. One linear heading pass uses actual occurrence IDs for
nearest-shallower parents, preserving all original context across suppression.
Projection failure withholds facts; a later callback error retains prior reliable
findings and explicit incomplete coverage. No fresh source, parse, allowance or
public-tree clone is created. This is finite application-work accounting, not a
process memory or throughput guarantee. See [MD024 policy](../../docs/rules/headings/md024.md).

MD026 adds HeadingTerminals, a narrow provenance projection sharing the exact
semantic walk with HeadingFact.Visible. Existing Visible and Label contracts stay
unchanged. It selects one final non-whitespace scalar before code/escape exclusion;
Exact text selects original rune bytes and transformed tokens remain atomic.
Code text participates in selection, so it cannot expose earlier punctuation.
The projection borrows actual immutable inline segments under the completed
original parse and allowance, charges tree/segment/rune visits and output growth,
and withholds all facts on failure, including final publication checks.
RuleContext joins caller and origin controls; the callback reserves scalar-set
construction, membership and finding growth, retaining reliable prior findings
with a late interruption. No source/parse/index/allowance/tree clone, correction
provider or write authority is added. This is finite application-work accounting,
not a process memory or throughput guarantee. See [MD026 policy](../../docs/rules/headings/md026.md).

MD036 adds EmphasisHeadings, a narrow completed-parser projection of actual
one-line emphasized Paragraph labels and their immediate same-parent prose
followers. It borrows immutable internal nodes and the original physical index;
actual retained container prefixes and one cumulative nonblank-line pass prevent
jumping source-only reference definitions. Candidate descendants and follower
ordinary Text use actual parser ownership, including task and raw-region
exclusions. Original outer envelopes retain both delimiter markers.
All line arrays, tree/marker/line/rune visits, binary searches and retained output
growth share the original work/node/time allowance. Caller/origin controls and
final withholding apply; callback interruptions retain reliable prior findings.
No fresh source, parser, index, budget, public-tree clone, edit or write authority
is added. Accounting bounds application work, not process RSS or throughput.
See [MD036 policy](../../docs/rules/headings/md036.md).


MD041 adds OpeningBlock, a narrow original-operation projection of the first
actual root body block after fully comment-owned introductory blocks. It borrows
actual comment/tail and raw-protection facts and scans only uncovered ASCII
whitespace gaps, without copying comment payload or a public tree. It retains
one original zero point/heading-level record; empty bodies use original EOF.
Root/comment/marker/source-gap visits and the fixed output record are charged
before work/publication. Caller/origin and sticky work/node/time controls remain
shared; any failed projection withholds its fact, including at the final check.
The callback short-circuits a sufficient opening heading before metadata decode;
otherwise opt-in title truth supplies an alternative or explicit affected
coverage. No Source, parse, index, allowance, edit or write authority is added.
Finite application accounting is not a process-memory or throughput guarantee.
See [MD041 policy](../../docs/rules/headings/md041.md).

MD043 compiles its exact single-line descriptor list through HeadingOutline after
reserving independent entry storage, without Options' eager list clone or mutable
option borrowing. An additive semantic list domain rejects invalid entries at
admission, with per-element source locations even while disabled. Empty outlines
avoid heading projection and comparison workspace.
For nonempty patterns, existing HeadingFact level/Visible/Label/Envelope facts
supply the whole source-ordered sequence. A checked suffix cost/action grid reserves
both arrays under the original allowance before allocation; cells, transitions,
chunked repeated exact byte comparisons and canonical reconstruction are charged.
Worst-case matching uses O((patterns+1) * (headings+1) * (label-bytes+1)) work
and O((patterns+1) * (headings+1)) state, rather than unbounded wildcard backtracking. Overflow or exhausted
original work/node/time controls produces explicit incomplete analysis, not a
guessed mismatch. The completed-original parser/index witness and compound
caller/origin checks also apply to empty option access. Failed alignment withholds
its observation through the final check; existing late-operation handling retains
one already justified finding. No source, parse, index, budget, public tree clone,
second Visible definition, correction or write authority is introduced. These are
finite application controls, not hard RSS or OS preemption guarantees.
See [MD043 policy](../../docs/rules/headings/md043.md).

MD063 reuses the shared heading Visible walk with independently owned scalar,
code-owner and exact/atomic source-token records. Proper-name list storage is
reserved before copying; immutable string bytes may be borrowed without exposing
mutable engine options. Name protection is settled before selecting a dependent
initial, with repeated scalar comparisons, protection writes, word visits and
finding records charged to the original operation. Worst-case matching work is
O(heading-scalars * names * name-scalars); retained state is linear in projected
heading scalars and option-list entries, bounded by the original work/node/time
allowance rather than a new name cap. Caller/origin cancellation and completed
private parse/source/index identity remain required, including final withholding.
Already justified earlier-heading findings follow existing late-error handling.
No Source, parse, index, allowance, second renderer, public tree clone or correction
authority is added. These are application accounting controls, not hard RSS or OS
preemption guarantees.
See [MD063 policy](../../docs/rules/headings/md063.md).

MD080 uses OriginalHeadingAnchors to invoke the existing shared resource index
builder on the actual completed original parse. A private profile/options/source/
index/operation witness prevents forged public parse fields from granting facts.
Only owned heading records and availability escape; no Service/cache/view/content
witness, resource read, Root or new parse/source/allowance is introduced. Original
caller and origin cancellation, sticky work/node/time and final withholding apply.
The shared builder retains raw labels from the same HeadingFact and preserves its
whole-index uncertainty. MD080 reserves a finite seen-map capacity before allocation,
charges borrowed base bytes before hashing/equality and reserves actual output
records; immutable string bytes are not charged again as constructed nodes. Every
later present-base duplicate, including empty bases, uses the original full Label.
Local AnchorStyle defaults github, validates github/explicit and persists through
plans/candidates. This does not enable index_file or cross-file consumers. Finite
application accounting is not a hard RSS or OS-preemption guarantee.


### Empty sections

MD082 uses the parser-owned EmptySections projection under the complete original
production profile, parse options, source/index and operation allowance. One
pending actual heading and intervening body flag resolve each heading once; a
deeper next heading itself satisfies its parent. All actual container and hidden
headings participate before filtering. Only the globally last actual heading per
document can receive the boolean allow_final_section exemption. Labels share the
raw heading-label helper, including empty-label points; own inline labels are not
body. Semantic whitespace, actual object presence and confirmed comment ownership
follow the [MD082 policy](../../docs/rules/headings/md082.md).

Traversal stack growth and derived output are reserved before allocation. Borrowed
AST visits and bounded semantic UTF-8 byte scans use work from the original
allowance; both caller and origin controls and final publication are checked.
An interrupted projection withholds its inventory atomically. Raw/comment state
uses actual private nodes and owned content/tails, excluding container prefixes.
This provider does not build an anchor index or consume AnchorStyle, metadata,
resource availability, correction, write or MCP authority. MD082 remains default
disabled and report-only; MD047 retains the sole production correction.


### Thematic breaks before headings and explicit slides

MD090 borrows direct actual body-block siblings under the complete original
production profile/options/source/index/budget witness. An iterative reserved stack
keeps each container sequence separate; owned first/last thematic markers provide
one original range without raw scanning. Comments interrupt this adjacency. Labels,
traversal/output storage and final publication share original operation controls.

A narrow immutable per-path slide provider stays on the selected execution plan,
including candidate and saved views. Configuration owns the existing direct bounded
matcher, first match and exact origin; adapters bind invocation paths to the selected
configuration directory once. The annotation follows mandatory encoding validation
and changes neither selection nor format. Option-aware applicability may use only
admitted options and explicit context before syntax; it cannot borrow partial syntax.
Known slide exemptions are retained independently of unrelated parser/directive
uncertainty. Inspection prepares context before callbacks and preserves truthful
completed sections when a later operation stop occurs. MD090 stays default disabled
and manual; MD047 remains the sole production correction.

## Heading inline construct projection

MD093 borrows actual inline kinds and complete source envelopes from each real
Heading using the completed private production profile, options, source/index and
original operation allowance. It walks nested wrappers independently, retains raw
HTML closer traversal and image-alt isolation, and never redispatches code, HTML,
destination or title payload. Escaped/unresolved text remains parser-owned text.
No new parse, renderer, public-tree clone, anchor index or resource service is used.

Traversal stacks, output and finding growth reserve original records/work before
construction. Caller and originating controls, sticky work/node/time stops and
final publication checks withhold incomplete inventories. The immutable admitted
allow list is read under the same controls into five fixed allowance bits; returned
facts own their slice and cannot mutate syntax or options. Nested constructs stay
independent and source geometry uses original envelopes, including container gaps.
The rule remains default disabled, RequiresSyntax and NoFix; MD047 alone owns edits.

## Original list structure projection

MD004 borrows actual List/ListItem ancestry and first-line marker facts through
the completed private production profile/options/source/index/original-budget
witness. Detached records retain group/item/parent envelopes, exact marker/prefix
spans and scalar list properties without mutable syntax handles or public-tree
cloning. Body-container preorder preserves outer-before-inner source order; code,
HTML, heading and paragraph payload is not reparsed. Later list rules can reuse
this seam without a second parse, renderer, anchor index or resource service.

Stack/fact storage and copied prefixes reserve original records/work before
construction. Caller and originating controls, sticky work/node/time stops and
final publication checks withhold unfinished inventories. MD004 selects one
per-document first-bullet baseline before directive filtering, or an admitted
explicit marker, and emits marker-only manual findings. It remains enabled by
default with PlannedFix metadata; MD047 alone supplies executable corrections.

MD005 reuses the same detached list facts. It compares structural marker
indentation (`ContentIndent - MarkerWidth - Padding`) against each actual group's
first direct item, before directive filtering. Full source-backed group spans
retain independent nested baselines without replacing an enclosing baseline.
Ordered-width and post-marker padding changes do not move marker starts.
Per-analysis baseline storage, comparisons and finding growth reserve original
work and records before construction, including aligned groups. Final checks
withhold interrupted output. Exact full item-marker spans produce manual
findings; default-enabled PlannedFix metadata grants no correction authority.

The internal lint-only ListSeparators projection reuses the original ListItems
inventory and completed production witness. It derives detached Marker,
Separator, Padding and FirstLineNonempty facts; no AST property or inspection
schema changes. The separator is [Marker.End, Prefix.End). A bounded binary
search of the original physical-line index establishes nonempty source syntax
when Prefix.End is less than that line's content end. Definitions, tasks and
later descendants do not replace this physical first-line test. Empty virtual
padding has no fabricated separator bytes. Capacity, logical records and lookup
work are reserved under the original allowance before construction; final
caller/origin checks withhold interrupted inventories.

The internal lint-only ListSpacing projection merges actual immediate source
participants in Document, BlockQuote and ListItem containers. It consumes retained
reference-definition envelopes once through the original tree, without reparsing
or public syntax changes. Only ancestor prefixes are active for each boundary;
rollback preserves target/deeper markers. List/item terminal content floors
retain empty item markers and definitions; opaque blocks and quotes retain their
entire boundary. List-owned trailing blanks can count at the parent body level.
Detached zero-width missing-side facts require the completed original production
profile/options/source/index/operation witness and share its allowance. All
visits, storage, copies and records are reserved; final failure withholds all facts.

The private MD069 source projection obtains actual ListItems once under the
completed original production witness. It keeps ordered and mixed context,
associates parents by constant source-token/offset/presence identity, and checks
same physical line before borrowing ASCII whitespace gaps. Returned maximal-run
spans are detached immutable-source values without edit authority.

The private MD076 ListItemSpacing projection traverses actual List children under
the completed production/profile/options/source/index/operation witness. It uses
ListSpacing's source-floor mechanics: source-only definitions and empty markers
are content, terminal lists/items propagate floors, quotes and opaque leaves keep
full envelopes. Only ancestor prefixes remain during sibling comparisons. Each
List frame owns its immutable exact first-count baseline; the admitted style
selects one first-extra blank suffix without retaining per-blank spans. Detached
mismatch facts carry actual/expected counts and original diagnostic ranges only.
Visits, source summaries, lookups, frames, rollback, capacities, copies and records
are reserved before construction; final interruption withholds all facts.


MD077 continuation analysis borrows the final private block/inline tree, removed
definitions and confirmed item/quote markers under the completed production
witness. It returns detached Span/Actual/Expected mismatches only; NodeSpec,
Properties and inspection schemas are unchanged. It preserves physical cursor
phase and partial tabs without retaining new original-parse observations.

MD010 consumes actual CodeBlock Content and intrinsic indent-prefix ownership,
never coarse block envelopes. Literal contributions preserve container gaps;
confirmed raw bounds additionally protect trimmed original body TABs. Charged
scalar replay of final confirmed prefix facts distinguishes fully consumed TABs
from residual body columns at original physical phase. Empty first items borrow
the actual Prefix segment widths. This projection adds no retained parser model
fields or public inspection schema.

MD012 derives detached first-excess body spans from completed original physical
lines, actual final prefixes and protected code/raw/header intervals. Empty payload
endings retain physical ownership. Global runs cross container transitions but
protected lines break them. Original source, profile, options, index and allowance
authority, precharged growth/copies and final withholding apply without new retained
syntax or inspection fields. Zero-width findings are manual evidence only.


MD064 borrows the completed original production profile/options/source/index and
operation through RepeatedProseSpaces. Actual Paragraph/Heading Text, transparent
formatting and visible labels supply semantic Unicode Letter/Number/Mark tokens;
whitespace, Cc, original NUL and opaque syntax are barriers. Only contiguous exact
original SPACE runs count; hidden link fields never supply events. Actual raw tags,
HTML tails and isolated image-alt state protect payload without whole-line hiding.
The detached surplus [start+maximum,end) remains manual. Every source/semantic
probe, traversal frame, capacity, copied prefix and logical span/finding is reserved
before work, including empty/clean/protected input. Caller and original controls,
sticky finite work/nodes/time and final producer/facade/callback checks atomically
withhold failure. No reparse, renderer, source/tree clone, retained AST, new allowance
or correction authority is added; MD047 remains the sole correction producer.


ConfiguredNames is an original-operation projection for MD044. One Text owner,
touching Exact ranges and one physical line define eligible islands. Non-Exact
semantic first/last scalars retain outside word facts without supplying match bytes.
Actual Link/Image owners, rawTag/rawClosing/htmlTail and opaque header ownership
remain authoritative. Opt-in code uses actual CodeBlock Content, never an envelope
or info string. Source scalars retain original endpoints and borrowed Unicode17
full folds once; bounded name-prefix comparisons cannot split expansions. All
arrays, failed probes, comparisons, copies/growth and detached spans reserve work
and records before use. Final failure withdraws output, including early returns.

MD065 analysis uses a narrow completed-original ThematicSpacing projection and an
optionless RuleContext facade. A full tree/inline/tail walk preserves semantic raw
lifetimes and image isolation; a separate body walk merges retained definitions
with direct source participants. Actual prefix rollback and terminal list/item
content floors preserve immediate physical separators. Detached Point/Before facts
carry present zero-width original evidence only. No source, parse, index, options,
allowance, AST inspection field or correction capability is added. Producer,
facade, comparison and whole callback reserve work/storage before use and finalize
under both caller and originating controls; any error withholds dependent output,
including empty, clean, protected-only and post-construction paths. MD065 remains
default enabled and report-only for CommonMark and GFM.


MD081 uses ParagraphEmphasis to borrow the completed private original production
source/index/options/profile and operation. Detached full Content-hull/count facts
retain actual Paragraph ownership, semantic Text provenance, Emphasis/Strong
boolean union and full raw-tag/closing-tail lifetime. Image descriptions are
opaque and cannot change outer raw state. No renderer, reparse, public AST/schema,
source/index/options clone, resource/metadata or correction authority is added.
Every traversal, terminal probe, provenance/byte/scalar/raw/option test, capacity,
copy and fact/finding reservation precedes work under the original finite controls.
Unicode table shape is guarded before bounded scalar classifications. Producer,
getter, facade, comparison and distinct whole-callback final checks bind caller
and origin and atomically withhold all dependent output on error, including empty,
clean, protected, minimum-skipped and post-output paths. MD081 remains disabled
and manual; MD047 alone produces corrections.


## MD088 original ASCII punctuation analysis

ASCIIPunctuation borrows the completed private original parse, source, production
profile, options, physical index and operation budget. It returns detached scalar
ranges only: actual Paragraph/Heading/GFM prose-cell Exact Text, transparent
formatting, all Link findings excluded with global Link raw transitions, and fully
opaque Image children/tails. It follows private rawTag/rawClosing/htmlTail lifetime
without resetting at owners. Code, comments/attributes, headers and retained
definitions stay protected. Entity/Escape/Normalized/PartialTab contributions
supply no candidates; Exact bytes are validated against original source.

Reservations precede structural and terminal visits, source/provenance checks,
byte association, bounded UTF-8 decode and fixed-six classification, raw comparisons,
stack/output growth, copied prefixes and retained facts/findings. Producer, style
getter, facade, comparison and distinct outer callback final gates retain caller
AND originating controls and sticky work/node/elapsed limits; any error atomically
withholds all dependent MD088 output, including empty/clean/protected/post-output.
No reparse, renderer, public-tree/source/index clone, new allowance or calibration
is used. Finite application accounting is not a CPU/RSS or throughput guarantee.

Exactly one original three-byte/one-code-point range per eligible scalar uses
`Use ASCII quotes and dashes in prose.` Full context precedes shared START
suppression and inspect intersection. Default OFF/style ascii/PlannedFix remain;
this pass creates no MD088 edits, previews or corrected content. MD047 is sole fixer.

## MD033 embedded HTML inventory

`EmbeddedHTMLTags` borrows completed private original production syntax, source,
profile, parse options, physical index and operation. It returns detached Tag/Name
spans and explicit availability. Actual HTMLBlock/InlineHTML owned content is
lexed with existing mapped CommonMark machinery; whole Images are opaque. Complete
named tokens in form-6/7 owners provide local transition proof. Independently
owned InlineHTML requires private rawTag/rawClosing proof under incoming raw state.
Opaque tokens and quoted attributes cannot create a cross-owner closer. Form 1
retains the parser's literal any-of-four closer boundary. Full owners include
closing tails exactly once; no public-tree clone, new parse, renderer, source,
index, allowance, decoder or browser/foreign model is added.

Traversal, failed/terminal scans, names, quotes, mapping/association bytes, raw
comparisons, stack/output capacity, copied prefixes and logical records reserve
original work/storage before use. Existing lexer terminator indexes prevent
repeated failed suffix scans; the first failed owned named candidate atomically
withholds the inventory with context-unavailable coverage. Empty, clean, protected,
allowed and post-output paths observe caller AND origin controls and sticky
work/nodes/elapsed limits through producer/getter/facade/comparison/distinct outer
final publication. The detached allowlist getter preserves the permissive string
list domain; equal-length whole-name ASCII comparison has no trim/Unicode fold.
MD033 remains default OFF, RequiresSyntax, UsesFrontmatter=false and NoFix; MD047
alone supplies corrections. Finite application accounting is not a hard CPU/RSS,
throughput or OS preemption guarantee.

## MD091 literal Markdown candidates

`HTMLMarkdownCandidates` borrows completed private original production syntax,
source, physical index, options, profile and cumulative operation allowance.
Its detached internal Marker/Extent/Kind records identify manual opening markers
and the complete extents consumed before suppression or inspection selection.
Only actual HTMLBlock mapped payload supplies candidates. InlineHTML retains
incoming raw protection, Image alternatives are isolated, and complete owner
content includes private closing tails exactly once.

The finite flat emphasis/link/ATX grammar is documented on MD091. Token/attribute
union protection is independent of actual form1 ANY4 literal termination;
lexically entered raw lifetimes require corresponding confirmed closers and
survive owner boundaries. Opaque forms2–5 retain actual delimiter semantics.
Only contiguous Exact original contributions form candidate islands; physical
endings, gaps, known transformations and control scalars are barriers. Known
unclosed raw/opaque context protects its remainder; genuinely unknown plausible
named-token boundaries make the atomic inventory unavailable. Impossible colon
boundaries do not create uncertainty.

All scratch, association checks, scalar visits, failed probes, reverse indexes,
capacities, copies and logical records reserve original finite work/storage
before use. Reverse delimiter/label/destination indexes avoid repeated suffix
scans. Producer, optionless facade, comparison and distinct outer callback final
checks retain caller AND origin controls and withhold every provisional prefix
on failure. No new parse, Source, LineIndex, public tree, model/options clone,
allowance, resource authority or correction capability is introduced.
