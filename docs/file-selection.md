# File selection and exclusions

Use this guide to understand which files an operation targets and why a path might be skipped. The command guides describe each operation's flags.

The shared filesystem selection provider implements the lint/fix path rules
below; compiled lint still stops after configuration preparation. Source loading
and command integration follow separately. See the [provider boundary](engineering/file-selection.md)
and [current lint status](commands/lint.md#current-implementation).

## Selection by operation

| Operation | Target | Exclusions and boundaries |
| --- | --- | --- |
| `lint` and `fix` | Named files or recursive directory scans; no paths means the working directory | One selected configuration supplies exclusions. Git metadata and symlinks are skipped. Git ignore support is deferred. |
| `config validate` | All eligible `.mdchk.yaml` files beneath the named project directory; defaults to the working directory | Git ignores and valid, scoped configuration exclusions apply. The scan never searches above its project directory. |
| `inspect` | Exactly one explicitly named file | Can inspect an excluded file and follows an explicitly named symlink for read-only analysis. Configuration discovery starts at the resolved target's directory. |
| Project MCP `lint` and `fix` | Paths relative to the server root; omitted paths select Markdown beneath it | Configuration discovery and file access stay within the server root. Symlinks cannot broaden access. |
| Standalone MCP `lint` and `fix` | The submitted file names and contents | Exclusions match submitted relative names. No files are loaded or saved using those names. |

Lint and fix directory scans select `.md` and `.markdown` files using ASCII-case-insensitive extension matching, including `.MD` and `.Markdown`. An explicitly named regular file is eligible regardless of its extension. Naming a file explicitly does not bypass configuration exclusions. A skipped file is not a checked file; read the report's skip reasons before assuming it passed.

Lint or fix selecting no files is a usage error (`11`). A config validation scan finding no definitions succeeds (`0`), provided the scan completed without errors. Missing or unreadable lint and fix targets are file errors (`13`).

## Resolve paths and configuration

Terminal paths are relative to the command's working directory unless absolute. A relative `--config` path is also resolved from that working directory. Explicit configs must stay inside the operation's established discovery boundary and repository scope, and cannot be reached through symlinks; see [configuration boundaries](configuration.md#choose-a-configuration-file). Use `--` before a lint or fix path beginning with a dash:

```sh
mdchk lint -- --draft.md
```

Lint and fix discover their single configuration upward from the working directory, rather than independently for each target. Naming a file in another directory does not select that directory's configuration. Use `--config` when you need an explicit choice:

```sh
mdchk lint docs/ --config .mdchk.yaml
mdchk inspect docs/guide.md --config .mdchk.yaml
```

Inspect normally discovers configuration from the resolved target's directory. Supplying the same `--config` to lint and inspect helps compare their behavior. Neither operation merges ancestor rule settings or applies nested rule overrides. Higher-level exclusion and directive permissions are checked separately; see [configuration discovery and permissions](configuration.md).

## Resource access is separate from selection

Link and anchor checks may read excluded or unselected resources without linting or fixing them. Resource lookup never selects another target, loads its nested configuration, or recursively checks its outgoing links. An excluded submitted standalone file can also supply a read-only resource snapshot; all-targets-excluded remains a usage error.

CLI resources are rooted at the nearest repository from the working directory. Outside a repository, use the selected configuration's directory only if it contains that working directory; otherwise use the working directory. Inspect applies that rule from the canonical target's parent. Project MCP uses its startup root, and standalone MCP uses a virtual request root.

Explicit CLI targets outside the resource root still receive ordinary lint and same-document anchor checks, but cross-file context is unavailable. Their presence does not enlarge the root. Logical resource names are case-sensitive on every platform. Resource paths reject every symlink component, including links contained within the root, and Git metadata. Inspect's explicit-target symlink exception does not grant linked-resource access. See the [resource contract](engineering/resources.md) for complete resolution and outcome rules.

## Configuration exclusions

Patterns are relative to the directory containing the selected configuration, not the working directory or each Markdown file. For example, a project-root `.mdchk.yaml` can exclude generated documentation:

```yaml
version: 1
exclude:
  - "docs/generated/**"
```

This exclusion also applies to an explicitly named target beneath `docs/generated/`. To inspect that target without changing it, use `mdchk inspect`.

During `config validate`, each valid definition contributes exclusions scoped to its own directory and descendants. Ancestor exclusions remain in force, and a nested definition cannot reopen a skipped directory. A definition is validated before its own exclusions are applied; it cannot exclude itself. Invalid definitions contribute no exclusions. See the [validation scan guide](commands/config/validate.md#understand-scan-exclusions).

Configuration exclusions and Git ignore patterns are separate mechanisms. `.mdchk.yaml` does not support Git negation or implicit basename matching at every depth.

## Exclusion pattern grammar

Patterns match complete paths relative to the selected configuration's directory. Without a configuration, use the invocation root; standalone requests use their virtual root. An explicit target outside the configuration directory does not match that directory's relative exclusions. Normalize native input paths to `/`-separated relative names before matching; do not resolve pattern text as a native path.

| Syntax | Meaning |
| --- | --- |
| `*` | Zero or more characters within one path segment |
| `?` | One character within a segment |
| `[abc]`, `[a-z]`, `[^a-z]` | Character classes and ranges using Go `path.Match` semantics |
| `\` before a pattern metacharacter | Escape the metacharacter; a backslash is never a path separator in a pattern |
| `**` as a whole segment | Zero or more path segments |
| Trailing `/` | Match directories only |

A matched directory excludes its entire subtree, including explicitly named descendants. The matcher must consider ancestor directories when checking an explicit file; explicit selection cannot bypass a directory exclusion. A terminal `/**` includes the directory itself as well as descendants.

| Pattern | Matches | Does not match |
| --- | --- | --- |
| `vendor` | The root `vendor` entry and, if it is a directory, its descendants | `packages/vendor` |
| `**/vendor/` | Directories named `vendor` at any depth and their descendants | A regular file named `vendor` |
| `docs/generated/**` | `docs/generated` and its descendants | `other/docs/generated` |
| `*.md` | Markdown names at the pattern base, plus descendants of any directory whose name matches | `docs/guide.md` unless an ancestor matched |
| `**/*.md` | Matching names at any depth, including the pattern base | `README.MD` because patterns are case-sensitive |

Reject empty patterns, leading `/`, drive-qualified or UNC paths, `.` or `..` segments, repeated separators, malformed classes or escapes, embedded `**` such as `ab**cd`, and longer star runs. A single trailing slash is allowed. Leading `!` negation and unescaped brace-expansion syntax are unsupported and are configuration errors; literal metacharacters can be escaped. Quote patterns in YAML, preferably with single quotes, to preserve backslashes and avoid YAML interpreting a leading `*` as an alias.

Validate every pattern before file selection, including patterns that match nothing. Matching is case-sensitive on every platform, independently of the underlying filesystem and the case-insensitive extension filter. Other hidden names and directories, including `.github`, are included. Wildcards can match a leading dot. Always skip `.git` and symlinks for lint/fix discovery; explicit inspection retains its documented read-only exception.

## Git ignores

`config validate` respects Git ignore patterns, including nested `.gitignore` files, even when a matching configuration is tracked by Git. A pruned directory's contents are not discovered or individually reported. Outside a repository, the scan honors `.gitignore` files within the selected project directory.

Git ignore support for lint and fix is deferred in the current design. A `.gitignore` entry alone therefore does not define their exclusions. Put Markdown paths you want skipped in the selected configuration's `exclude` list.

See [troubleshooting](troubleshooting.md) when selected files or configuration differ from what you expected.

When `integrations.quarto.enabled: true`, directory scans also select `.qmd` with the same case-insensitive extension matching. `documents.slides` only annotates selected documents; its patterns never select or exclude targets. See [optional contexts](configuration.md#optional-document-contexts).
