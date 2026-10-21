# Filesystem target selection

`internal/files.Select` implements the reusable lint/fix selection provider.
The compiled lint command still stops after configuration preparation; #126
adds source acquisition and integration, and later tasks add analysis and reports.
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

## Outcomes and later acquisition

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

`Empty()` describes target count alone. The later adapter must retain IO errors
rather than mask them with empty-selection usage behavior. A completed selection
with no targets produces the accepted lint/fix usage outcome; selection itself
does not choose exit codes or create public result records.

Selection is a filesystem snapshot, not a persistent authority to open a name.
The later acquisition provider must recheck symlink components and regular-file
identity, handle read failures and acquire exact source bytes without newline or
encoding normalization. A selected but unread file has not entered analysis and
must not appear in `checked_files`. Concurrent filesystem changes may invalidate
a selection; no stable path identity is promised across separate IO operations.

Tests combine real temporary trees with injected metadata/enumeration failures
and shuffled enumeration. They verify pruning before access, error causes,
independent successes, stable identities, native separator/escape semantics,
symlink and hard-link behavior, and unchanged source bytes.
