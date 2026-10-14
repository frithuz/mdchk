# mdchk fix

Apply safe corrections to Markdown files and report problems that still need attention. Preview changes with `--dry-run` before saving them.

```text
mdchk fix [options] [paths...]
```

## Select files

Pass one or more files or directories. Directories are searched recursively for `.md` and `.markdown` files. With no paths, fix processes the working directory. It uses the same selection and exclusion behavior as [lint](lint.md).

```sh
mdchk fix README.md docs/
```

## Options

| Option | Effect |
| --- | --- |
| `--config PATH` | Use the named configuration file instead of discovering one |
| `--format FORMAT` | Choose `text`, `json`, `vscode`, or `github`; defaults to `text` |
| `--dry-run` | Compute corrections and report the predicted result without writing files |

The [global options](../index.md#global-options) also apply. See [configuration](../configuration.md) and [result formats](../output-formats.md) for details.

## Preview and apply corrections

```sh
mdchk fix docs/ --dry-run
mdchk fix docs/
mdchk fix docs/ --dry-run --format json
```

Text and JSON previews include proposed diffs. VS Code and GitHub output show only findings remaining in the proposed content, so use [lint](lint.md) when highlighted locations must match files currently on disk.

Corrections preserve meaning and unrelated content. For example, fix can add a missing final newline while preserving intentional hard breaks. It leaves code examples, front matter, and ambiguous changes alone. A skipped heading level or missing code language needs a decision rather than a guessed correction.

After corrections, mdchk checks the resulting content and reports remaining findings at their new locations. The report identifies changed files and what was corrected.

## Understand the result

Exit code `0` means no violations remain after corrections. Code `10` means problems remain, including those that cannot be safely fixed. For a dry run, those codes describe the predicted content; even a `0` can accompany changes that still need to be applied.

Errors can occur after other files have changed. Inspect both successful changes and failures before retrying. If a file changes during fixing, mdchk refuses stale edits rather than overwriting the newer contents. See the [exit code guide](../exit-codes.md) for usage, configuration, I/O, and internal error codes.
