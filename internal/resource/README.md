# Read-only resource contracts

Issue #249 establishes engine-facing types and fixtures. This package is not a
production resource resolver, and no engine callback or advertised capability is
registered. Adapters, bounded acquisition/cache, anchor indexes,
rule consumers and coordinated fix execution remain issues #251–#260.

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
checked files, file records, returned documents, diagnostics or changes. Call it once with the complete view's
request batch, then use the existing engine finalizer. Existing exit precedence is
unchanged. Result mutation itself is caller-owned and must be serialized.

Construction and request projection charge the supplied shared budget; no query
resets it. Immutable snapshots/views/observations support concurrent reads. Result
projection after an interruption does not create a new execution budget. Concrete
acquisition checkpoints, aggregate content/cache bounds, singleflight, confined
filesystem operations and cancellation of actual resource work remain downstream.
The types cannot prove adapter truthfulness, inventory completeness or native
confinement; those remain explicit future adapter obligations. Adapters must use
the shared normalization pipeline below rather than reinterpret destinations.

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
Supplied roots never call this predicate and do not inherit server-OS filename aliases. Actual native
path rules and confined acquisition remain #251. Literal supplied keys from #170
are never passed back through URL decoding.

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
gate from #170 remains unchanged. No native root selection, supplied inventory
lookup, cache, anchor interpretation, production rule consumer or runtime resource
capability is enabled here.

Run a bounded normalization campaign with
`GOMAXPROCS=2 go test ./internal/resource -run '^$' -fuzz '^FuzzNormalizeDestination$' -fuzztime=20s -parallel=2`.
Fuzz oracles preserve logical confinement and once-only percent data; they do not
re-feed literal output keys as URLs and incorrectly demand decode idempotence.
