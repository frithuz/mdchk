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
