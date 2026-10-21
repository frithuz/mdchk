# Configuration discovery and Git ignore sources

`Complete` describes traversal completeness. Semantic definition errors can
coexist with a complete scan; source/traversal failures and interruptions make
it incomplete. Rejected definitions retain their error and cannot install state.

The internal `configscan` walker discovers definitions for the later config
validation adapter. It does not validate or activate definitions, implement the
command, or alter lint/fix selection. It returns owned source bytes and typed
failures, skips and interruptions. A directory callback can prune before reads;
a definition callback can install branch policy only on success. This permits
validation to retain valid ancestor policy after a child error.

Scanning is read-only and top-down inside one explicit directory. No mdchk
policy above that directory is opened. Git metadata, symlinks and nested
repositories are skipped. Metadata aliases on case-insensitive filesystems are
recognized by filesystem identity; a distinct `.GIT` on a case-sensitive
filesystem remains ordinary. Checked component identities and `os.Root` handles
confine reads; nonregular files are never acquired as definitions or policy.
Concurrent changes produce failures rather than following replacement symlinks.

Git ignores apply even to tracked definitions. Production does not run Git or
consult its index. Outside repositories only `.gitignore` files inside the scan
root apply; user Git configuration is not consulted. Inside a repository,
applicable parent `.gitignore` files up to its worktree root apply even for a
subdirectory scan. These Git names stay repository-relative; scan results use
native absolute paths, and mdchk state still starts empty at the scan root.

From lowest to highest precedence, sources are user excludes, common Git
`info/exclude`, parent `.gitignore`, and successively deeper `.gitignore` files.
Last matching line wins within a source. Pruned directories are never entered,
so an unread child ignore cannot reopen them. A failed applicable source stops
its affected subtree; independent siblings continue. Unseen descendants are not
invented in skip records. Source failures leave completeness false.

The matcher uses Git's byte-oriented wildcard grammar, slash anchoring,
slashless basename rules, `**`, directory-only patterns, negation, escaped `#`
and `!`, trailing-space escaping, bracket/POSIX classes, BOM and CRLF handling.
`core.ignoreCase` folds ASCII bytes, including classes, as Git does. Matching
source precedence is independent of tracking status. Development tests compare
against isolated native `git check-ignore --no-index`; verbose negation output
is not mistaken for exclusion.

Repository discovery supports regular `.git` gitfiles, relative `gitdir:` and
`commondir` paths, and worktree-local configuration when enabled. Configuration
is read in system, XDG, home, common repository and enabled worktree order.
`GIT_CONFIG_NOSYSTEM`, `GIT_CONFIG_SYSTEM`, `GIT_CONFIG_GLOBAL`, `HOME` and
`XDG_CONFIG_HOME` select those sources. Relevant simple Git config sections,
booleans, quoted strings and comments are supported. `core.excludesFile` accepts
absolute and `~/` paths; otherwise the XDG Git ignore default applies. Relative
excludes paths, config includes/conditional includes, relevant unsupported
escaping/continuations, and repository/config parameter environment redirection
are explicit incomplete source errors. They never silently become empty policy.
No include execution or arbitrary external program is performed. Callers may
supply an explicit environment snapshot to avoid ambient host configuration.

Defaults cap observed directory entries at 100,000, relative paths at 4,096
bytes/64 segments, each source at 1 MiB, aggregate source text at 8 MiB, and
source pattern records at 250,000 per source and 2 million operation-wide, and
operation work/time through the shared parser budget (250 million units and
60 seconds). Operator adapters may supply validated finite limits and a shared
remaining budget. Reads check sizes before allocation and meter actual acquired
bytes; enumeration reads one entry at a time and stops before materializing a
giant directory. Names are sorted only after bounded admission. Existing own
ignore/definition paths are checked against path/depth limits before any source
stat, acquisition, publication or callback. Missing hypothetical source names do
not exhaust those limits. Source parsing
and wildcard state visits consume the same work budget. Context/time checks
surround bounded loops and I/O; OS calls themselves are not preemptible.
Encountered failures survive later cancellation or limits. No write, retry,
index lookup or runtime Git subprocess is part of discovery.
