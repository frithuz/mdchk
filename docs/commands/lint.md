# mdchk lint

Check Markdown files against the enabled rules and report problems without modifying files.

```text
mdchk lint [options] [paths...]
```

## Current implementation

Build the executable with `go build -o mdchk ./cmd/mdchk`. Help, version, argument
parsing, configuration preparation, source acquisition and mandatory UTF-8
validation are available. Lint resolves its single
configuration from the working directory or `--config`, validates all settings
and ancestor permissions, and checks selected capabilities before target access.
Invalid or unreadable configuration returns `12`. Help and version need no
project access.

This build supports CommonMark configuration, but GFM, directives, optional
contexts and resource handling remain unavailable. Built-in defaults still select
GFM and therefore return `12` when no configuration exists. A configuration with
`flavor: commonmark` and `directives: {enabled: false}` can complete preparation;
an inherited global directive denial also satisfies the directive requirement.
Explicitly enabled unavailable rules fail preparation; unmentioned default rules
remain in the complete catalog for coverage reporting. Preparation retains an
immutable admitted execution plan for the shared engine; the actual CLI
analysis connection remains pending.

After successful preparation, lint selects targets using the reusable
[target selection provider](../engineering/file-selection.md), opens regular
files without following symlinks, and retains immutable original-byte snapshots.
Prepared exclusions are bound to the selected configuration directory (or working
directory without a file). Missing or unreadable targets and invalid UTF-8 return
`13`; successful selection with no targets, including excluded-only input,
returns `11`. Independent readable targets are retained even when another fails.
UTF-8 validation is mandatory regardless of rule enablement; MD094 only controls
error attribution. It reports the first invalid sequence's original byte offset
and Unicode position, retaining the original size and SHA-256 snapshot identity.

Valid source acquisition still returns `15`: Markdown analysis and production
rule execution remain pending. This build never reports a clean lint success.
Reporters are deferred to #128: `--format` accepts the documented names but does
not yet produce their reports or JSON envelopes. Errors use plain stderr text.

The remaining execution behavior on this page is the accepted target contract.

## Select files

Pass one or more files or directories. Directories are searched recursively for `.md` and `.markdown` files. With no paths, lint checks the working directory.

```sh
mdchk lint
mdchk lint README.md docs/
```

Configuration exclusions apply even to explicitly named files. Git metadata and symlinks are skipped. Missing or unreadable paths are errors; selecting no files is also an error. The report identifies skipped files and any checks that could not complete.

Git ignore support for lint and fix is deferred; use configuration exclusions for paths you want skipped. See [file selection](../file-selection.md) for path resolution, differences between commands, and the defined pattern grammar and case handling, or [troubleshooting](../troubleshooting.md) for unexpected results.

## Options

| Option | Effect |
| --- | --- |
| `--limit NAME=VALUE` | Set a finite operator [resource limit](../engineering/limits.md); repeat for multiple limits |
| `--config PATH` | Select a configuration inside the operation's discovery boundary |
| `--format FORMAT` | Choose `text`, `json`, `vscode`, or `github`; defaults to `text` |

`--limit` accepts positive decimal integer counts; bytes accept decimal integers
in bytes or the exact suffixes `B`, `KiB`, `MiB`, and `GiB`. Durations use explicit
Go duration units, such as `500ms`, `60s`, or `2m`. Values must fit signed 64-bit
integers (durations in nanoseconds). Conflicting duplicates, unknown names,
zero, overflow, and incoherent bounds are usage errors. Equivalent repeats are
accepted. `fix-rounds` cannot exceed 10. The minimum report reserve is 65,536
bytes. Acquisition enforces `document-bytes` (default 1 MiB) and
`total-content-bytes` (default 8 MiB), sharing the operation work and elapsed-time
budget with mandatory decoding. A known oversized file is a usage error (`11`);
runtime growth, aggregate exhaustion, cancellation or deadline leaves work
incomplete (`15`). I/O failures retain precedence (`13`). Broader traversal,
analysis and reporting limit enforcement remains pending.

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

Exit code `0` means analysis completed and the selected files pass the enabled rules. Code `10` means a complete check found violations. Code `15` means analysis is incomplete; review coverage reasons and available findings. Codes `11` through `14` identify usage, configuration, I/O, and internal errors. See the [exit code guide](../exit-codes.md) for details and error precedence.
