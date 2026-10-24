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
