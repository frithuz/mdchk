# Correction planning and filesystem contract

Status: accepted on 2026-10-15. Structural in-memory atomic proposal planning,
original-source permission/precondition checks and provisional application are
implemented in `internal/fix`. Combined candidate semantic validation, mapped
common protection/directive checks, shared reanalysis and bounded convergence
with coupled rollback are implemented by its validated-stage API. Rule-specific
trusted checkers remain required. MD047 supplies the first production producer
and independent checker, with compiled text/JSON preview acceptance. Production native acquisition, stale protection and per-file replacement are implemented;
CLI publication and actual saved-content results are implemented on the verified native adapter. See [native write support](native-writes.md).
These requirements incorporate the revised **1 MiB per-file limit** where applicable.
See the [contract index](contracts.md) for related specifications and issue ownership.

## Atomic edit groups and convergence

### Scope

Use one shared correction planner for CLI dry-run/apply, standalone corrected content and project
MCP. Preserve the existing rule-specific correction limits; this does not authorize automatic fixes
for report-only rules or broaden the initial MD047 implementation scope.

### Atomicity

A correction proposal is an atomic group of one or more edits. All component edits must pass source
preconditions, directive permissions and preservation checks before any are eligible. If any
required component is invalid, unsafe, blocked or conflicting, defer the entire group. Paired
fences, list-wide indentation and similar coordinated corrections must declare their grouping
explicitly.

### Duplicates and conflicts

Deduplicate byte-identical edits while retaining all provenance and group dependencies. Proposals
sharing an identical edit form one planning unit for all-or-none validation. Defer every planning
unit involved in a genuine conflict rather than choosing a winner by rule order. A distinct
insertion at the same offset conflicts; insertion at or inside another proposal replacement boundary
is also a conflict. Adjacent nonempty replacement spans can coexist. A producer needing ordered
same-position insertions must consolidate them within its single atomic proposal. Independent
nonconflicting planning units remain eligible.

### Application

Generate each round against one immutable source/candidate snapshot with original-byte
preconditions, UTF-8-safe boundaries and intact CRLF sequences. Apply accepted edits from the end,
preserving unrelated bytes. Reparse and validate the combined candidate, including rule-specific
meaning, protected spans and unchanged directive structure/permissions. Separate-group safety alone
does not prove their combination safe. If combined preservation cannot be established, defer the
involved group(s), or conservatively the whole batch when they cannot be isolated; do not retain an
unverified partial correction.

### Repeated passes

After each validated changing round, reanalyze and regenerate proposals from the updated candidate.
Do not reuse stale offsets. Require a final planner/validation check with no further accepted
changes under the same inputs. Allow at most 10 changing rounds, followed by a read-only convergence
check. Record repeated candidate content to detect cycles early. The bound is initially fixed;
shared operation budgets can terminate work earlier and cannot be bypassed by repeated passes.

### Failure to stabilize

If candidates oscillate or still need accepted changes after 10 changing rounds, retain the original
content for the affected file and any coupled edit-dependency group; do not save or return an
arbitrary intermediate correction as the completed candidate. Report explicit incomplete fix
coverage and the reason, preserving reliable observations. Independently converged files may still
proceed after revalidation against the retained originals. Precise serialized outcome/reason names
belong to the results-and-limits area.

### Dependencies

Use a coordinated candidate overlay for all selected targets in each round. Track source and
resource observations that drive edits or prove their safety, including target-to-target
dependencies; coupled candidates must converge and validate together. If a rollback invalidates a
dependency, discard or revalidate dependent candidates before they are eligible. Final cross-file
findings must use the resulting complete candidate overlay, with saved-view reconciliation after
writes under the existing resource contract. A convergence failure does not authorize writes to
resource-only files.

### Remaining findings

Intentional deferral for conflicting or unsafe corrections does not by itself make completed lint
analysis incomplete: retain the underlying violations and explicit deferral reasons. Report-only
rules and deferred fixes can remain after a stable correction result. Distinguish these from failure
to complete requested fix computation, missing prerequisites or operational failures. Idempotence
means a second fix on the same final content/config/context makes no further changes, not that every
violation is fixable.

### Newlines

Preserve existing physical LF, CRLF and lone-CR sequences individually wherever the rule does not
explicitly add/remove them; never normalize the entire file as a side effect. For newly required
line endings, reuse the nearest existing complete line-ending sequence in the current source
snapshot by byte distance, preferring the preceding one on a tie; use LF when none exists,
independent of host OS. At EOF this uses the last existing terminator. Never split a CRLF pair. This
explicitly extends the preservation wording to lone CR recognized as a Markdown line ending.

### Md047

Keep empty files exempt. To add a missing final newline use the shared insertion convention. To
reduce multiple terminal line-ending sequences, preserve the first existing terminator following the
last non-line-ending source byte and remove only the excess terminators, subject to ordinary
protection and semantic-preservation checks. For an otherwise empty sequence of line endings retain
the first. Do not treat spaces on a terminal line as part of a bare newline suffix or silently
change intentional hard breaks.

### Reporting

Report final candidate differences rather than intermediate-pass diffs. Expose deferred correction
reasons separately from accepted proposed/applied changes. No project/CLI writes begin until
candidate construction and required validation for the operation have finished. Filesystem
stale-source checks and replacement guarantees follow the filesystem replacement section below.

## Filesystem replacement

### Guarantee

Use atomic per-file replacement on explicitly supported platform/filesystem adapters, with final
stale-source checks and coordination among participating mdchk writers. This provides whole-file
publication and refuses detected stale inputs; it is not compare-and-swap against arbitrary external
writers. A nonparticipating editor/process can change source or a dependency after the last check
and before replacement; its newer content can be overwritten without detection. Document that the
full no-lost-update guarantee requires exclusive/coordinated access during apply. Do not claim
guaranteed concurrent-writer protection from hashes, timestamps, advisory locking or atomic rename
alone.

### Coordination

Serialize overlapping write operations among cooperating mdchk invocations and within project MCP,
using an OS-backed coordination mechanism with stable logical target/parent identity and
deterministic acquisition order. Hold coordination across final validation and commit. An advisory
lock only on the original file inode is insufficient across replacement. Lock failure or bounded
waiting/cancellation prevents the affected commit, without an unlocked fallback. Finalize the native
coordination mechanism and its scope under #146; do not imply unrelated editors honor it.

### Preconditions

Retain the analyzed snapshot, stable native file identity/type and relevant metadata. Immediately
before commit revalidate the source bytes, directory-entry identity, regular-file type,
permissions/security metadata, and every content/existence/absence dependency used to justify edits.
A replaced file with identical bytes is still stale when identity changed. Refuse detected
appearance/disappearance, symlink/type substitution, metadata changes or changed edit dependencies.
Do not automatically rebase an old proposal onto new content. Report the affected
source/dependencies and keep independent work eligible.

### Confinement

Use native directory/file handles and race-resistant no-follow operations within the authorized
scope; do not rely on string path checks or an earlier symlink scan. Never follow symlinks/reparse
points when creating, validating or replacing a write target or resolving its parent chain.
Final-name replacement must operate on the directory entry rather than opening its referent for
writing. Detected substitutions are refused; acknowledge the concurrent-writer limitation for a
substitution after the final check. This does not broaden resource access, enable writes through
inspection symlink exceptions, or claim resistance to privileged mount changes.

### Replacement

Create a private exclusive temporary sibling on the same filesystem, write the complete validated
candidate, preserve required metadata, flush and check write/close outcomes, then publish with the
adapter’s supported per-file replacement primitive. Never fall back to truncating/writing the target
in place or deleting it before a replacement is ready. Keep candidate generation and all dry-run
paths free of temporary files/locks that write to the filesystem. No new backup-file workflow is
required for successful normal writes.

### Metadata

Preserve owner/group or equivalent ownership, access permissions and ACLs, security labels, extended
attributes and named streams where present and supported. If the adapter cannot inspect or preserve
relevant metadata safely, refuse that file rather than silently discarding it or relaxing access. Do
not clear read-only/immutable restrictions to force a write. Content modification times and
replacement identity may change; this is not a promise to retain inode identity or every historical
timestamp. Exact per-platform metadata support and preservation ordering must be documented and
tested before enabling that adapter.

### Hard links

Keep the existing per-logical-path contract: replacement changes only the selected directory entry;
other hard-link aliases retain the old inode/content unless separately selected and successfully
processed. Do not modify aliases in place or merge targets by inode. Make link-count/alias
consequences visible in write notes; do not claim hard-link topology is preserved by sibling
replacement.

### Platform support

Keep Linux, macOS and Windows as target platforms, but enable apply only for native
adapters/filesystems whose actual replacement, no-follow, metadata and failure behavior has been
implemented and verified. Generic os.Rename is not sufficient evidence of atomic Windows
replacement. Unsupported or unverified environments get a clear write refusal and can use dry-run;
no silent weaker fallback. Record the supported local-filesystem matrix; do not assume
network/cloud/virtual filesystems share local guarantees.

### Durability and failure

Separate atomic visibility from power-loss durability. Flush the staged content and the containing
directory where the native adapter supports the required flush, and report actual failures. Do not
promise survival of arbitrary hardware/filesystem failures. Track whether publication occurred
before a later flush, cleanup or cancellation failure: successful writes remain reported, uncertain
outcomes are marked uncertain and reacquired when possible. Cleanup only owned temporary artifacts;
report leftovers/failures. Do not roll back a published file in a way that could overwrite
subsequent external changes.

### Partial results

No whole-directory transaction or rollback. Continue independent files after detected
stale/unsupported/failed writes, retaining prior successes. Rebuild the actual saved view and rerun
affected cross-file checks; externally changed or uncertain documents are unavailable until a fresh
stable snapshot is obtained. A stale refusal/write failure remains an io error and exit 13 under
existing precedence even if the unsaved candidate was clean. Post-write checks describe observed
saved content, not a promise that external writers cannot change it later.

### Design status

This decision sets product guarantees and accepted limitations. The #146/#147 Darwin APFS native adapter and its precise metadata/coordination
support are implemented and tested; see [native write support](native-writes.md).
CLI apply is implemented on that verified adapter. Broader native-platform proof remains required for other platforms.


## CLI dry-run adapter

The `fix --dry-run` adapter resolves invocation-directory configuration with the
bounded resolver before target access, then shares one context and residual
operation budget through selection, acquisition, `Validator.Validate`, and diff
construction. It does not run an initial adapter lint pass or reopen targets.
Production MD047 corrections use the shared provider boundary and are exercised
through the ordinary compiled command. Build-time synthetic providers additionally exercise the adapter;
there is no runtime fixture switch.

Each changed file publishes one `Change` from original to retained final
candidate, with sorted contributing rule IDs and the number of accepted atomic
distinct applied edit records across validated rounds (identical shared edits
count once and byte-identical edits do not count). Deferred groups retain round-qualified
producer IDs, actual round source snapshots, anchors and conflict group links.
The authoritative final candidate findings retain candidate coordinates and
identities. Reliable rejected-source observations are explicitly identified by a
`provisional-observation` note and never authorize candidate content or changes.
Unchanged file write outcomes use schema-v1 `not-needed`; changed dry-run files
use `proposed`. CLI returned documents stay empty.

Final line diffs preserve LF, CRLF, lone CR and absent terminators independently
of text. A linear-space patience algorithm anchors unique common physical lines
and preserves common edges; unmatched regions become whole replacements. It uses
O(n log n) structural work and O(n) storage, avoiding a quadratic edit-distance
matrix. Three context lines surround each hunk. Diff byte scans, hash comparisons,
anchor searches and constructed records share the operation meter. An interrupted
diff is wholly omitted with an explicit interruption and exact section omission;
validated candidate identities remain available. Text escapes terminal controls
and marks absent final endings.

Before acquiring targets the adapter reserves their complete outcome and
worst-case proposal-identity manifest. Reporting omits whole diffs first, then
whole ordered detail records with exact known counts. It retains all target
outcomes and proposal identities. Before the independent reporting timer starts,
a pre-encoded essential envelope reserves those outcomes and at most four error
category summaries. Nanosecond reporting deadlines therefore deliver truthful
incomplete results rather than a synthetic output failure. Actual failed/short
writes return I/O status 13 unless an established internal failure requires 14.
No temporary files, locks, filesystem report spool or writes are created.


The correction adapter distinguishes preinitialized validator targets from targets
whose shared analysis actually started. An acquired but unstarted target keeps
its original identity, a `not-started` analysis outcome and explicit retry
coverage; it has no candidate snapshot and is absent from `checked_files`.
Accepted attempts later discarded by component rollback remain deferred groups
with their actual round snapshots, anchors and rollback reason. Only accepted
attempts discarded by the actual runtime rollback inherit its canonical
interruption/error links; completed intrinsic conflict, preservation
and nonconvergence decisions retain their own reasons and links. If the exhausted
meter prevents located detail construction, exact known whole-group omissions replace those
records. Independently converged candidates retain their proposed disposition.

Bounded selection uses the same residual matcher budget and context for both
ancestor and discovered-entry exclusions. Directory enumeration admits each
single-entry `File.ReadDir(1)` batch before constructing/retaining it, with a
fixed native buffering overhead; it never uses whole-directory `os.ReadDir`.
An EOF probe may consume one unused construction reservation, without refunds.
Only admitted entries reach deterministic ordering and later target selection.


### Production rule integration

`internal/rules.Corrections` regenerates MD047 groups from each current round's
actual shared analysis and source-owned diagnostic range. The source's retained
physical-line index supplies the same terminal facts as analysis; the shared
`fix.InsertionEnding` chooses inserted spelling without normalizing input.
The independent `rules.CorrectionCheckers` registration proves the precise
minimal suffix transformation and unchanged body bytes. Complete shared parser,
protected envelope/payload/binding and mapped directive checks remain binding.
The engine's bounded terminal eligibility query withholds positive fix flags
when syntax, permissions or EOF protection cannot establish safety.

Future corrections add rule producers, separately registered trusted checkers,
implemented catalog metadata and affirmative per-finding eligibility. Compose
producers at the existing `configuredCorrections` boundary; command parsing,
selection, reporting and validation need no rule-ID dispatch. Producers cannot
self-authorize preservation through requirement flags. Keep ambiguous rules
report-only until independent proof and production acceptance exist. Retained
compiled fixtures in `cmd/mdchk/testdata/md047_corrections.json` are available
for later consumer acceptance; they do not claim filesystem apply or MCP support.

Original selected-file UTF-8 failures remain local observations during correction
validation. They withhold their correction component and selected-target proof
dependents, including resource edges, while independent analysis and approved
corrections continue. Blocked peers retain justified original findings and receive
no producer, final candidate reanalysis or private publication approval. The final
observed snapshot reports its encoding error once. Generated invalid replacements,
other shared failures and runtime stops retain fail-closed behavior.

MD033 embedded-HTML analysis is available for CommonMark and GFM, disabled by
default and permanently NoFix. Full original tag ranges remain manual, including
candidate and saved analysis after an independent MD047 newline correction.
MD033 produces no edits, eligibility, previews or corrected contents. MD047 is
the sole current fixer. Unsupported native apply, including CGO0, retains its
early complete I/O refusal 13 with empty analysis/effects and unchanged files.

## MD086 manual findings

MD086 unmatched-comment analysis is enabled by default for CommonMark and GFM,
with no rule-specific options and permanent NoFix. It emits no edits, eligibility,
previews or corrected content. MD047 remains the sole current correction producer.
An unmatched opener in an ended quote owner remains a manual MD086 finding when
MD047 adds an eligible final LF to later ordinary text. An unclosed comment at
physical EOF protects that EOF, so MD047 remains deferred and MD086 remains manual.
Candidate and saved diagnostics retain their exact source identities.
