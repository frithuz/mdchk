# Read-only resource contracts

Issue #249 establishes engine-facing types and fixtures. Issues #170/#250 add
the strict supplied-input boundary and shared destination normalization. Issue
#251 adds root selection and an internal raw native read adapter. Issue #252
adds immutable partial/complete supplied-inventory lookup. Issue #253 adds the
operation-owned first-observation service and parsed-source cache seam. Issue
#254 adds the immutable CommonMark/GFM heading and verified HTML ID index.
Production consumers and coordinated fixes remain downstream. No CLI/MCP resource capability or engine rule
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
Available anchors require the service's opaque computed `AnchorIndex` and complete
parsed evidence for the exact operation, source owner, view and settings. A
caller-supplied complete parse alone cannot grant anchor availability. The index
retains the full digest, parse options (including frontmatter), parser revision
and shared anchor style in its `ParseCacheKey`. `ValidateContent` checks strict UTF-8 and bounds while execution is
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
the service below supplies first-observation retention, cache bounds, singleflight
and queried-identity composition. The types alone do not authenticate adapter truthfulness,
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
anchor interpretation, production rule consumers and runtime resource capability
remain downstream of normalization, raw acquisition and the operation service.

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
is not a cache or an atomic whole-tree snapshot; Service retains first
observations and coordinates concurrent duplicate lookups above this raw
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
executes directives or traverses outgoing links. The raw supplied reader keeps no
cache. The operation service below supplies first-observation/singleflight behavior;
there is no cross-call content or settings cache. Complete navigation
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


## Operation-owned observations and parsing

`NewFilesystemService(reader, limits)` or `NewSuppliedService(reader, limits)`
registers one canonical service on the exact Operation. Repeating the same native
root/operation or supplied reader with identical limits returns that service.
A different adapter or policy is rejected; another wrapper cannot reset counters.
A new operation must acquire new target/configuration snapshots and native session
identity. The service never shares mutable content, misses or settings globally.

The owner **must call Service.Close before closing the native root**. Close cancels
and joins the single owned worker and is idempotent. Do not abandon an operation's
service after its caller cancels: individual wait cancellation intentionally leaves
operation-owned acquisition available to other callers. The idle worker polls the
existing monotonic operation meter and exits on its stop or context cancellation.
Waiting callers also poll that meter; they do not wait only on Context.Done.
A kernel call that does not return can still delay worker shutdown and Close;
there is no claim that a goroutine or cancellation can interrupt such a call.
There is no per-waiter service goroutine or retained waiter registry. Flights and
queued work are reserved before allocation and run through one operation worker.

The service retains raw immutable facts/evidence, not an Observation bound to an
earlier requesting link. Bind every returned spec through Operation.Record for
that exact Request/View. Positive, missing, blocked, unknown and failure outcomes
remain first observations. Resource-only originals are shared across all views;
selected targets use exact view identities and sources, including unavailable
Saved targets. A cached miss stays missing after external appearance. Hard-link
logical names stay independent. A root handle is not a simultaneous tree snapshot.

Existence and content are independent capabilities. Existence-only images read no
binary bytes. The first later content read may see later bytes of the **same native
object/type**, validated by a private operation/key receipt before data acquisition
and the existing stable-read checks. Receipt identity does not require historical
size/mtime equality and retains no descriptor. Replacement/deletion/type/policy
changes refuse that upgrade while preserving prior existence and encountered IO.
The first acquired content or content failure is retained; cache hits never reread
it. Implicit non-Markdown anchors remain unavailable. RootDirectory uses the exact
root Location and returns the known Directory fact plus any independent stop.

ServiceLimits defaults are 5,000 submitted/selected files, 25,000 combined logical
identities, 25,000 distinct canonical lookup keys and 8 MiB original content.
The identity ledger is a union of keys from all targets/submitted files, supplied
entries and inferred parents, and queried keys including misses/blocked keys.
A no-key target counts as its own record without minting a report-derived key.
A queried seed is not counted again. Each distinct directory/index key is a lookup.
Known seeded excess fails preflight; runtime admission at a bound returns a typed
interruption and refuses new work. **No authoritative fact is evicted/reopened.**
Previously admitted facts remain retained, with the encountered stop reported.

Native actual retry bytes, enumeration, work and stability attempts still consume
the existing raw adapter counters. The native total-content policy must equal the
service's policy; targets seed that ledger once, and the service additionally
tracks retained logical content once. Supplied admission counts all submitted
payloads, including excluded seeds. Hits do not count input again or perform IO;
actual digesting, validation, parsing, request binding and wait polling remain
metered. Per-text/path/depth and cumulative node/time bounds remain in force.
Complete candidate/saved overlay aggregate admission remains its producer's duty.

Read returns both a spec and an independently retainable error. Transfer the initial Result value to `NewServiceReport(service, initial)`, then
append complete view batches with `owner.Append(view, observations, failures)` and
call `owner.Finish()` before shared finalization;
each ServiceFailure carries the matching Request and returned error. Do not also
project that batch through a raw adapter or View.AppendEffects. Real IO and actual
stops remain reportable even when File/Directory/Missing or acquired content
satisfies the requested capability. Missing/type ownership stays MD057/MD051 as
before. The caller transfers ownership of the initial result and its slices while the
report is active, and serializes mutation. Copies share a consuming Finish guard.
Full admission is validated before mutation; report-owned incremental cause maps
support sparse occupied IDs, multiple readers/views and fresh results without
retaining caller pointers or rescanning growing output for every error.
Root-only planning stops have no Request and remain caller-owned control effects.

`ParseKey(view, evidence)` and `Parse(ctx, view, evidence)` require this service's completed
ContentEvidence returned by Read. An opaque validation witness proves that those
bytes passed the adapter/service content admission; generic same-operation UTF-8
validation cannot admit unrelated new payloads into derived retention. The bounded parsed-source cache key includes SHA256,
complete immutable ParseOptions/frontmatter interpretation, parser/profile revision,
shared anchor policy, exact Source ownership and view. Same-byte foreign sources
cannot borrow another tree's spans or parser-budget ownership. The single worker
runs ParseWithOptions using the exact operation settings/budget; result records are
defensively copied and immutable Documents may be reused. Derived entries have the
finite identity-policy cap, with additional node/work accounting before retention.
A later parse stop cannot invalidate already completed ContentEvidence. Key hashing
and parsing are execution work and require a live meter. This seam computes no
heading/explicit-ID index and grants no transport, lint target or write authority.
An exhaustive navigation inventory cannot be inferred from the queried-key cache.

Native receipt/service fixtures are selected by the required TestFilesystem/
TestNative hosted Linux/macOS/Windows matrix. Current candidate checks run locally
on Darwin; compilation for other platforms does not prove runtime behavior. New
hosted execution remains a delivery gate before claiming those fixtures passed.


Service content upgrades require a proved first native receipt after a committed
nonselected File. A partial File left by exhausted unstable acquisition has no
such proof: content is cached unavailable, with that File and its actual IO/stop
retained, without another data read or validation/parse admission. Initial raw
reads, selected immutable snapshots and proved compatible upgrades are distinct.

Retained error evidence has immutable canonical read/decode classification and
messages. Reports use that classification rather than inspect a caller's mutated
Unwrap chain. Supported mutable PathError, LinkError, SyscallError and LimitExceeded
carriers are detached on retention and return; standard fmt wrapping and joined
causes are preserved as finite snapshots. Changing a returned carrier's Op, paths
or Err cannot change later cached messages, permission/context/limit provenance,
classification or source cause links. Copies preserve standard sentinel Is and
known carrier As inspection; copied mutable carrier pointer identity is not a
contract. Unsupported custom errors are opaque frozen identity leaves when their
values are comparable; custom Is/As/Unwrap callbacks are not delegated, and generic
deep cloning of arbitrary custom state is not promised.

Error metadata traversal is capped at 128 visited nodes, depth 32, 256 child edges
and 64 KiB of copied text, with explicit boundary/repetition markers. Cycles and
shared subgraphs cannot expand into cyclic/exponential returned traversal. Bounded
copy work/construction is charged through the existing operation meter; post-stop
copies reconcile already-known evidence. Canonical IO kinds/messages and cancel/deadline/limit facts (including limit
details) are private values, copied independently of optional metadata traversal.
Reports project every present control reason (at most cancellation, deadline and
the first exact limit) through the shared cause index. Reports reconstruct those
control facts even when returned inspection metadata
is changed or cut off. Nil children and typed-nil supported carriers become
finite metadata markers before any dereference or nil-unsafe Error call. Canonical
operational failure remains retained even when unsupported or excessive inspection
metadata is cut off.

## Computed anchor authority

`Service.Anchors` accepts only a content witness admitted by that service.
`Read` with `ParsedAnchors` obtains this computed index; `Parse` alone does not.
Parsing completes before the index is queued on the same operation worker.
Concurrent callers share production, while cancelling one wait does not cancel
another caller's index. `Close` joins the worker. Derived entries are bounded by
the finite parsed admission policy and the shared operation budget; there is no
global cache or retained history outside the operation.

`TargetAnchors` admits an exact already-selected snapshot by target ID and view.
This permits same-document analysis of outside-root or metadata targets with
`NoKey`, without minting resource keys or broadening cross-file access. Saved
unavailability never falls back to original content. Original bytes were counted
at service construction; each replacement has the per-text bound, and the
coordinated producer must still enforce the complete candidate overlay's
aggregate limit. Repeated requests reuse a bounded witness. Generic
`ValidateContent` evidence cannot bypass service admission. `Source.Token` is
only an opaque owner identity for ready-cache lookup, with no admission authority.
The full policy/digest/source key remains authoritative.

The `github` style flattens parsed heading text, code, formatting, link labels,
image alternative children and line breaks once. Raw tag spelling, destinations
and titles contribute no text. Unicode simple lowercase is applied without
normalization or case folding; Unicode punctuation is removed except `-` and
`_`, symbols are retained, and each whitespace rune becomes one hyphen without
trimming or collapse. All proven effective nonempty HTML IDs are reserved first,
including later occurrences. Headings then take their base or the lowest free
numeric suffix in source order. Empty bases remain empty, with no invented
fallback; the first empty ID is not a usable named target. Per-base suffix cursors
only skip proven occupied names.

The `explicit` style generates no heading IDs. Current CommonMark/GFM has no
explicit-heading-ID producer: `{#id}` remains ordinary text. HTML IDs on child
elements or raw HTML headings are general targets, not Markdown heading IDs.
HTML attribute names compare ASCII-insensitively, the first duplicate wins, and
values decode once in HTML attribute context without trimming, URL decoding or
case normalization. Empty and ignored duplicate attributes remain source
occurrences but confer no membership. Heading records retain visible text, level,
source envelopes and container ancestry separately from general ID occurrences.
Quote/list headings participate in anchors; future TOC consumers decide their
own eligibility.

`AnchorIndex.Lookup` accepts an already-decoded exact ID. Its empty successful
result proves absence only in a complete index. Zero, uncertain or interrupted
indexes cannot prove absence. Accessors copy mutable slices and charge the
original shared meter before scanning/copying. Construction charges syntax
extraction, ID bytes, occupancy/suffix work, retained nodes and occurrence copies;
no partial map is published on a stop. Required parse or HTML-context uncertainty
withholds the whole index because unknown explicit IDs can affect any suffix.
SVG/MathML entry is one such unsupported context: foreign integration points do
not grant ordinary HTML raw-text ownership from a local name such as title.
Already acquired content and its witness survive such deficits and actual
operational causes retain their existing report ownership. A completed index
also survives a later stop without content rehashing. The index itself introduces no MD051/MD073/MD080 callback, TOC fix, renderer,
sanitizer or transport capability. The fragment query seam below composes it.

## Fragment query completion

`NewFragmentResolver(service, normalizer)` binds the existing shared service to
one immutable source view. `Prepare(link)` accepts the original source-owned
parser-semantic Link and normalizes it once. It reserves exact source range,
SHA256/size/view metadata, a single-use query and at most three bounded request
carriers before acquisition or lookup can stop. These carriers do not themselves
perform or admit resource lookups. Copies share the query's consume guard.
If preparation stops, no query was admitted: retain the returned controlled error
as a producer-owned operation/source effect. Do not fabricate a resource Request,
rescan/hash the source after stop, or claim source coverage for an unadmitted
query. Prepare the required query batch while execution is permitted.

`Resolve(ctx, query, crossFile)` consumes that prepared query outside the service
worker. The typed outcome distinguishes no named query, an empty-fragment document
address, skipped distinct documents, known missing/type-inapplicable targets,
unavailable context, applicable malformed encoding, found names and proved named
absence. The outcome is authoritative; neither a nil error nor an available index
alone proves that the query finished. MD057 ownership never transfers when that
rule is disabled. No fragment, empty fragments and skipped references acquire no
resource/content/index. Their independent MD057 existence checks remain a separate
producer responsibility. Unsupported implicit resource formats with named queries
retain truthful existence but unavailable fragment interpretation.

Same-source aliases use the exact selected snapshot at any extension. Fragment-
and query-only links also work for selected outside-root/metadata NoKey sources,
without deriving a key from the report ID. For a distinct locator, pure planning
of the sole configured index can identify the current selected document before
the cross-file skip decision; only actual directory/root evidence authorizes the
index attempt. Unknown directory facts cannot prove a missing index. A regular
file with a directory hint is a mismatch, and a directory-valued index never
recurses or falls back. Parsing and anchors use only the operation's settings.

A complete index is retained independently of the final named lookup. Lookup uses
the Normalizer's once-decoded exact UTF-8 fragment: plus is literal, case and Unicode
normalization stay unchanged. Only a successful zero-match Lookup proves named
absence. Actual stops preserve the acquired content/index and yield query-specific
context unavailability. Returned occurrences are copied through `Matches(ctx)`
under the original meter; a later inspection stop does not alter the completed
query outcome. No fragment miss history or additional worker is retained.

Transfer the initial Result value and its slices to
`NewFragmentReport(resolver, initial)`. Append complete view batches through
`Append(view, resolutions)` using one owner for Original/Candidate/Saved, even
when each view has its own resolver. Finish consumes all aliases and transfers the
Result for the shared finalizer. Do not also project those resolutions' raw
observations through ServiceReport or an adapter. Whole-batch validation precedes
mutation, and the existing incremental cause indexes preserve collision-safe IDs
and actual IO/Internal plus cancellation, deadline and first exact limit causes.
Each unresolved query emits at most one affected coverage record with its own
original Link range/SHA/view, including NoKey and stopped available-index queries.
Known missing/type-inapplicable outcomes retain actual causes without inventing
MD051 coverage.
The opaque resolution retains the final anchor/query cause identity after a proved
directory selects its index. Anchor problems and terminal controls use that exact
identity; independently encountered directory-stage causes retain their own
identity. A successful directory never acquires a second copy of an index error. The resolver creates no diagnostics, checked files, changes,
transport fields, targets or write authority.

New fragment-native fixtures run under the required TestFilesystem/TestNative
hosted platform filter. Local Darwin runtime checks do not establish Linux or
Windows execution; the actual hosted matrix remains a delivery requirement.

## Derived context consumers

OwnsOperation is identity only. Service.Checkpoint lets synchronous derived
consumers share the existing operation meter and observe service closure together
with actual caller/operation/quota causes. It grants no resource, target or write
authority. Derived work runs outside the service worker and observes Close at
bounded checkpoints.
