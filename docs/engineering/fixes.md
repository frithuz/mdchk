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
for report-only rules or broaden the implemented correction scopes.

### Atomicity

A correction proposal is an atomic group of one or more edits. All component edits must pass source
preconditions, directive permissions and preservation checks before any are eligible. If any
required component is invalid, unsafe, blocked or waiting on a conflict, defer the entire group. Paired
fences, list-wide indentation and similar coordinated corrections must declare their grouping
explicitly.

### Duplicates and conflicts

Deduplicate byte-identical edits while retaining all provenance and group dependencies. Proposals
sharing an identical edit form one planning unit for all-or-none validation. A distinct
insertion at the same offset conflicts; insertion at or inside another proposal replacement boundary
is also a conflict. Adjacent nonempty replacement spans can coexist. A producer needing ordered
same-position insertions must consolidate them within its single atomic proposal. Independent
nonconflicting planning units remain eligible.

Only units that are otherwise valid take part in a conflict; a unit already deferred for
another reason keeps that reason and can't block a valid one. Each connected group of
conflicting units accepts exactly one winner: the unit holding the group that sorts first by
weight, then catalog rule order, then proposal ID. Every rule has weight 0 except MD047, the
final newline, which has weight 1 and so loses every conflict and is applied last. Each other
unit in the component gets reason `conflict-waiting`, with the winner's group ID in `Related`
(reported as `conflict_group_ids`), and none of its edits apply that round. The result doesn't
depend on proposal order.

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
Do not reuse stale offsets. A unit that waited on a conflict is not carried over: its producer
proposes it again against the new candidate, if it still applies, and it competes afresh. A unit
that keeps waiting until the round limit falls under the failure-to-stabilize rules below. Require a final planner/validation check with no further accepted
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

Intentional deferral for unsafe corrections does not by itself make completed lint
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

Amended by #261: there is no cross-process lock. The original design serialized cooperating
invocations with Darwin `flock`; that required platform-specific code and is removed in favor of
one portable implementation. Stale protection still detects concurrent changes to the source,
its dependencies and the parent immediately before the rename, but it does not serialize writers,
and a noncooperating writer can change a file between the last check and the rename. Reviewed
when MD019, MD021, MD023, MD029, MD009 and MD004 corrections joined MD047: no lock is added, intentionally, because mdchk
trusts one execution at a time, in line with #261. Stale protection is unchanged. Two concurrent
runs with the same configuration compute identical output. Runs with different configurations,
such as different MD009 settings, can compute different output, and a run can overwrite another
run's result after its last check. Full no-lost-update protection requires exclusive or coordinated
access. Do not imply that any editor or process honors a lock.

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

Preserve owner/group, access permissions, and (on Linux and macOS) every extended attribute,
which carries POSIX ACLs, security labels, quarantine and provenance. If the adapter cannot copy
that metadata, refuse the file rather than silently discarding it or relaxing access. Do not
clear read-only/immutable restrictions to force a write. Amended by #261: macOS ACL entries are
not inspected or preserved, and Windows alternate data streams, including Mark-of-the-Web, are
dropped on replace; both are accepted, documented limits. Content modification times and
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

Apply is enabled on Linux, macOS and Windows through one portable implementation (amended by
#261). On Windows, `os.Rename` is a same-volume `MoveFileEx` replace and is documented as nothing
stronger. Operating systems outside those three get a clear write refusal and can use dry-run; no
silent weaker fallback. There is no filesystem-type check: do not assume
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

After a complete validation, a saved run confirms and analyzes its result on a fresh
allowance of the same limits, so a spent budget can't report a successful write as
incomplete. `--limit` therefore caps each half of a saved run, and the context deadline
does not restart. [Saved fix runs](limits.md#saved-fix-runs) states the contract and when
status `15` can still accompany a `written` change.

### Design status

This decision sets product guarantees and accepted limitations. The #146/#147 native adapter was
replaced by the portable #261 implementation for Linux, macOS and Windows; see
[native write support](native-writes.md) for the accepted limits. CLI apply is implemented on it.


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


### MD009 corrections

`internal/rules.Corrections` regenerates one atomic MD009 group per active finding from
each round's own analysis and parse. Each group has a stable line-based ID, the
diagnostic span as its anchor and the `Meaning`, `ProtectedSyntax` and
`DirectiveStructure` requirements. `hard_break_spaces` comes from the target's resolved
plan. The single `Document.TrailingWhitespace` pass returns per-suffix facts, so MD009's
rule callback neither reparses nor scans protected nodes per finding. Literal code gets
no group. A suffix known to carry meaning, or a hard break that would grow past
`parser.MaxGrownHardBreakSpaces`, gets a zero-edit group with a `DeferReason`, which the
planner reports without blocking other groups.

The expansion limit is why `hard_break_spaces` can't grow a file arbitrarily. Growth is
capped per suffix, so it stays a few bytes per line, and the check runs before any
replacement is built, so a huge value allocates nothing. No shared growth limit exists:
MD010 caps its own growth the same way, and every other production fixer deletes bytes
or adds one line ending.

The independently registered checker recomputes the expected edits from the before parse
and trusts neither producer data nor `fix_available`. Through `CheckContext.Once`, it
proves `OnlyEditsChanged` and that both parses match in node kinds and order, break
kinds and origins, heading, list, table, code, task and link properties, and all
reference definitions. Text values may differ only in ASCII spaces and tabs directly
before a break. Each group then checks its own span, original bytes and replacement. Any
mismatch returns a non-`Preserved` proof, and the shared `commonPreservation` checks stay
mandatory. No `ForbidNewFindings` is registered: changing line-end whitespace outside
comments cannot create a directive finding.

`editMapping` lookups binary-search cumulative deltas, so preservation checks don't walk
the edit list per node. A lint target's projector charges each position's search and
line-prefix cost rather than the whole source, so one finding per line doesn't exhaust
the budget. Budget exhaustion and validation failure stay fail-closed, with no partial
candidate.

An MD009 removal touching MD047's EOF insertion conflicts with it. MD047 loses by weight, so
the removal applies first and MD047's insertion, proposed again on the next round's candidate,
adds the final newline: `foo<SPACE><SPACE><SPACE><EOF>` becomes `foo<NEWLINE>`. MD009
therefore sets `fix_available` for a suffix at EOF as for any other suffix. A document
opening with an unrecognized block is the exception: `Corrections` makes no MD009 group
there, not even a deferral, so nothing touches MD047's insertion and it applies alone.

### MD010 corrections

`internal/rules.Corrections` regenerates one atomic MD010 group per active finding whose
TAB sits in inline text, from each round's own analysis and parse. The round fact,
`engine.TabCharacterFacts`, joins `Document.TabCharacters` with
`Document.InlineTextTabs`: a TAB in the exact Text content of a paragraph, heading or
table cell, outside image descriptions, with a non-whitespace byte of the same block
before it and after it on its line. Indentation, container prefixes, code, HTML,
destinations, titles, definitions, delimiter rows and cell trimming own no such Text,
so they get no group at all. Line-end TABs are excluded too: they belong to MD009, and
spaces there could form a hard break.

Each group has the ID `MD010-byte-<offset>`, the one-byte finding range as both anchor
and edit span, and the `Meaning`, `ProtectedSyntax` and `DirectiveStructure`
requirements. The edit replaces the TAB with `spaces_per_tab` spaces from the target's
resolved plan. When that exceeds `parser.MaxTabSpaces` (8), the group has no edit and
the `expansion-limit` `DeferReason`, and the finding's `fix_available` is false. The
cost is charged before the spaces are built, and the cap keeps growth to a few bytes
per TAB.

The independently registered checker recomputes the facts and expected edits from the
before parse and trusts neither producer data nor `fix_available`. Through
`CheckContext.Once`, it proves `OnlyEditsChanged` and `SameMeaningAcrossTabs`: both
parses match in node kinds and order, break kinds and origins, heading, list, table,
code, task and link properties and all reference definitions, and Text values match
when every TAB on either side reads as `spaces_per_tab` spaces, compared without
allocating the expansion. Each group then checks its own span, original byte and
replacement. No `ForbidNewFindings` is registered: replacing a TAB in inline text
outside comments cannot create a directive finding.

MD010 groups take a round of their own. Every other proof compares the Text an MD010
edit changes, and the MD010 proof compares exact Text elsewhere, so `Corrections`
proposes MD010 edits only in a round with no other edit, MD009 and MD047 included, and
holds MD047 and MD003 back while they are pending. A line with a TAB and trailing
whitespace, or a file with a TAB and no final newline, settles over the rounds of one
`fix`.

### Production rule integration

`internal/rules.Corrections` regenerates MD047, MD004, MD009, MD005, MD019, MD021, MD022, MD023, MD029, MD030, MD032, MD063, MD007, MD077 and MD076 groups from each
current round's actual shared analysis and source-owned diagnostic ranges. For
MD047, the source's retained physical-line index supplies the same terminal facts
as analysis; the shared `fix.InsertionEnding` chooses inserted spelling without
normalizing input. The independent `rules.CorrectionCheckers` registration proves
the precise minimal suffix transformation and unchanged bytes outside the round's
edits. Complete shared parser, protected envelope/payload/binding and mapped
directive checks remain binding. The engine's bounded terminal eligibility query
withholds positive fix flags when syntax, permissions or EOF protection cannot
establish safety.

Interior edits use `engine.InteriorEditQuery`, or `RuleContext.InteriorEdits` in a
rule callback. It answers with the same protection sources as `fix.Plan` (literal
spans, front matter and directive comment candidates) and `AllowsEdit`. It returns
false for hidden observations, disabled anchors, unreliable syntax, incomplete
parses, any parse uncertainty and a document opening with an unrecognized block. A
rule run builds its protected-span index at most once, on the first finding that
needs it.

MD022 insertions use `RuleInteriorEdits.InsertionEligible`, which adds a check that
the neighbouring physical line touches no code or HTML block envelope, delimiter
lines included. `internal/rules/internal/headingblanks` derives each deficient
side's insertion boundary, shortfall and quote prefix from `HeadingSpacing` and
`Headings` facts, for the rule, the producer and the checker alike. Sides sharing a
boundary insert the larger shortfall, as identical edits that the planner applies
once. The trusted checker proves once per round that only the round's edits
changed bytes, every heading keeps its level, style, text and containers
(`CheckContext.SameEnvelope`) and the document keeps its meaning, then requires
each group to be the exact insertion Before's facts require.

`parser.Document.UnrecognizedBlock` reports a complete `---` or `+++` block opening
the source while recognition is off or the format is unselected. Such a block may be
real metadata, or a pair of thematic breaks, and each round's own parse answers this
once with the shared delimiter detection, so no fact is carried between rounds or
from saved input. `InteriorEditQuery` and MD009's eligibility return false, and
`Corrections` makes no MD009, MD032, MD007 or MD076 group, and MD004, MD005, MD019, MD021, MD022, MD023, MD029, MD030, MD063 and MD077 are withheld by their flags. MD047 stays, because a line
ending at EOF can't change a value inside the block.

Combined rounds propose all of a document's eligible corrections together, so a
document whose edits are all accepted finishes in one changing round plus the
read-only final check, or two when MD076 edits come first. A failing checker still defers every group in the file for
that round, so a producer must not propose an edit its checker would reject. An
MD047 finding without `fix_available` is therefore held back until no other
correction remains, and is then proposed alone: it defers as it always did, without
costing the corrections already retained. MD047's checker proves its own
suffix edit and `CheckContext.OnlyEditsChanged`; task sources compare through the
edit mapping rather than by absolute offset. In a round with other edits, a task fact
those edits touch may differ only in its line-end whitespace.

A checker runs once per accepted group, and `BoundedViews`, `Edits` and
`ForbidNewFindings` charge whole-round costs on every call. Proof that spans the
round goes through `CheckContext.Once`, which computes it at most once per file and
round. MD019 and MD021 compare heading count and order, level, style, closed state,
visible text, raw labels, containers, mapped envelopes and anchor bases there. Each
group then only checks its own edit against the before parse: one deletion of the
second through last space of a parser-confirmed spaces-only gap that belongs to the
group's rule. A Setext label is mapped by its endpoints, because an MD023 deletion
in the same round can lie inside it.

MD023 groups hold every leaf-indentation deletion of one heading, taken from
`parser.IndentedHeading` line spans, and are anchored on the heading's marker. Its
checker proves once per round that only the round's edits changed bytes, that the
block tree is unchanged apart from line-end whitespace
(`MeaningPreservedAcrossLineEndWhitespace`), and that every heading keeps its
level, style, closed state, visible text and container kinds. Each group must then
equal the before parse's deletions for a heading with no lazy line or partial
tab, and the reparsed heading must no longer be indented.

MD063 shares one selection of required initials between the rule and
`internal/rules/internal/headingcase`. The rule sets `FixEligible` only when every
initial of the heading is a literal scalar with a single titlecase form, is clear
of proper-name ambiguity and passes `InteriorEdits`. The producer regenerates one
group per heading from the round's heading scalars and options, with one edit per
initial. Because MD009, MD019, MD021, MD022 and MD023 checkers prove visible text unchanged,
MD063 waits until no such edit can reach proof. Its checker compares heading
count, level, style, closed state, raw label ends, containers and envelopes in
`Once`, requires visible scalars to differ only at correctable initials of the before
parse, and requires the candidate heading to report no MD063 finding. Each group then
checks that its edits are exactly that heading's titlecase replacements.

MD032 inserts one blank body line per finding at the start of the later
participant's line. `Document.ListSpacing` reports that line's active ancestor
prefix, the previous line's ending and whether the line would separate two
blocks of an item in a list not known to be loose. The analysis grants
`fix_available` only when the prefix holds just spaces and `>` markers and the
list stays tight or loose as it was; the inserted bytes are the prefix without
trailing spaces plus that ending. Its checker proves `OnlyEditsChanged` and the
same whole-document comparison as MD009 once per round, then recomputes each
group's line from the before parse. A "before" and an "after" insertion at one
offset, or one at another group's boundary, conflict in the planner, and the loser waits
for the next round.

MD005's checker compares the list item facts of both parses in `Once`: the same count and order,
marker kind, width and padding, an unchanged marker, and group, item, parent and
prefix spans that map through the round's edits. It also records which candidate
items sit at their group's first-item indentation. Each group then checks its own
edit: the spaces-only indentation of an eligible before item, replaced by the
group baseline's spaces, with that item now aligned. The MD009, MD022, MD023 and
MD032 checkers compare whole parses across line-end whitespace, which a moved
marker breaks, so `Corrections` proposes MD005 groups only in a round with no edit
from those rules; they apply in a later round of the same run.

MD029's checker in `internal/rules/internal/orderedprefix` compares list item
count and order, ordered state, delimiters, numbers, padding and content
indentation, and the mapped group, item and parent envelopes, then walks both
block trees comparing kinds and mapped envelopes. Only items its own edits
renumber may change number or marker width. Each group must equal the edit the
producer's shared `Propose` derives from the before parse. `Corrections`
proposes MD029 groups only in a round with no MD022, MD023 or MD032 group. The
MD009 and MD005 checkers require unchanged list numbers, so MD009 suffix groups
are held back while any MD029 group remains, and MD005 and MD063 groups wait
for a round without one.

MD007 makes one group per eligible finding, anchored at the bullet marker. Its
edits add or remove the same number of spaces at the start of the marker line and
of every later nonblank line up to the item's end. The shift comes from
`bulletOffsets`, the helper MD007's analysis uses, over `RoundContext.BulletIndentation`
(the round's list facts and resolved options). Eligibility requires those lines to
hold no tab, blockquote prefix or lazy continuation, a shift right of at most 16
columns, and `InteriorEditQuery` approval over the whole affected range. Only the
outermost of nested findings gets a group in a round, so groups never overlap, and
the next round regenerates the rest. MD007 groups wait for a round with no MD009,
MD022, MD023, MD029, MD032 or MD005 edit, because those checkers compare list
items or columns that a shift moves, and MD063 waits for MD007 groups. The
checker, through `CheckContext.Once`, proves `OnlyEditsChanged`, that both parses
match under `Document.SameMeaningAcrossIndentation` (the MD009 comparison with list
content columns allowed to differ), and that every list item keeps its order,
parent, list membership, kind, bullet, ordered start and delimiter, marker width and
padding. Each group then recomputes its exact line-start edits from the before parse
and requires the candidate's corresponding item to match MD007's expected offset.

MD077 groups come from `parser.ListContinuationFact`, whose `SpaceOnly` marks a
line with ASCII-space indentation through the span, no quote prefix or quote
ancestry, and no fence opening whose indentation strips payload bytes. Analysis
asks `InteriorEditQuery` about the deleted span, or for a deficit the first byte
after its insertion point, and withholds every edit after any leading front matter
block. The producer regenerates each edit from the round's own facts: a deletion
of the surplus span, or `Expected - Actual` spaces inserted at the deficit point.
Its checker recomputes those facts from the before parse, then in `Once` proves
`OnlyEditsChanged`, meaning across both parses and every block envelope mapped
around the edits, so each line keeps its owning item and block kind. MD077 groups wait for a round
with no MD009, MD022, MD023, MD029, MD032, MD005 or MD007 edit, and MD063 waits
for them.

MD030's producer, `listSeparatorGroups`, builds one group per `fix_available`
finding from the round's `parser.ListSeparatorFact` inventory. The parser fact
says whether the item is on one line with no nested item (`OneLine`), whether the
code-indentation fallback applies (`CodeFallback`), the separator column, the
column of the next nonblank line after the item and whether the item is quoted.
Each edit replaces the separator with exactly `spaces` spaces. The checker,
`checkListSeparator`, proves in `Once` that only the edits changed bytes, that the
document means the same apart from list item content indentation
(`CheckContext.MeaningPreservedAcrossIndentation`), and that list item count,
order, kind, bullet or ordinal and delimiter, marker width, mapped marker, list
and parent are unchanged. Each group then checks its edit against the before
parse's correctable separators. MD009's checker uses the same meaning comparison,
because a line-end edit never moves a content column and an MD030 edit sharing the
round is proven by its own checker. `Corrections` proposes MD030 groups only in a
round with no MD022, MD023, MD029 or MD032 group, whose checkers compare list items
or whole parses that a separator edit moves, and MD005, MD007, MD077, MD063 and
MD003 groups wait for a round without one.

MD076 groups come from `internal/rules/internal/listspacing`, which derives each
separator edit from the round's `RoundContext.ListItemSpacing` facts and source
alone: an insertion of prefix-only blank lines at the next sibling's marker line, or
a deletion of whole surplus lines. The rule's eligibility uses the same derivation
and `InteriorEditQuery`. Its checker recomputes every edit from the before parse in
`CheckContext.Once`, proves `OnlyEditsChanged` and compares both parses with
`CheckContext.MeaningPreservedAcrossListSeparators`, where only a list owning a
corrected separator may change tightness. That tightness change fails MD009's
line-end whitespace proof and the other list and whole-parse proofs, so while any
MD076 group exists `Corrections` proposes no other group; they follow in later
rounds. MD047 still joins the first round when
its finding is eligible.

MD004 proposes one group per fixable finding, replacing the bullet byte with the
expected marker. Analysis grants eligibility to a whole list or none of it, so a
round never splits a list, and withholds a list beside another bullet list, which
would merge with it, or one whose item would read as a thematic break. Its checker
compares, once per round, the mapped block tree of both parses: block kinds, order
and envelopes, and the ordered state, start, delimiter, tight state and widths of
every list and item. Each group then checks its own edit against the before parse's
list items and baseline, and that the candidate item carries the expected bullet.
MD004 groups come right after MD076 and share their round only with MD009, MD019,
MD021 and MD047; every other group waits for a round without them. MD004 edits
count as waiting edits for MD047, as heading edits do. MD009's line-end meaning
comparison ignores bullets, which only an MD004 edit can change.

Future corrections add rule producers, separately registered trusted checkers,
implemented catalog metadata and affirmative per-finding eligibility. Compose
producers at the existing `configuredCorrections` boundary; command parsing,
selection, reporting and validation need no rule-ID dispatch. Producers cannot
self-authorize preservation through requirement flags. A new correction needs no
code for an unrecognized leading block: `fix.Plan` defers, with reason
`protected-syntax`, every edit in such a document except one line ending inserted at
EOF or line endings deleted up to EOF. It decides by edit shape, not rule ID. The
rule's eligibility should still use `InteriorEditQuery`, so `fix_available` stays
accurate. Keep ambiguous rules report-only until independent proof and production acceptance exist. Retained
compiled fixtures in `internal/rules/md047/testdata/md047_corrections.json` are available
for later consumer acceptance; they do not claim filesystem apply or MCP support.
`internal/rules/md019/testdata/heading_spacing_corrections.json` and
`internal/rules/md007/testdata/md007_corrections.json` run through preview and saved modes.

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
MD033 produces no edits, eligibility, previews or corrected contents. Apply on an unsupported operating system returns an early
complete I/O refusal 13 with empty analysis/effects and unchanged files; a build
without cgo applies normally.

## MD086 manual findings

MD086 unmatched-comment analysis is enabled by default for CommonMark and GFM,
with no rule-specific options and permanent NoFix. It emits no edits, eligibility,
previews or corrected content. An unmatched opener in an ended quote owner remains a manual MD086 finding when
MD047 adds an eligible final LF to later ordinary text. An unclosed comment at
physical EOF protects that EOF, so MD047 remains deferred and MD086 remains manual.
Candidate and saved diagnostics retain their exact source identities.

## MD087 manual findings

MD087 is permanently NoFix. It never produces edits, previews, safe-checker
work or corrected contents. Hidden MD047 observations bypass terminal edit
eligibility before its protection checks or GFM candidate parse; ordinary
active MD047 findings retain their established correction behavior. The engine
also bypasses eligibility for hidden findings discovered by a reused ordinary
callback. Separately authorized MD047 fixes recompute the audit on candidate
source using the original operation's residual allowance and on actual observed
saved source using the allowance described under
[saved fix runs](limits.md#saved-fix-runs), preserving the remaining manual MD087 finding.
