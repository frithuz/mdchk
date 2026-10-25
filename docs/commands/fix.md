# mdchk fix

Current implementation: `fix` applies validated MD047 corrections on Darwin
arm64 with cgo and local APFS. Other native adapters refuse writes with I/O
status 13. `fix --dry-run` supports all four formats and writes nothing. Text/JSON
include proposed diffs; editor formats report only remaining candidate findings.
MD047 supplies the first production correction: minimal terminal-newline changes
with independently checked preservation and shared directive permissions. Unsafe
protected EOF contexts remain manual, with explicit deferral reasons. In GFM,
adding an ending to literal task spelling such as `- [x]` at EOF would turn
visible text into task syntax. MD047 leaves that case manual with false
`fix_available`; choose the intended text or task meaning before editing.


Apply safe corrections to Markdown files and report problems that still need attention. Preview changes with `--dry-run` before saving them.

```text
mdchk fix [options] [paths...]
```

The examples below assume the [current reduced setup](../index.md#get-started-with-this-build):
a CommonMark+MD047 configuration and readable fixture paths. Without it,
default GFM and CommonMark both retain missing default-rule coverage (`15`). Apply additionally requires the verified native adapter above.

## Select files

Pass one or more files or directories. Directories are searched recursively for `.md` and `.markdown` files. With no paths, fix processes the working directory. It uses the same selection and exclusion behavior as [lint](lint.md).

```sh
mdchk fix --dry-run README.md docs/
```

## Options

Options may appear before or after path arguments; `--` ends option parsing.
Valid help/version requests bypass target requirements, while invalid option
syntax or scope still returns usage `11`. See the linked global options policy.

| Option | Effect |
| --- | --- |
| `--limit NAME=VALUE` | Set a finite operator [resource limit](../engineering/limits.md); repeat for multiple limits |
| `--config PATH` | Select a configuration inside the operation's discovery boundary |
| `--format FORMAT` | Choose `text` (default), `json`, `vscode` or `github` |
| `--dry-run` | Compute corrections and report the predicted result without writing files |

The [global options](../index.md#global-options) also apply. See [configuration](../configuration.md) and [result formats](../output-formats.md) for details.

## Preview and apply corrections

```sh
mdchk fix docs/ --dry-run
mdchk fix docs/
mdchk fix docs/ --dry-run --format json
```

Text and JSON previews include proposed diffs when a registered correction
passes shared candidate validation. MD047 final-newline correction is implemented. MD001, MD003, MD018, MD019, MD020 and MD021 remain report-only, with no edits or previews for their findings.
Other corrections remain planned. VS Code and GitHub describe findings remaining
in proposed content for dry runs, without diffs or raw corrected Markdown. Use
[lint](lint.md) when highlighted locations must match files currently on disk.

Corrections preserve meaning and unrelated content. For example, fix can add a missing final newline while preserving intentional hard breaks. It leaves code examples, front matter, and ambiguous changes alone. A skipped heading level or missing code language needs a decision rather than a guessed correction.

After corrections, mdchk checks the resulting content and reports remaining findings at their new locations. Apply reports observed saved snapshots and confirmed written changes; dry-run reports candidate snapshots and proposed changes. Refused or unavailable saved observations are never replaced by predicted clean content.

Production cross-file rule/resource consumers remain planned. Their accepted
post-fix contract uses one candidate view containing all proposed target changes,
with referenced resources read-only. Current CLI publication conservatively
defers coupled target changes rather than claiming such checks ran. After project writes, results use the actual saved view: successful writes plus confirmed unchanged snapshots. If writes fail or a dependency changes, mdchk reports the error and affected incomplete coverage instead of treating every proposal as saved. See the [resource snapshot contract](../engineering/resources.md#snapshots-caching-fixes-and-limits).

## Understand the result

Exit code `0` means analysis completed and no violations remain after corrections. Code `10` means a complete check found remaining problems, including those that cannot be safely fixed. Code `15` means post-fix analysis is incomplete; available safe changes and findings remain in the report, and uncertain regions remain protected. For a dry run, those codes describe the predicted content; even a `0` can accompany changes that still need to be applied.

Errors can occur after other files have changed. Inspect both successful changes and failures before retrying. If a file changes during fixing, mdchk refuses stale edits rather than overwriting the newer contents. See the [exit code guide](../exit-codes.md) for usage, configuration, I/O, and internal error codes.
