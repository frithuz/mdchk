# Read-only resource contracts

Issue #249 establishes engine-facing types and fixtures. Issues #170/#250 add
the strict supplied-input boundary and shared destination normalization. Issue
#251 adds root selection and an internal raw native read adapter. Issue #252
adds immutable partial/complete supplied-inventory lookup. First-observation
caching, anchors, production consumers and coordinated fixes remain downstream. No CLI/MCP resource capability or engine rule
callback is enabled by these internal APIs.

A caller supplies an opaque root identity, immutable parser/index/anchor policy,
a shared operation context and budget, and explicit selected target snapshots.
Root identity has no host path or generic filesystem handle. Target IDs are the
source reporting identities; resource keys are separate literal root-relative
names. Config discovery, exclusions and report bases are deliberately outside this
API and cannot widen root authority. A resource observation cannot register a
target, configuration, write or outgoing-link traversal.

Original, candidate and saved views have distinct immutable identities. Only
selected targets may be replaced. A saved view describes caller-confirmed bytes;
a stale or failed save must be supplied as unavailable. An omitted replacement
means that the caller confirms the original is still current. This models the
replacement contract; it does not execute overlays or verify disk persistence.
For work that may finish after a stop, `ReserveSavedView` preallocates one bounded
selected-target reconciliation attempt. Its `Bind` retains already-confirmed or
unavailable saved facts after cancellation/quota exhaustion. The reserve is shared
across copies and consumed even by an invalid attempt; it grants no write or new
analysis permission. Creating another reservation still requires execution budget.
Requests must name the selected source snapshot in that exact view. Their
semantic destination and source-owned original byte span remain independent from
the eventual logical key. Public ranges and source digests are captured before
execution so interrupted work can still report source-attributed coverage.

`Reader` is the consumer-owned read-only adapter seam. `Operation.Record` validates
and copies its evidence. File/directory/missing/unknown existence is independent
from content and parsed-source availability. An empty acquired source is distinct
from omitted content. Missing is authoritative; partial inventory omissions,
blocked context, read/decode failures and interruptions do not imply absence.
Available parsed-source evidence must come from a complete reliable parse using
the operation's budget and `Operation.Settings().ParseOptions()`. Producers MUST
use these options. `Record` verifies source, completeness and budget ownership,
but cannot authenticate parser-policy provenance; a same-budget wrong-options
parse is not mechanically rejected. It is not an anchor index or an anchor
algorithm. `ValidateContent` checks strict UTF-8 and bounds while execution is
permitted. Retain its opaque operation-owned `ContentEvidence` in
`ObservationSpec.Validated` to preserve acquired bytes when later anchor work
stops. Matching evidence avoids rescanning/recharging; zero, foreign or mismatched
evidence cannot validate content. Without evidence, `Record` requires active
validation budget. Evidence does not prove inventory truth or parse policy.
Directory and index-file attempts use separate keys and required types; a known
directory alone says nothing about whether its configured index exists.

`Treatment` records the MD057/MD051 ownership contract without emitting rule
findings: MD057 owns missing/type mismatches; MD051 is inapplicable for known
missing targets even when MD057 is disabled. Unknown existence or required
unavailable anchors remain incomplete. `AppendEffects` appends only shared result
coverage, operational errors and interruptions, deduplicating resource causes
while retaining each affected source link. Encountered operational causes are
projected independently of the requested capability: a satisfied existence query
retains a later I/O error/stop without falsely acquiring a coverage deficit.
Monotonic per-prefix cause ordinals preserve existing-ID collisions without
rescanning prior assigned ordinals for every resource. Projection never adds
checked files, file records, returned documents, diagnostics or changes. Direct
users call it once with the complete view's request batch, then use the existing
engine finalizer. Native-reader users instead submit complete batches through
`FilesystemReport` below, which owns both these effects and native failure
projection. Existing exit precedence is unchanged. Result mutation must be
serialized.

Construction and request projection charge the supplied shared budget; no query
resets it. Immutable snapshots/views/observations support concurrent reads. Result
projection after an interruption does not create a new execution budget. The native
adapter below implements bounded acquisition and shared operation accounting;
first-observation retention, cache bounds, singleflight and queried-identity service
composition remain #253. The types alone do not authenticate adapter truthfulness,
inventory completeness or native confinement. Each concrete producer owns those
guarantees and must use the shared normalization pipeline below rather than
reinterpret destinations.

## Supplied input boundary

`DecodeSupplied` validates the raw optional resource argument and all submitted
`SuppliedFile` snapshots before exclusions. Nil raw input means an omitted partial
inventory; a present object requires `entries`, and `complete` defaults to false.
`SuppliedInput` retains these facts and immutable file/entry snapshots. File entry
content may be omitted; a supplied empty string retains a real empty Source.
Entries cannot register targets or perform filesystem/network operations.

Reuse `SuppliedSchema()` when embedding the closed object shape in future tool
schemas. Schema validation alone cannot recover malformed raw Unicode or duplicate
object names, enforce operator limits, or compare different records. The shared
raw decoder uses `strictjson.DecodeMetered` before ordinary decoding can discard
evidence. It also rejects noncanonical literal keys, duplicate paths/target
overlap, and declarations below file/missing/unavailable ancestors. It does not
build implied directories or interpret completeness as lookup outcomes (#252).
No case folding, URL decoding, native alias interpretation or filesystem fallback
occurs. MCP #189 must preserve raw arguments and bound the complete request before
calling this boundary; this package does not implement a transport.

`InputLimits` are explicit operator-owned finite preflight bounds, aligned with
the accepted shared profile. They bound raw argument bytes, all submitted targets,
explicit combined identities, path bytes/depth and aggregate supplied content;
per-text bounds use the existing operation budget. Submitted excluded snapshots
still count. Inferred and queried identities/acquired content require additional
accounting by #252/#253. A typed `InputError` identifies malformed/known oversized
input before analysis; runtime shared quota/deadline/cancellation errors retain
their original identity. Callers own protocol/status mapping.

Raw decoding reserves three byte inspections, meters record/name/path visits,
and checks the existing cumulative budget and context between tokens and each
4096 bytes of escape inspection. `encoding/json` token operations are bounded by
raw input size and are not asynchronously interruptible. Its independent fixed
64-container/250000-record/250000000-work safety ceilings still apply; accepting
an operator input bound does not promise those ceilings can be raised. Shape,
content copy/UTF-8 validation and bounded declared-ancestor walks are also charged.
No validation call resets the operation budget or advertises full resource support.

Schema validation also requires `node tools/resource_schema.mjs` (Node.js 18 or
newer, built-in modules only). This separate required local/CI gate runs both
published path patterns in an actual ECMAScript engine, with and without Unicode
mode. It shares `testdata/supplied-paths.json` with the ordinary Go suite and
covers U+2028/U+2029 plus forbidden controls, separators and dot segments after
those scalars. Python JSON Schema validation uses a different regex dialect and
cannot replace this gate. CI runs the command unconditionally; a missing Node
runtime fails the gate rather than silently skipping it. Node is verification
tooling only, not a runtime dependency of mdchk.

## Destination normalization

Issue #250 adds the single `NewNormalizer(view, PathLimits, NamePolicy)` /
`Normalize(Link)` pipeline for future project and supplied-map adapters. It starts
with the parser-semantic destination and retains the exact original Link/span.
It classifies raw schemes/native forms, splits raw path/query/fragment, discards
the query, and path-unescapes path and fragment independently once. Literal plus,
case, Unicode and percent data are preserved. Remote/protocol-relative references
are ignored; unsupported local/native forms, encoded separators and above-root
traversal are blocked. Invalid path encoding or controls are invalid-reference
facts. Fragment errors remain separate and do not erase a valid file path.
Nonlocal raw paths leave any present fragment `unchecked`; consumers need not
interpret irrelevant remote fragments.

`NormalizedLink` is an immutable decision, not evidence of resource existence.
Its `Location` distinguishes a root directory from an ordinary canonical Key;
the zero location means neither. Never submit `.` or an empty file Key to express
the root. `DirectoryHint` retains a directory type requirement even if the spelling
matches the source. `SameSource` exposes the exact selected original/candidate/
saved snapshot only when proven, without content reacquisition. Empty/query/
fragment-only paths work for selected sources outside the namespace, but have no
resource Location. Nonempty paths from those sources are blocked; this API accepts
no unverified self-alias proof and never derives authority from report names.

A project `NamePolicy` is required and checks representability of the complete
canonical key before a new lookup; it is a pure, bounded, concurrency-safe trusted
adapter predicate. It does not establish actual spelling, type or confinement.
Known selected-file identity needs no fresh native name lookup. This includes a
source-equal directory hint: keep its Local locator and DirectoryType requirement,
but do not claim SameSource directory success. A selected configured index likewise
reuses the exact available view Source before a new-lookup name predicate.
Supplied roots never call this predicate and do not inherit server-OS filename
aliases. Issue #251 supplies the native path policy and raw acquisition boundary
below. Literal supplied keys from #170 are never passed back through URL decoding.

After establishing directory existence, call `DirectoryIndex` once. Its
`IndexLookup` names only the configured basename with `RegularFile` required,
retains active-adapter representability, and exposes no recursive index planner
or fallback list. A directory hint alone is not existence evidence; check its
`DirectoryType` requirement before using the plan. A known regular file where a
directory is required, or a directory at the index key, remains a type mismatch.
Retain the original NormalizedLink for source attribution and fragment state.
Root existence and actual directory/index facts belong to the authorized adapter;
unknown directories never imply absent indexes.

`PathLimits` uses the shared operator path-byte/depth defaults, while the existing
operation document-byte limit bounds a semantic destination. Normalization reserves
16 work units per destination/source-key byte before scans, decoding, name-policy
checks and joins, charges constructed decisions/segments to the same node meter,
and checks cancellation/time between bounded segments and around whole decisions.
The standard unescape/UTF-8 routines are size-bounded, not asynchronously preempted.
Runtime stops return their original context/quota errors, never absence or malformed
URL facts. Completed decisions remain readable after a stop; new index planning
still requires execution budget. Exported handle replacement cannot mutate a
previously returned decision.

The exact 80-row outcome table, source/view/result fixtures and
`FuzzNormalizeDestination` exercise this boundary. The required schema ECMAScript
gate from #170 remains unchanged. Supplied inventory lookup, first-observation
cache, anchor interpretation, production rule consumers and runtime resource
capability remain downstream of normalization and raw native acquisition.

Run a bounded normalization campaign with
`GOMAXPROCS=2 go test ./internal/resource -run '^$' -fuzz '^FuzzNormalizeDestination$' -fuzztime=20s -parallel=2`.
Fuzz oracles preserve logical confinement and once-only percent data; they do not
re-feed literal output keys as URLs and incorrectly demand decode idempotence.

## Native root selection and raw reads

Issue #251 implements `SelectFilesystemRoot` with explicit CLI, inspect and
project modes. CLI selects the nearest repository/worktree root from cwd, then
uses a containing selected-config directory or cwd outside repositories. Inspect
applies that selection from the already acquired canonical target's parent.
Project mode retains its validated startup root exactly, including a narrow root
inside a repository.
A configuration discovery cap is never resource permission; below/beside/external
configuration facts do not enlarge the root and are not newly probed for fallback.
Standalone callers have no native-root mode or filesystem fallback.

`OpenFilesystemRoot` pins a canonical native anchor and exposes an opaque Root,
not descriptors, a generic filesystem or a writer. `BindTarget` obtains an opaque
membership receipt for an already acquired canonical target path and its original
Source before constructing the Operation. Report IDs and plausible Key strings
alone cannot prove membership. `FilesystemKey` is only lexical mapping; actual
entry spelling, metadata aliases and native identity require the concrete adapter.
Outside/unproved/metadata targets retain their independent same-document snapshots
without a cross-file ResourceKey. The original source's acquisition/provenance is
still the caller's responsibility; binding never rereads its contents.

Construct `FilesystemReader` for the exact Operation with those receipts. It
returns the bound view's original/candidate/saved selected snapshots directly,
including explicit unavailable saved state, rather than reopening candidate or
saved targets as fresh disk data. Other in-root names go through raw native
acquisition. Excluded/unselected files may supply read-only context; they never
become lint targets, corrected returns, writes, nested configuration or recursive
outgoing-link checks. A directory and its one configured index-file attempt retain
the separate type requirements from the shared normalizer.

The native backend uses checked single-component operations relative to pinned
parent handles: true no-follow `openat` on Linux/macOS and strict directory-relative
NT/reparse handling on Windows. Passing `O_NOFOLLOW` to `os.Root.OpenFile` is not
this guarantee: that resolver deliberately follows contained links. Resource reads
reject contained, escaping and dangling links, Git metadata identities and
unsupported types, with no weaker fallback. Actual entry names are compared exactly
before lookup; only a complete stable parent scan can prove an exact spelling
absent. A wrong-case native alias is never opened as the requested logical name.
Windows device/stream/trailing-dot aliases are blocked separately from absence.

Windows anchors are limited to admitted local drive paths. UNC, device and raw
extended namespaces are refused before canonicalization or metadata probing;
reparse anchors and mapped network drives are also refused. Trusted Unix host
aliases may establish an operation anchor before strict pinning, but no resource
key passes through that canonicalization. Unix filesystems may themselves be
network-backed; this code does not claim physically network-free kernel I/O.
There is no URL fetching or transport/network-client fallback.

`FilesystemLimits` shares operator-owned path/depth, directory-entry, byte and
retry allowances with the existing operation context/budget. All reads, failed
attempts and retries count; constructing another Reader cannot reset them. Root,
Operation and individual caller cancellation each stop affected new work, including
waiting for the serialized acquisition gate. Bounded enumeration, reads and
stability checks preserve actual I/O failures separately from established absence
and runtime stops. The initial attempt permits at most two instability retries.
Checkpoints do not asynchronously interrupt an unresponsive native filesystem.

Original selected-source bytes are admitted once into native session accounting;
newly read resource bytes are cumulative. Individual original/candidate/saved texts
still obey `document-bytes`. Admission of an entire candidate/saved overlay against
its separate aggregate limit belongs to the overlay producer/#258, not this raw
reader. `Session` accepts a distinct operation budget and creates a new identity
over the retained startup anchor without sharing contents, misses or settings. It
is not a cache or an atomic whole-tree snapshot; #253 must retain first
observations and coordinate concurrent duplicate lookups above this raw
acquisition boundary.

`Read` can return a valid partial `ObservationSpec` together with an error. Keep
both and bind the Spec with `Operation.Record`. Native result projection uses an
explicit report owner:

```go
report, err := NewFilesystemReport(operation, initial)
// Handle err before continuing.
err = report.Append(view, observations, []FilesystemFailure{
    {Reader: reader, Request: request, Err: readErr},
})
// Handle err before continuing; append further complete view batches as needed.
out, err := report.Finish()
// Handle err, then pass out to the existing shared finalizer.
```

`NewFilesystemReport` takes an initial `result.Result` value, not a caller Result
pointer. The caller transfers ownership of that value, including its slices, and
must not mutate or finalize it while the report owns it. `Append` validates the
entire complete-view batch, including its observations and failures, before
mutating the report. Append
Original, Candidate and Saved batches through the same owner when they belong to
one report; failures from different Reader handles share that owner's incremental
cause indexes and collision-aware monotonic IDs. Projection has no mutable
Reader-local cache and does not scan the growing Result for every error.

`Finish` consumes the owner and transfers its `result.Result` value back for shared
finalization. Copies share the consuming guard; zero or consumed owners reject
`Append` and `Finish` with `ErrContract`. Construct a fresh report explicitly for
each independent result;
reusing the same caller `out` variable for successive finished reports is supported.
No wire fields are added. This ownership boundary preserves late close errors and
simultaneous I/O/quota causes, exact per-view source attribution and already
acquired File/content facts without inventing a coverage deficit for a satisfied
capability. Report use remains serialized by its caller. The raw reader does not
parse anchors; later producers must still use the operation ParseOptions and
retain validated ContentEvidence across later stops.

Native no-follow, root, stability and lifecycle tests passed on actual Linux,
macOS and Windows hosts in [run 40145609610](https://github.com/frithuz/mdchk/actions/runs/40145609610),
including real Windows symbolic links/junctions and live/closed native handle
controls, with no native test skips. Cross-vet, cross-compilation or a mocked
backend alone is not runtime proof. No new CLI/MCP capability or broader native
writer support is advertised by the read adapter.


## Supplied inventory acquisition

`NewSuppliedReader(ctx, operation, input, limits)` builds an immutable logical
index from `DecodeSupplied`'s owned `SuppliedInput`. Use the same operator input
limits and operation budget. All submitted files seed it, including excluded
ones. The selected subset remains explicit in `Operation`; every selected key
and original Source must match an actual submitted seed exactly. Equal bytes in
a different Source or a reporting ID alone cannot grant this authority. The
selection owner must reject all-targets-excluded as usage; creating a resource
reader does not make an empty selected set valid for lint/fix.

Only positive files/directories infer parent directories. Explicit declarations
and distinct inferred parents share the `Identities` cap, checked before retaining
new names; the virtual root is a separate constant fact. The builder publishes
nothing after input excess, cancellation or quota failure. Known input excess
uses `InputError`; actual runtime stops retain their context/limit identity.
Constructing another reader charges the existing budget rather than resetting it.
Supplied content bytes are admitted as input once; repeated lookup charges actual
lookup/UTF-8 validation work, not another submitted payload allowance.

Lookup resolves an explicit/seeded/inferred entry, then an absent or blocked
ancestor, then completeness. A known file ancestor proves descendants absent.
Partial omission is Unknown; complete omission is Missing. An unavailable subtree
stays unavailable even when complete. Negative declarations infer no parent
existence, and a directory alone does not prove its children absent in a partial
map. `RootDirectory(ctx, location)` accepts only a root-directory Location from
this exact namespace. It works with the existing one-level `DirectoryIndex` plan;
there is no empty/dot Key, recursive index search or implicit entry for the root.

Files without content satisfy existence only; an explicitly empty string is real
available zero-byte content. Completeness asserts names/types, never content.
Existence/type checks do not request content or parse work. Nonselected implicit
non-Markdown resources cannot gain Markdown anchor interpretation. A selected
snapshot remains an explicit Markdown document regardless of extension. Original,
Candidate and Saved selected resources use that exact view's snapshot, including
an unavailable Saved resource queried from another source. Excluded seeds and
resource-only entries keep original bytes and gain no target/write authority.

Supplied names are exact literal canonical keys. They do not inherit native
case/Unicode, Windows device/stream or trailing-name aliases. `.git` components
are blocked for resource lookup; distinct `.GIT` is literal data. The decoder does
not reject these names merely because lookup policy blocks a resource. An
independently selected source retains its own same-document snapshot authority.
No call reads server files, fetches URLs, decodes keys again, parses Markdown,
executes directives or traverses outgoing links. There is no cross-call cache or
first-observation/singleflight service; those belong to #253. Complete navigation
inventory consumers, anchors, MCP transport and coordinated fix overlays remain
later integrations.

`Read(ctx, request)` returns an ObservationSpec and an independent controlled
runtime error. Bind the spec with `Operation.Record` and retain any returned error
using `SuppliedStop` in `reader.AppendEffects(&out, view, observations, stops)`.
Call once for the complete view batch, then use the existing finalizer. Do not
also project the same observations through `View.AppendEffects`. A late stop can
accompany known Directory/Missing or already acquired content: it must not be
stuffed into an invalid spec, erased, or turned into I/O. The effects method keeps
the stop even when the request's capability is already satisfied. Context deficits
appear only for actual unresolved requests, never merely because the inventory
is partial. Missing/type treatment stays MD057-owned and MD051-inapplicable.

Projection validates all observations and stop carriers before result mutation.
The caller retains result/slice ownership and serializes mutation. Different
readers/views and fresh results reusing the same caller variable can interleave;
there is no mutable reader projection state or saved Result pointer. Each complete
batch scans existing cause IDs once and processes that batch incrementally; it
is not a persistent indexed report service. Root-only planning errors have no
source Request and remain the caller's operation-level interruption handoff.
