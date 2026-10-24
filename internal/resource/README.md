# Read-only resource contracts

Issue #249 establishes engine-facing types and fixtures. This package is not a
production resource resolver, and no engine callback or advertised capability is
registered. Normalization, adapters, bounded acquisition/cache, anchor indexes,
rule consumers and coordinated fix execution remain issues #250–#260.

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
The types cannot prove adapter truthfulness, inventory completeness, confinement
or normalization correctness; those are explicit future adapter obligations.

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
