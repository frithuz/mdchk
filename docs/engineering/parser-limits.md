# Parser resource accounting

The parser implements the six parser-layer budgets of the accepted
[shared profile](limits.md): `document-bytes=1048576`, `syntax-depth=256`,
`document-nodes=250000`, `total-nodes=2000000`, `work-units=250000000`, and
`operation-time=60s`. These are finite application-work limits, not a process
memory or throughput guarantee. Wire admission, aggregate acquisition,
filesystem, rules, metadata, reporting and CLI/operator option parsing belong
to their future owning adapters; this parser does not implement those systems.

`Parse(ctx, source)` starts a private default operation. Callers that own an
operation instead construct `NewOperationBudget(limits)` and call
`ParseWithBudget(ctx, source, operation)` for each document, retry or corrected
candidate. The same operation's `ChargeWork` charges non-parser adapter work.
Every parse resets only its document construction counter. Work, total
construction and monotonic elapsed time continue from operation construction.
The accepted initial worker count is one: simultaneous parses and adapter
charges sharing one operation serialize. Independent operations execute
concurrently without mutable global parse state. An operation is a pointer
handle and must not be copied after use.

`Limits` values are copied and validated before use; all fields must be
positive. Document construction cannot exceed total construction. Validated
implementation maxima are depth 4,096, total construction 2^40, work 2^60,
and document bytes `min(1 GiB, MaxInt/16)`. The latter leaves room for bounded
semantic expansion and intermediate integer arithmetic on the host. Durations
are positive `time.Duration` values. Counter addition saturates before it can
wrap; no zero/unlimited or ambient environment/project override exists.
Changing defaults requires the accepted contract review, not a parser option.
The context-free source/model constructors and accessors are independent APIs;
they do not claim to share a parse operation. In particular, `NewSource` owns
its initial snapshot copy before Parse begins. Adapters must perform acquisition
admission before constructing an oversized snapshot.

## Admission, outcomes and depth

Document size is admitted before any UTF-8/line scan or size-dependent
parser allocation.
An oversized malformed snapshot reports `document-bytes`; it is not scanned to
find an encoding failure. For admitted snapshots, malformed UTF-8 retains its
typed original-byte failure ahead of cancellation/deadline. Validation itself
is work-budgeted, so a prior operation exhaustion or an insufficient validation
allowance can interrupt before encoding is established.

All runtime budget exhaustion produces `Incomplete`/`Partial`, nil `Failure`,
`Interruption=LimitExceededReason` (`limit-exceeded`) and one whole-source
uncertainty region, including `[0,0)` for empty input. `EffectiveLimits` reports
the copied profile. The detached typed `Limit` supplies `Name`, `Observed` and
`Allowed`; time amounts are nanoseconds. The interrupted phase remains explicit.
The fixed outcome envelope/root is available even if admission or validation
stopped before analysis began. Reference availability and private complete
handoffs are withheld. An unfinished inline tree never replaces the completed
block tree. Exhaustion never invents literal/flattened syntax or an internal
failure; complete status requires both successful phases and a final checkpoint.
Operation work/total/time exhaustion is sticky. Document-size/depth/construction
exhaustion stops that document; it does not refund cumulative operation work.
Mutating a returned result, profile or typed error cannot change the operation.

Depth counts simultaneous nested nontext syntax constructs. Document, Text and
soft/hard breaks add zero; paragraph/heading/code/HTML and each quote, list,
list item, emphasis, strong, link, image and code span add one. A paragraph
containing 255 nested Strong nodes is depth 256; 256 quotes containing a
paragraph is depth 257. A list plus its item contributes two ancestors.
The GFM profile counts Table, TableRow and TableCell as one
construct each; inline syntax adds to those three ancestors and any enclosing
quotes/lists/items. Delimiter cells occupy the same depth as ordinary cells.
Reference definitions occupy a leaf's construct position while recognized.
Unmatched bracket/delimiter runs are candidates, not fabricated syntax depth.
Container and leaf admission checks run before stack pushes/leaf metadata;
inline ancestor contexts combine block and inline depth. Explicit emphasis
frames check before descent; immutable subtree-depth metadata checks link/image
and reconstruction boundaries before allocating a new node.

## Metering specification

Units are conservative application-work charges, not elapsed CPU instructions.
One scanner/candidate/structural visit costs at least one unit. Byte scans,
comparisons and copies additionally cost their inspected length or a documented
upper bound before a bulk library operation. Checks include unsuccessful
candidates, provisional builders and reconstruction, not only retained final
AST traversal. Constructor/model source records reserve eight structural units
per record to cover validation passes and freezing, including empty spans; byte
comparisons/output are charged additionally. Unicode folding charges each
scalar's source byte width and folded output, not only its scalar visit.

| Site | Charged work and construction |
| --- | --- |
| UTF-8 and physical-line indexing | Reserve the full validation/byte-scan length before scanning; malformed-offset localization charges decoded bytes again. Physical-line records count before append. Long line scans check cancellation/time every 256 bytes. |
| Block classification and containers | Meter recognizer loops, indentation visits, failed candidates, quote/item pushes and lazy-reference lookahead. Provisional open/closed block records, markers, blank/definition spans and reference-line records count before retention. |
| Semantic mapping and decoding | Reserve full NUL provenance scans; charge decoder/token lookahead and copied semantic bytes. Segment appends and model-owned segment copies count as source records. Escapes/entities/full Unicode folding charge inspection, output and candidate visits. |
| Inline indexes and recognition | Charge backtick/HTML/destination index construction, failed link/reference/HTML scans, delimiter/bracket records, pairing searches/forest records, scoped retirement, source slicing and text splitting. Index integer storage costs initialization/copy work; it is not a semantic node. |
| Node/reference construction | Charge every node and definition, including provisional block nodes and replacement inline/container nodes. Charge source-record slices, markers, Values and copied/compared semantic/raw lengths before constructor validation and freezing. New immutable handle copies do not construct semantic nodes; their slice/handle copying still costs work. |
| Grouping, sorting and validation | Charge grouping/boundary visits, binary-search/sort comparisons, reference key hashing and projection comparison lengths. Final reference-binding validation is metered independently of recognition. |
| Bulk library operations | Charge scan bounds before Contains/Index/ContainsRune, prefix/suffix comparisons, case conversion, replacement and Builder writes. Replacement reserves expanded output before allocation; fixed CommonMark token lookahead remains bounded. |

`meteredAppend` charges retained records before growth; `meteredSlice` charges
initialization/reservation work before allocation. Slice/Builder growth also
charges the existing prefix that must be copied before entering the allocator. Node/reference constructors
charge validated source records before their immutable copies. Work is never
refunded after a failed candidate. The counters use checked arithmetic and stop
before an allowance is exceeded. Fixed-size interruption reconciliation is
outside the exhausted analysis allowance; it performs no further Markdown work.

Monotonic time is checked on operation entry/exit, at bulk scan checkpoints,
and after at most 256 charged units. Bulk standard-library calls are precharged
and bounded by admitted input; this is cooperative interruption rather than an
OS-level hard deadline or RSS guarantee. Cancellation/deadline checks also run
through block/inline loops and private publication boundaries. The private typed
unwind is recovered only at Parse's boundary; unrelated panics propagate.

Boundary/concurrency tests cover exact construction/work allowances, actual
1 MiB admission, 255/256/257 mixed syntax depth, source-line records, shared
reparses/adapter charges, typed detached limit identity, tiny work allowances,
invalid profiles/overflow and deterministic elapsed-time expiry. The
[benchmark record](parser-performance.md) includes reproducible timings,
allocations and measured hardening overhead; no machine-specific threshold is
claimed.

The private table/task stage uses the same admission, validation, phase recovery and
operation as public core parsing. It charges provisional row/cell specifications,
separator/escape spans, alignment and property records, raw mapping segments and
immutable copies, including failed delimiter/width candidates. Complete source
cells are retained even beyond semantic table width. Padding is one spanless
count per row, avoiding width-times-row synthetic allocations. Candidate rows
freeze only after header/delimiter equality and combined depth admission. Public
legacy entry points keep their existing core-only behavior; `ParseGFMWithBudget`
selects the complete approved profile through this same budgeted lifecycle.


Task recognition inspects at most four original logical tokens on the first
direct Paragraph, after core reference extraction. Existing segmented paragraph
mapping excludes later container prefixes; the final untrimmed line supplies
terminal separator evidence without copying or reparsing a suffix. Metadata,
original token/segment spans, rewritten Content segment slices, constructor
validation and immutable copies are charged before allocation/freezing, including
failed candidates. Fixed token validation checks at most two bytes per token and
three canonical marker segments; it never scans an aggregate marker's source gap.
Task metadata adds no syntax-depth construct. Real list/item/paragraph and shared
inline descendants keep their existing combined depth and operation authority.


The private strikethrough stage shares the existing inline scanner, delimiter
stack and pair forest. Maximal tilde runs are scanned once; runs of three or more
create no delimiter records. Single/double failures use separate arity floors,
independent of core modulo-three classes. Successful matches consume complete
runs and retire intervening records; label-local cleanup never revives retired
candidates. Pair/forest/source records and new Strike construction frames are
charged before allocation. Temporary delimiter metadata/markers and frozen
metadata/marker/Content copies are charged before construction. Narrow Strike
validation inspects at most four marker bytes and visits only supplied payload
segments/children; it never scans the full envelope across container gaps.
Strike adds one syntax-depth construct. Overlapping nested raw Content remains
real retained output and is charged for each owner, rather than asserting an
unconditional linear bound for arbitrarily large nested output.


The private bare-link pass uses one charged endpoint/prefix-fact index per eligible
original Text interval. Domain/local/resource/path endpoints, dot/underscore and
empty-label facts, parenthesis counts, unconditional punctuation/entity trim
endpoints and terminal closing chains make even failed overlapping starts
constant-work queries. Global closing-parenthesis ordinals select the required
trim endpoint without walking a shared suffix. Domain validation follows the fully
trimmed survivor. Successful candidates consume their range monotonically;
trimming never recounts a whole prefix or copies shrinking strings. Invalid maximal
email tokens advance past the failed token rather than retrying every suffix.
All index, match, image-label and delimiter-copy records reserve work/construction
before allocation; original mappings, semantic prefix builders, replacement Text,
Link properties, validation and immutable copies are separately charged.

The structural scan records actual failed-angle sites in a charged logical-byte
bitmap. Deferred image records borrow immutable raw/flat facts and copy only live
label delimiters; successful enclosing Links exclude their image labels before
bare recognition. Image reconstruction follows structural closing order and uses
the shared pairing engine. Delimiter retirement and eligible intervals advance in
source order. Bare Link adds one real syntax-depth construct; its Text child adds
zero. Ancestor/subtree checks, shared sticky operations and private final inline
publication remain unchanged. Nested source payload amplification is charged per
owner rather than hidden behind an unconditional linear-output claim.


Supplied bare-prefix association validation reserves the complete visible-label
scan bound before checking an email at-sign; it does not run another grammar
recognizer. Changed Text remainders borrow only the structural scan's terminal
Normalized/empty discarded-space facts. Their reverse suffix visits and original
boundary lookup are metered; appending their source records and immutable copies
charges before allocation. Earlier raw remainder decoding and615 priority remain
unchanged. This adds no new persistent metadata or mutable scanner cache.


The private HTML filter metadata pass runs after both syntax phases and before
final completion authority. It scans logical raw payload monotonically with a
fixed nine-name comparison bound, retaining original mappings and actual comment
exclusions. Inline owners test only their start; private tail nodes are never
visited again as filter owners. Mapping uses indexed original Content; no copied
Source or full-envelope rescan per fact is used. The shared HTML lexer indexes
quotes/terminators once and optionally caches opening-tag attribute suffix
successes and failures for raw-prefix queries. Each uncached suffix state advances
or terminates; later overlapping queries reuse it. Index searches, cache records,
failed candidates, source slices, property/Value/segment copies, validation and
freezing are charged before allocation or bulk comparison. Both generic record
inventories include HTMLFilteredTag. New HTML validation costs convert lengths
to int64 before scaling or addition, use checked saturation and charge the same
shared operation without narrowing back to a host int. Saturation exceeds every
validated work allowance and fails closed. Pure arithmetic controls exercise
32-bit admission/NUL expansion and overflow boundaries without huge allocations
or claiming actual 32-bit runtime verification. Content association is indexed and visits
only the supplied contributions; fixed prefixes/boundaries need no general
constructor grammar parse. Optional overlapping full Tag metadata is metered per
retained owner/fact extent rather than hidden behind a linear-output claim.

Filtering adds no syntax depth. Tree replacement preserves private HTML/comment/
tail facts and publishes only after the shared final interruption checkpoint.
Incomplete metadata never grants authoritative comments, references, literals or
protected nodes. Exact budget boundaries, sticky operations, doubled finite
success/failure families, mapping/freezing cancellation/deadline sweeps and
same-operation/independent concurrency are tested. These finite checks do not
promise hard RSS bounds, OS preemption or an arbitrary-input complexity proof.

## Integrated GFM verification

`go test ./internal/parser -run '^TestGFMVerified' -count=1 -v` exercises the
private all-five profile with exact work/construction boundaries, cumulative
protection and core reparses, sticky exhaustion, mixed depth, cancellation and
deadline checkpoints, and finite adversarial families. Incomplete parses deny
Comments, LiteralSpans and ProtectedNodes authority. Separate real engine,
inspection and fix integration tests verify consumers obey that refusal. The [GFM verification record](gfm-verification.md)
describes the semantic fuzz bounds and remaining limits.

## Terminal task preservation consumers

GFM MD047 insertion eligibility charges source copying and construction before
parsing one prospective LF ending with the unchanged whole-profile parser on
its existing operation budget. LF, CRLF and CR all supply task whitespace; the
candidate checker compares the actual chosen ending's actual parse. This extra
parse is cumulative, including source admission, work, nodes, depth and time;
exhaustion or cancellation cannot publish an eligible or corrected result.
No suffix recognizer or alternate grammar decides task adoption.

The completed same-operation TaskSources inventory charges traversal, stack
storage and detached task/Content records before construction. Comparison charges
original source reads and visits; it compares task classification, original token
spans/spelling and visible paragraph contributions. Candidate validation obtains
both real round parses independently of producer eligibility. This is a narrow
suffix preservation proof alongside existing exact body, protection, binding and
directive checks, rather than a general semantic equivalence claim.


MD018 candidate retention charges each actual paragraph line visit and at most
seven hash-byte inspections, reserving provisional and final source records
before append/growth. Reference extraction and final block promotion decide
ownership before publication. Its completed opening-hash projection charges
candidate flags, one tree/stack traversal, indexed overlap comparisons, raw-region
filtering and output records, including copied growth prefixes. It uses the
original operation's context, work, construction and elapsed allowance, without
reparsing, cloning the tree or scanning the whole source per candidate. Any
interruption withholds the complete inventory. Literal expected fixtures and
original-allowance/authority controls verify these claims separately from full
syntax conformance.

MD019 reuses the original-operation heading inventory. Marker visits, first raw
content inspection and separator fact construction are charged before publication.
Its callback reserves each bounded separator byte scan and source/span access
before counting literal ASCII spaces. No fresh parse, source or budget is created.
Interrupted inventory projection withholds the list; a later callback stop retains
findings already constructed with the canonical interruption.

MD020 projects terminal runs only from actual open ATX nodes. Original first/last
Content contributions retain bounds across NUL normalization. Each backwards hash
and backslash inspection checks the original operation and reserves work before
reading; stack, candidate, flag, overlap and output construction are charged.
The shared MD018/MD020 protection filter traverses actual syntax once per inventory
and uses indexed whole-run overlaps, including active raw-text regions. No fresh
source, parse, budget or public-tree clone is introduced. Failed projection
withholds the inventory; late callback interruption retains reliable findings.

MD021 adds two fixed source spans to each detached heading fact. Only genuine
closed ATX markers grant these gaps. The first/last raw Content source bounds
survive NUL normalization; Visible does not decide content presence or bounds.
Raw-empty closed headings expose one shared gap. OpeningSeparator retains its
absent-on-empty contract. Marker traversal and the fixed gap construction reserve
work on the original completed parse before publication; no additional inventory,
source, parse, allowance or tree clone is created. The callback reserves bounded
source access/scans and finding construction, checking long scans every 256 bytes.
Failed projection withholds the inventory; later callback interruption retains
previous reliable findings with Err and explicit incomplete coverage.

MD022 uses the original completed document and same-source physical LineIndex.
Its separate heading-separation projection reserves one record per physical line,
visits retained structural markers and headings once, and charges binary line
lookups, byte scans, amortized output copying and detached heading/possible side records before
retaining them. Linear forward/backward passes compute body-blank runs after each
line's own actual prefixes. Periodic byte-scan and per-record checkpoints retain
caller/original-operation controls and sticky cumulative node/work/time limits.
No fresh source, index, parse, public tree clone or allowance is constructed.
Cost is bounded by actual bytes/lines/tree facts and logarithmic line lookups,
never numeric before/after values or a whole-document scan per heading. Projection
failure withholds all facts; a later callback stop retains reliable side findings
under the ordinary partial-result contract. This is finite accounting, not a
process memory or throughput guarantee. See [MD022](../rules/headings/md022.md).

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
guarantee. No correction provider or write authority is added. See [MD023 policy](../rules/headings/md023.md).

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
process memory or throughput guarantee. See [MD024 policy](../rules/headings/md024.md).

MD025 counts matching-level headings with one saturated baseline flag and no
text comparison or container partition. Opt-in metadata is decoded once through
the original title provider; unknown semantics withhold only the first matching
body comparison and retain reliable later duplicates with incomplete coverage.
The callback charges each heading, each retained finding and potential slice
copy before append through the original compound caller/operation and sticky
work/node/elapsed controls. It uses original raw Label spans and adds no source,
parse, index, allowance, edit or write authority. See [MD025 policy](../rules/headings/md025.md).

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
not a process memory or throughput guarantee. See [MD026 policy](../rules/headings/md026.md).

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
See [MD036 policy](../rules/headings/md036.md).


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
See [MD041 policy](../rules/headings/md041.md).

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
See [MD043 policy](../rules/headings/md043.md).

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
See [MD063 policy](../rules/headings/md063.md).

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
follow the [MD082 policy](../rules/headings/md082.md).

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

MD005 consumes the same original list facts with per-analysis baseline storage.
It reserves records and work before constructing baseline and finding storage,
compares constant-size complete group identities and structural marker indentation,
and withholds findings on caller/origin or sticky quota/final publication stops.
No replacement allowance, parse or source scan is introduced.

MD007 reuses the complete original list inventory and admitted scalar options.
For each actual bullet item, local indentation is ContentIndent minus MarkerWidth
minus Padding. With a bullet parent, its actual marker width plus padding plus
that local indentation must equal configured indent. With an ordered parent,
local indentation must be zero: its content column is the target without extra
indent. Parentless bullets require zero, or exactly indent with start_indented.
Quotes strip prefixes without resetting actual ancestry; hidden or misindented
parents remain anchors. A bounded invocation-local map keys constant source-token
and item offsets, retaining ordered anchors before children; storage, work and
finding growth reserve the original allowance even on clean inputs. Final caller
and origin checks withhold interrupted findings. Findings own one bullet byte
and remain manual despite PlannedFix metadata; MD047 alone produces edits.

MD029 reuses the original detached list facts and borrows its admitted `style`
under the same original operation controls. Each actual ordered group keeps an
immutable first direct numeric start and a direct ordinal, including wrong and
suppressed items. Independent nested groups and quotes retain enclosing state;
ordered descendants below bullets are targets. Numeric leading zeros are equal,
zero starts are valid, and comparing actual minus first to ordinal avoids growing
numeric sums. `one` requires numeric 1 even on the first item. State uses constant
source-token and group offsets, with storage, work, capacity, copies and finding
records reserved before construction, including clean inputs. Caller and origin
checks atomically withhold interrupted findings. Diagnostics own the full digits
and delimiter only. Default-enabled PlannedFix remains metadata; findings are
manual and MD047 alone supplies executable edits.

MD030 reuses ListItems once, reserves detached separator capacity and logical
records, and precharges bounded binary searches of the original line index.
It derives first-line presence from original prefix/physical-line bounds, without
scanning Unicode prefixes or copying source. Comparisons and finding capacity,
copies and records use the same original allowance. Caller and origin controls,
sticky work/node/time limits and final publication checks withhold all findings.
Configured spaces is a borrowed positive int compared directly, including max int.

MD032 reserves original physical-line storage, byte scans, traversal frames,
prefix rollback capacity and copies, definition association visits, binary
lookups, detached boundary capacity and logical records before construction.
Clean and no-list paths are metered. Caller and origin controls and final
publication checks withhold incomplete inventories. No new allowance is created.

MD069 reserves association-map and run capacity before construction, bounded
line-index comparisons before lookup, and original gap bytes before scanning.
Different physical lines are rejected before gap work. Run and output storage
are distinct from logical records; final publication observes original controls.

MD076 uses one original source-line summary and actual tree traversal, with
precharged frames, prefix rollback and definition association. Each actual List
retains its own exact baseline in its frame; no source-bearing span hashing or
per-blank inventory is used. The admitted style chooses the first extra line in
that traversal. All output is withheld on original caller/budget interruption.


MD077 replays only confirmed final container markers using borrowed original
source/index cursors, preserving partial tabs and physical phase. It reserves
per-line state, associations/lookups, cumulative frames, inline protection visits,
growth/copies and detached findings before construction. No parse retention,
replacement index, whole-source reparse or additional allowance is introduced.

MD009 borrows final private production ownership, original source/index/options
and the same operation witness. It precharges physical suffix scans, failed byte
probes, line searches, markers, HTML tails, per-line state, frame/output capacity,
copied growth prefixes and logical records. Empty, clean and protected inputs
still consume work. Producer, facade and callback withhold all output on failure.

MD010 borrows completed original production/profile/options/source/index and
operation authority. Original-byte scanning, TAB inventories, binary associations,
actual content/markers/tails, confirmed scalar prefix replay, line summaries,
frames, capacities, copied growth prefixes and logical output records are charged
before work. Clean, empty and protected inputs still consume work. All output is
withheld on caller/origin cancellation or sticky work/node/time failure, including
producer, facade and callback final checks. Storage depends on source facts,
never spaces_per_tab. No new retention, grammar, source/index/options clone,
public schema, allowance or correction authority is added.

MD012 borrows the completed original production/profile/options/source/index and
operation witness, rejecting private conformance scopes or constructed syntax.
Physical ownership includes empty protected payload endings and exclusive interval
ends. All state, lookups, visits, capacities, copies and output records reserve
before work. Final failure withholds detached spans; no parse-time retention,
public model fields, source copies or fresh allowance are added.

MD013 borrows completed original source/index/options/profile/reference facts. Its detached scalar spans use physical width and separately charged contribution/raw/category/URL eligibility. Confirmed raw-prefix replay preserves original TAB phase; semantic destinations are borrowed without decoding. Every probe, interval/frame/output capacity and copied prefix is reserved before work, with caller/origin and final atomic withholding.


MD027 borrows completed private original syntax, source/index/options/profile and operation. It charges physical state, source and failed probes, bounded lookups, actual prefix replay with residual TAB phase, Paragraph/inline/raw associations, HTML tails, traversal frames, capacity, copied prefixes and logical output records before work. It creates no parse-time facts, source/index/options clones or new allowance; final failures withhold the dependent inventory.


MD028 borrows completed original syntax/profile/options/source/index and operation authority. BlockquoteGaps associates actual retained atomic prefix ends and source-ordered raw/literal protection before comparing final direct siblings. Original nonblank gap bytes prevent stepping over removed definitions. All state, probes, lookups, comparisons, frames, capacities, copies and records are precharged. No parse-time retention, reparse, clone or new allowance is added; failure withholds all output.


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


MD085 borrows the completed original production source, index, options, profile
and operation through ParagraphIndentation. Actual root Paragraph content fixes
one baseline; all container descendants are excluded. Ordinary semantic Text
witnesses prose separately from protected multiline physical prefixes. Actual raw
state crosses Paragraph boundaries and HTML tails, with isolated image labels.
Actual unprotected HardBreaks exempt only their physical successors. Original
ASCII SPACE/TAB surplus spans remain detached and manual. Physical-line state,
failed byte probes, semantic/provenance associations, bounded lookups, raw state,
traversal frames, growth capacity, copied prefixes and logical spans/findings are
reserved before work. Empty/clean/protected input retains caller and originating
controls, sticky finite work/nodes/elapsed and atomic producer/facade/callback
publication. No reparse, source/index/tree clone, renderer, decoder, new allowance
or correction authority is introduced. MD047 remains the sole correction producer.


MD089 shares the original completed production source/index/options/profile and
operation through a narrow CJKSpacing projection. Actual Paragraph, Heading and
TableCell Exact scalars supply physical pairs under Unicode 17 Han intersect
Ideographic, Latin intersect Letter and ASCII digits. Actual recognized URL
owners, raw tags, HTML tails and isolated image alternatives retain protection.
Zero-width insertion spans and findings are detached and manual. Source/semantic
association comparisons, failed UTF-8/scalar probes, bounded Unicode table
lookups, traversal state/frames, capacity, copied prefixes and logical records
reserve work/nodes before use. Empty, clean and protected paths retain caller
and origin cancellation, sticky work/nodes/elapsed and final atomic withholding
at producer, facade and callback. No reparse, tree/source/index clone, retained
AST facts, decoder, new allowance or correction authority is introduced.

MD035 borrows completed original thematic-marker spans, private raw tags and HTML
tails through ThematicStyle. A single global borrowed original pattern survives
container changes and disabled intervals; image alternatives isolate raw state.
Traversal frames, marker/source associations, comparisons, state, capacity, copied
prefixes and detached span/finding records reserve the original finite allowance
before work. Empty, clean and protected inputs retain caller/origin controls and
final atomic withholding at producer, facade and callback. No reparse, retained
AST, source/index/options clone, decoder, new allowance or correction authority
is added. MD035 is default enabled and manual; MD047 alone supplies corrections.

MD037 borrows completed original source/index/options/profile and operation
through DelimiterStyle. Actual recognized Emphasis/Strong facts remain distinct
from malformed whitespace ranges. Semantic contributions, original associations,
UTF-8/scalar/word/whitespace and failed marker probes, raw state, traversal frames,
capacity, copied prefixes and retained records are reserved before use. Empty,
clean and protected input retain caller/origin controls, sticky finite work/node/
elapsed limits and atomic final producer/facade/callback withholding. No reparse,
decoder, tree/source clone or new allowance is introduced. Source-derived bounded
reservation tests establish specific fixtures, without a performance claim.

MD038 CodeSpanSpaces borrows the completed original production parse and allowance.
It scans actual retained SPACE edges and actual normalized Literal separately,
without reconstructing payload or using semantic offsets for original geometry.
Source/marker/segment associations, failed byte and end probes, traversal and raw
state, output/growth capacity, copied prefixes and logical records reserve original
work/nodes before use. Empty/clean/protected results retain caller/origin and sticky
finite controls with atomic final withholding. Source-authored exact residual
reservation tests cover small inputs, long failed probes and multi-span growth;
these are finite accounting examples, not process-memory or throughput promises.


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

## MD086 original unmatched-comment inventory

`UnclosedHTMLComments` borrows the completed private CommonMark/full-GFM parse,
original source/index/options and original operation. It visits actual HTML
owners in source order, including mapped form-6/7 interiors and owned tails once.
Whole Images are opaque; actual Link labels remain eligible. Existing lexer
short forms, quote/PI/declaration/CDATA precedence and four-name raw lifetimes
are preserved. Matching never crosses the actual owner/container boundary.

Owner scratch capacity, mapping/source association, traversal, terminator indexes,
failed suffixes, raw/quote comparisons, copied prefixes, output capacities and
logical facts/findings reserve finite original work/records before use. Scratch
length arithmetic remains checked and wide before multiplication; saturation
fails closed through the same original allowance. Actual lexer work is metered
as well. Producer, empty-options facade, comparison and distinct outer callback
final publication observe caller AND originating controls, including empty,
clean, protected, unavailable and post-output paths. Any failure atomically
withholds output. Failed owned named-token context gives explicit coverage;
an established unmatched comment is a violation instead. No reparse, retained
parse-time facts, decoder, source/tree/index clone or replacement allowance is
introduced. Finite application accounting is not a CPU/RSS guarantee.

MD086 is default ON, RequiresSyntax, UsesFrontmatter=false and permanently NoFix.
Findings use `HTML comment is missing a closing delimiter.` over exactly the
original four-byte opener. Shared START suppression precedes inspection slicing;
MD047 remains the sole correction producer.
