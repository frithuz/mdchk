# Filesystem target selection

`internal/files.Select` implements the reusable lint/fix selection provider.
`Acquire` loads bounded immutable source snapshots, and compiled lint connects
selection and acquisition to the admitted engine plan after configuration
preparation. The plan owns mandatory UTF-8 validation, parsing and registered
checks. Lint text and JSON reporters present the finalized shared result;
VS Code/GitHub reporters remain pending.
Selection does not read document contents, label targets as checked, discover
configuration, interpret Git ignores, access linked resources, or write files.

## Inputs and identities

An operation supplies `files.Options` with its working directory, terminal paths,
the selected configuration's directory and compiled `config.Patterns`. Without a
selected definition, the exclusion base is the invocation directory. Empty paths
scan the working directory recursively. The independent `Quarto` flag adds `.qmd`
to directory discovery; it does not assert production Quarto interpretation.
Current capability admission still rejects unsupported requested integrations.

Targets use cleaned absolute host-native paths. Relative arguments resolve from
the supplied working directory. Native separators are converted with
`filepath.ToSlash` only after deriving a name relative to the exclusion base.
On POSIX a literal backslash remains part of the filename. Pattern text never
passes through native path conversion. Outside-base targets remain unmatched
and do not select another configuration or enlarge resource permissions.

Directory discovery uses ASCII-case-insensitive `.md` and `.markdown` extensions,
plus `.qmd` when requested. Explicit regular files accept any extension. Hidden
entries are included. Every `.git` component and symlink is skipped, including
explicit ancestor components and dangling links. Mixed-case Git metadata names
are skipped when native file identity confirms they alias the sibling `.git`;
distinct `.GIT` entries on case-sensitive volumes remain ordinary content.
Exclusion pattern matching stays case-sensitive. Raw explicit components are
checked before cleaning can erase a symlink through `link/../file.md`.

The shared `Patterns.Excludes` checks each entry and applicable directory
ancestors. Excluded directories are pruned before enumeration, including for
explicit descendants, directory-only patterns and terminal `/**` patterns.
A trailing-slash exclusion never matches a regular file. Selection applies only
the prepared list; it does not accumulate ancestor lists or read nested configs.

## Selection outcomes

`Selection.Targets`, `Skipped` and `Errors` are ordered deterministically by
native absolute path, with stable reason/operation tie breakers. Cleaned aliases
and overlapping scans do not duplicate targets. Explicit nonstandard files
remove an extension skip produced by an overlapping scan. Hard-link names remain
distinct logical targets. Each directory is enumerated at most once.

A skip records its observed entry and reason: exclusion, Git metadata, symlink,
extension or nonregular entry. A blocking ancestor or pruned directory gets one
record; unenumerated descendants are not invented. Failures retain their effect
operation, path and underlying cause in `fs.PathError`. Independent targets
continue after missing paths, failed metadata access, matcher limits or directory
enumeration errors. No partial failure becomes an intentional skip.

`Empty()` describes target count alone. The acquisition adapter retains IO errors
rather than mask them with empty-selection usage behavior. A completed selection
with no targets produces the accepted lint/fix usage outcome; selection itself
does not choose exit codes or create public result records.

Selection is a filesystem snapshot, not a persistent authority to open a name.
Selection retains private component and final-file identities. Acquisition
rechecks them, handles read failures and acquires exact source bytes without
newline or encoding normalization. A selected but unread file has not entered analysis and
must not appear in `checked_files`. Concurrent filesystem changes may invalidate
a selection; no stable path identity is promised across separate IO operations.

Tests combine real temporary trees with injected metadata/enumeration failures
and shuffled enumeration. They verify pruning before access, error causes,
independent successes, stable identities, native separator/escape semantics,
symlink and hard-link behavior, and unchanged source bytes.

## Read-only acquisition

`Acquire(ctx, selection, options)` retains the selection separately from its
`Documents`, `Errors`, `PreflightErrors` and `Interruptions`. Each document owns a
`parser.Source`; neither modifying returned byte copies nor rewriting its file
changes the snapshot. Source acquisition does not validate encoding. The CLI
passes each snapshot to the admitted plan using the same operation budget and
context; it performs no separate encoding scan. The engine gate counts as
checked, then available enabled checks execute. Missing enabled implementations
produce coverage deficits and return 15. Invalid UTF-8 returns one IO error with
the original snapshot identity and
leading-byte location. MD094 controls attribution only.

Acquisition pins parent directories with `os.Root` from the target's filesystem
root, without restricting explicitly named files to the configuration directory.
It compares nonsymlink component and final regular-file identities against
selection, and rechecks the current path after reading. Git metadata aliases
are rejected again. Unix final opens use no-follow and nonblocking flags to
reject raced symlinks and avoid waiting on a FIFO. Unstable snapshots fail after
one attempt; acquisition does not retry or retain partial bytes. These checks do
not promise atomic contents against concurrent in-place writes that evade host
metadata observations.

Open, descriptor-stat, read, identity and close failures retain path, effect and
underlying cause. Independent files continue. A failed read or close never
publishes a snapshot, including a read returning both bytes and an error.
Selection IO failures also remain available and take precedence over empty
selection usage behavior. Only a completed, error-free empty selection returns
11; encountered IO returns 13.

`AcquireOptions` defaults to 1 MiB per document and 8 MiB cumulative bytes read.
Finite positive operator overrides require document bytes not to exceed the
aggregate or the supported 1 GiB per-document implementation bound. A known
oversized descriptor is a preflight failure; growth beyond a bound, aggregate
exhaustion, cancellation, deadline and shared work exhaustion are runtime
interruptions. Reads probe at most one extra byte to detect exhaustion and never
retain it. Failed-read bytes count toward aggregate acquisition. Acquisition
charges byte reads and retained copies to the supplied `parser.OperationBudget`.
The CLI passes that same budget to the admitted plan without another encoding
reservation or scan. `engine.Lint` owns the mandatory gate's `4n+1` work charge
for validation, line indexing and snapshot hashing/copies, where `n` is the
original source byte length. Parsing, rule work and diagnostic coordinate
projection continue against that shared execution budget under their own
charges. Context and shared deadline checkpoints surround reads; unresponsive
OS calls are not promised preemption. No resource contents are loaded. Discovery
counts and broader application limits remain pending; encoded response bytes
and the independent reporting deadline are enforced by the CLI reporter. The
command does not claim complete operation accounting.
