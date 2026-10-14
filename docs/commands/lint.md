# mdchk lint

Check Markdown files against the enabled rules and report problems without modifying files.

```text
mdchk lint [options] [paths...]
```

## Select files

Pass one or more files or directories. Directories are searched recursively for `.md` and `.markdown` files. With no paths, lint checks the working directory.

```sh
mdchk lint
mdchk lint README.md docs/
```

Configuration exclusions apply even to explicitly named files. Git metadata and symlinks are skipped. Missing or unreadable paths are errors; selecting no files is also an error. The report identifies skipped files and any checks that could not complete.

Git ignore support for lint and fix is deferred; use configuration exclusions for paths you want skipped. See [file selection](../file-selection.md) for path resolution, differences between commands, and selection details awaiting implementation, or [troubleshooting](../troubleshooting.md) for unexpected results.

## Options

| Option | Effect |
| --- | --- |
| `--config PATH` | Use the named configuration file instead of discovering one |
| `--format FORMAT` | Choose `text`, `json`, `vscode`, or `github`; defaults to `text` |

The [global options](../index.md#global-options) also apply. See [configuration](../configuration.md) for discovery and settings, and [result formats](../output-formats.md) for output examples.

## Examples

```sh
mdchk lint docs/ --config .mdchk.yaml
mdchk lint . --format json
mdchk lint . --format vscode
mdchk lint . --format github
mdchk lint -- --draft.md
```

## Understand the result

Each finding identifies its location, rule, explanation, and whether a safe correction is available. Use [fix](fix.md) to preview or apply those corrections. Lint remains read-only regardless of configuration.

Exit code `0` means the selected files pass the enabled rules. Code `10` means violations were found. Codes `11` through `14` identify usage, configuration, I/O, and internal errors. See the [exit code guide](../exit-codes.md) for details and error precedence.
