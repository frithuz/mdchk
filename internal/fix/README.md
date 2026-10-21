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

Issue #144 supplies dry-run/report adapters; #145 supplies the first MD047
producer and newline insertion convention. Filesystem publication starts with
#146. This package adds no CLI command, public result records, production producer,
positive fix flags, temporary files or writes. Reusable byte fixtures in
`testdata/planning.json` cover the structural contract for later consumers.
