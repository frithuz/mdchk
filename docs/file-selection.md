# File selection and exclusions

Use this guide to understand which files an operation targets and why a path might be skipped. The command guides describe each operation's flags.

## Selection by operation

| Operation | Target | Exclusions and boundaries |
| --- | --- | --- |
| `lint` and `fix` | Named files or recursive directory scans; no paths means the working directory | One selected configuration supplies exclusions. Git metadata and symlinks are skipped. Git ignore support is deferred. |
| `config validate` | All eligible `.mdchk.yaml` files beneath the named project directory; defaults to the working directory | Git ignores and valid, scoped configuration exclusions apply. The scan never searches above its project directory. |
| `inspect` | Exactly one explicitly named file | Can inspect an excluded file and follows an explicitly named symlink for read-only analysis. Configuration discovery starts at the resolved target's directory. |
| Project MCP `lint` and `fix` | Paths relative to the server root; omitted paths select Markdown beneath it | Configuration discovery and file access stay within the server root. Symlinks cannot broaden access. |
| Standalone MCP `lint` and `fix` | The submitted file names and contents | Exclusions match submitted relative names. No files are loaded or saved using those names. |

Lint and fix directory scans select `.md` and `.markdown` files. Naming a file explicitly does not bypass configuration exclusions. A skipped file is not a checked file; read the report's skip reasons before assuming it passed.

Lint or fix selecting no files is a usage error (`11`). A config validation scan finding no definitions succeeds (`0`), provided the scan completed without errors. Missing or unreadable lint and fix targets are file errors (`13`).

## Resolve paths and configuration

Terminal paths are relative to the command's working directory unless absolute. A relative `--config` path is also resolved from that working directory. Use `--` before a lint or fix path beginning with a dash:

```sh
mdchk lint -- --draft.md
```

Lint and fix discover their single configuration upward from the working directory, rather than independently for each target. Naming a file in another directory does not select that directory's configuration. Use `--config` when you need an explicit choice:

```sh
mdchk lint docs/ --config .mdchk.yaml
mdchk inspect docs/guide.md --config .mdchk.yaml
```

Inspect normally discovers configuration from the resolved target's directory. Supplying the same `--config` to lint and inspect helps compare their behavior. Neither operation merges ancestor rule settings or applies nested rule overrides. Higher-level exclusion and directive permissions are checked separately; see [configuration discovery and permissions](configuration.md).

## Configuration exclusions

Patterns are relative to the directory containing the selected configuration, not the working directory or each Markdown file. For example, a project-root `.mdchk.yaml` can exclude generated documentation:

```yaml
version: 1
exclude:
  - "docs/generated/**"
```

This exclusion also applies to an explicitly named target beneath `docs/generated/`. To inspect that target without changing it, use `mdchk inspect`.

During `config validate`, each valid definition contributes exclusions scoped to its own directory and descendants. Ancestor exclusions remain in force, and a nested definition cannot reopen a skipped directory. A definition is validated before its own exclusions are applied; it cannot exclude itself. Invalid definitions contribute no exclusions. See the [validation scan guide](commands/config/validate.md#understand-scan-exclusions).

Configuration exclusions and Git ignore patterns are separate mechanisms. Do not assume `.mdchk.yaml` supports Git negation or every `.gitignore` pattern form.

## Git ignores

`config validate` respects Git ignore patterns, including nested `.gitignore` files, even when a matching configuration is tracked by Git. A pruned directory's contents are not discovered or individually reported. Outside a repository, the scan honors `.gitignore` files within the selected project directory.

Git ignore support for lint and fix is deferred in the current design. A `.gitignore` entry alone therefore does not define their exclusions. Put Markdown paths you want skipped in the selected configuration's `exclude` list.

## Selection details awaiting implementation

The complete configuration glob grammar remains undecided. The current documented recursive example is `docs/generated/**`; other wildcard forms, escaping, negation, directory-only patterns, and absolute or parent-relative patterns need a defined contract before release.

Case sensitivity for extension and exclusion matching, treatment of hidden directories other than Git metadata, and cross-platform separator normalization also remain unspecified. Until these decisions are implemented and documented, avoid relying on case folding, implicit hidden-directory skipping, or backslash-based exclusion patterns. Use the documented spelling and examples, explicit exclusions, and the checked/skipped report to review selection.

See [troubleshooting](troubleshooting.md) when selected files or configuration differ from what you expected.
