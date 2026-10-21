# Logical configuration patterns

`CompilePatterns` accepts the existing `Definition.Exclude` or
`Definition.Documents.Slides` string lists. It snapshots and validates the whole
list and returns no matcher if any pattern is invalid. `PatternError.Index` is
zero-based so a validator can attach the existing field/element `Origin` without
introducing another provenance representation. Compilation performs no I/O.

`Patterns.Match(name, isDir, inBase)` tests a complete entry directly.
`Patterns.Excludes(name, isDir, inBase)` also considers all ancestor directories
in the same matching pass, including the logical root. Use `Excludes` for target
exclusion, including explicitly named files. `Match` provides direct matching for
annotations such as slide patterns. The matcher never selects extensions or
walks files, and does not interpret Git-ignore syntax or permission policy.

The adapter establishes the pattern base, containment, native path normalization
and directory identity. It passes a UTF-8 logical name separated by `/`, with no
absolute prefix, drive qualifier, empty segment, dot segment, trailing separator
or NUL. The empty name denotes the base directory and requires `isDir=true`.
Backslashes in logical names are literal filename characters. An entry outside
the base uses `inBase=false` and returns false without inspecting its name. The
matcher never cleans `..` into scope or resolves native paths or symlinks.

Pattern segments use Go `path.Match` semantics, including Unicode characters,
`[^...]` negation and arbitrary backslash escapes. Whole-segment `**` adds zero or
more path segments. A single trailing slash restricts a match to directories.
A terminal `/**` includes its base directory, but does not match a regular file
at the base name. Wildcards match leading dots, and matching is case-sensitive.
Unescaped adjacent wildcard stars outside classes are invalid unless they form
the entire `**` segment. Escaped stars and stars inside classes are ordinary Go
pattern syntax. Unescaped braces outside classes and leading `!` are rejected.

UNC validation takes precedence over escape interpretation for a raw leading
pair of backslashes followed by another backslash, such as
`\\server\share`. A leading escaped literal backslash without that later
separator, such as `\\name`, remains valid. Forward-slash UNC forms are already
rejected as absolute patterns. Other backslashes escape pattern characters and
are never interpreted as Windows separators.

The zero value matches nothing. Compiled values are immutable and safe for
concurrent matching; each call owns its state. Matching keeps two memoized rows
of reachable pattern/name segment states. Ancestors are evaluated during that
single scan rather than restarting the scan for each ancestor.

The exported constants bound input sizes: 1,024 patterns; 4,096 bytes and 256
segments per pattern; 1 MiB total pattern bytes; 16,384 name bytes; and 1,024 name
segments. Each matching call has an 8,388,608 work-unit budget shared across all
patterns. A state visit costs one unit, and a standard-library segment match is
charged `(pattern bytes + 1) * (name bytes + 1)` before it runs. These conservative
bounds avoid reliance on elapsed-time assertions. Exceeding any limit returns
an error wrapping `ErrPatternLimit`; callers must propagate it rather than treat
it as an ordinary nonmatch. The limits do not change existing parser budgets.
