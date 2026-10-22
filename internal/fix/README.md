# Structural correction planning

`Plan` consumes one original `parser.ParseResult`, the shared engine's complete
`DirectiveResult`, the immutable rule registry, atomic producer groups and the
**existing residual** operation budget. It performs no I/O. A completed parse's
private operation token rejects constructed documents and replacement budgets;
source spans and directive authority must belong to the identical source owner.
Private registry and operation tokens also reject a different catalog instance or
a directive result built with another budget; callers retain the original registry.
Identical bytes from another acquisition do not authorize edits.

Groups carry a stable unique producer ID, canonical rule ID, original diagnostic
anchor, provenance, exact original-byte preconditions, replacement bytes and
preservation requirements. Inputs are copied. Empty, unknown or report-only rule
proposals are deferred. Catalog `planned` support is sufficient for this internal
structural stage, which deliberately does not register a correction producer or
claim `fix_available`. Missing or unknown preservation requirements fail closed.
`exact-bytes` requirements additionally protect their original spans across the
whole batch. Other recognized requirements remain obligations for the next stage;
a declaration is never evidence that meaning or syntax has been preserved.

Every edit has source-owned UTF-8 scalar boundaries outside CRLF interiors.
The original diagnostic start must be enabled, every replaced byte must avoid
disabled intervals, and insertion permissions use the exact position, including
the last instruction owning EOF. Read-only engine queries retain the disabling
instruction/token. Opaque code payloads, raw HTML and confirmed directive-comment
bytes cannot be incidentally rewritten. Insertions inside protected spans are
blocked; boundary insertions still require semantic and directive revalidation.
Fence delimiters are distinct from opaque code payload; any coordinated delimiter
change remains subject to the later combined-candidate proof.

Identical original-offset/replacement edits share one storage/application record
and retain all group provenance. Preconditions and permissions are checked for
**each** group before their transitive duplicate closure is accepted. A bad
precondition in one member defers the entire unit. Genuine conflicts defer every
involved unit without ordering priority. Distinct same-position insertions and
insertions at either replacement endpoint conflict; adjacent nonempty replacements
coexist. A sorted sweep tracks conflict connectivity rather than testing all pairs.
Decisions are sorted by group ID, edits by original byte coordinates. Deferral
`Related` IDs identify a canonical failing group or conflict component representative;
every member retains the same cause, and all groups remain in `Decisions`.

`StructuralPlan` owns all retained data. Accessors copy writable collections and
bytes. It exposes **provisional bytes**, not corrected content or a saveable result.
Application reads original offsets from right to left into one reserved output,
preserving untouched LF, CRLF, lone CR and all other bytes. The finite source-size
cap applies to the combined provisional output. The same budget meters input
ownership, validation, sorting, closure, protection queries and construction;
cancellation/limits return an error and a zero plan without partial output.

## Stage boundary and consumers

Issue #142 implements structural eligibility, original permissions, explicit byte
preservation and low-level in-memory application. It does **not** establish final
fix safety. Issue #143 must reparse the combined provisional bytes, reprocess
shared directives, prove protected syntax and rule-specific meaning, reconcile
findings, and implement bounded convergence/cycle handling. It must not treat
`Complete` or a group's `Accepted` as semantic approval. Baseline meaning,
protected-syntax and unchanged-directive obligations always apply, independently
of the producer's additional requirements. Only that later validated stage can
feed public proposed changes, returned corrections or persistence.

The CLI supplies dry-run/report adapters; rules supplies the first MD047
producer and newline insertion convention. Filesystem publication starts with
#146. This package adds no CLI command, public result records, production producer,
positive fix flags, temporary files or writes. Reusable byte fixtures in
`testdata/planning.json` cover the structural contract for later consumers.

## Validated candidates

`Validator.Validate` is the consumer boundary implemented by #143. It takes
immutable `Target` sources, their exact admitted `ExecutionPlan`, regenerated
producer callbacks, and `ValidationOptions` containing the same residual budget.
`NewValidator` independently freezes trusted rule-specific preservation checkers.
These are reviewed application code with bounded-work obligations, like engine
callbacks; they are never Markdown/configuration input or producer declarations.
No production correction is registered here. Unknown, absent or unsuccessful
proof cannot authorize a change. A checker must establish its rule's concrete
meaning policy, including any forbidden new finding, using the complete combined
candidate. AST equality, fewer diagnostics or requirement flags alone are not
proof. Baseline checks cannot be waived by a checker. Trusted registrations may
name `ForbidNewFindings`: these rules retain existing reliable findings matched
by mapped unchanged byte range, rule, severity and message, while new findings
defer the combined round. Generated IDs and shifted Unicode coordinates do not
identify a finding. Checkers can use `MapUnchanged` for additional source facts.

Every round uses structural `Plan`, then the actual shared engine parser,
directive processor and enabled callbacks on newly owned candidate sources under
the original plan. `InspectCandidate` gives every located result the candidate
view, exact SHA-256 and byte count. Reliable findings, suppressed observations,
notes, errors and unavailable-rule coverage remain; a remaining violation is not
a convergence failure. `Validated.Complete` describes correction computation,
independently of `FileView.Analysis.Complete` describing lint coverage. Engine
callback causes remain available through the returned error, and rejected runtime
candidate observations remain separately in `FileView.Observations`. These
observations never authorize returning/saving their provisional contents.

Common proof reparses the complete batch and maps unchanged source facts through
validated edits. Original comment envelopes, spelling, closing markers, source
order, protected status, directive interpretation, effects, restriction origins,
interval ownership and EOF state must survive. A safe preceding insertion may
shift their coordinates. New, removed, split or reinterpreted comments fail.
Opaque nodes additionally retain mapped full envelopes and node kinds. HTML
remains byte-exact. Code retains exact mapped body/info source facts and semantic
values; delimiter changes need trusted rule proof and cannot close on an interior
fence or change payload bindings. This supports a future specialized paired-fence
checker without implementing MD070 or authorizing changes through declarations.
Parser uncertainty likewise withholds correction.

Targets declare source dependencies; the transitive undirected closure defines
coupled rollback components and the source overlay visible to callbacks. All
members' provisional bytes exist before preservation callbacks see the overlay.
Combined failure conservatively defers the whole coupled round. Read-only
`ResourceObservation` entries distinguish content, authoritative absence and
unavailable context. A target declares its resource dependencies; missing or
unavailable observations make dependent correction computation incomplete.
Resource-only observations never enter `Paths` or become correction targets.
No resource acquisition/provider or cross-file engine implementation is invented:
existing unavailable engine checks retain their reduced coverage. Callbacks must
not consult undeclared mutable external context.

Each component regenerates proposals on its current immutable sources for at
most ten changing rounds, then runs a read-only planning and preservation check.
Byte-identical proposals do not count as changes. SHA-256 content identities,
with length framing in deterministic target order, detect cycles without
retaining historical source bodies for that purpose. A cycle or accepted eleventh
change rolls the entire coupled component back to retained originals with
`fix-nonconvergent`; an uncertain proof/context also withholds that component.
Independently converged components remain eligible. All retained final sources
are reanalyzed after rollbacks. A changed observation set under the same retained
bytes fails closed as `unstable-reanalysis`; an earlier proof never authorizes a
different final report. Cancellation, deadline, quota or runtime failures
return explicit incomplete computation and typed causes, with affected original
bytes and reliable observations, never a saveable unverified intermediate.
Intentional unsafe/conflicting deferrals can finish correction computation while
retaining findings. Final original/candidate bytes, rather than intermediate
edit offsets, are the authority for the downstream report's differences.

The caller owns original acquisition accounting. Each candidate overlay, including
retained read-only resource content, obeys `TotalContentBytes` (8 MiB by default,
finite operator override). Every document retains its parser byte cap. The same
meter charges source copies, parsing, structural planning, full source/protection
comparisons, directive views, actual constructed collections, encoded analysis
ownership, hashing, sorting, callbacks and all rounds. Callbacks checkpoint their
own loops and repeated view construction; the coordinator checks immediately
before and after each successful callback. No byte count is charged as a node
count. The coordinator shares each private immutable round edit set across checker
contexts. Public `Edits()` copies only when a checker requests a defensive view;
per-group preparation constructs and charges only that group, without eagerly
copying unused complete edit sets. Typed accessors return detached collections;
source handles are immutable.
No CLI records, filesystem effects, positive fix flags or production producers
are added by this validated-stage API. #144 and #145 consume it separately.


`FileView.RuntimeRollback` distinguishes accepted attempts discarded by the
returned runtime failure from completed intrinsic decisions in independent
components. Component errors mark their affected files; a final reanalysis or
checkpoint failure marks still-retained candidates actually rolled back by that
failure. Already rolled-back intrinsic components do not inherit the later
failure. Reporting can link synthesized accepted-group deferrals to that runtime
cause; existing conflict/preservation/nonconvergence decisions keep their own
provenance. An operation-wide error alone is not per-group causal authority.
