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
The private GFM-in-progress stage counts Table, TableRow and TableCell as one
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

The private table stage uses the same admission, validation, phase recovery and
operation as public core parsing. It charges provisional row/cell specifications,
separator/escape spans, alignment and property records, raw mapping segments and
immutable copies, including failed delimiter/width candidates. Complete source
cells are retained even beyond semantic table width. Padding is one spanless
count per row, avoiding width-times-row synthetic allocations. Candidate rows
freeze only after header/delimiter equality and combined depth admission. Public
entry points keep their existing core-only behavior; this internal stage does
not establish complete GFM capability or full-profile conformance.
