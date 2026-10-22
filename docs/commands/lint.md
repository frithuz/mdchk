# mdchk lint

Check Markdown files against the enabled rules and report problems without modifying files.

```text
mdchk lint [options] [paths...]
```

## Current implementation

Build the executable with `go build -o mdchk ./cmd/mdchk`. Help, version, argument
parsing, configuration preparation, source acquisition, mandatory UTF-8
validation, parsing and registered-rule execution are available. Lint resolves its single
configuration from the working directory or `--config`, validates all settings
and ancestor permissions, and checks selected capabilities before target access.
Invalid or unreadable configuration returns `12`. Help and version need no
project access.

This build supports CommonMark configuration and directive processing. GFM,
optional contexts and resource handling remain unavailable. Built-in defaults
still select GFM and therefore return `12` when no configuration exists. A
configuration with `flavor: commonmark` can complete preparation.
Explicitly enabled unavailable rules fail preparation; unmentioned default rules
remain in the complete catalog for coverage reporting. Preparation retains an
immutable admitted execution plan for the shared engine.

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

The admitted plan runs the internal parser and available registered checks with
the original source and validated options. MD047 terminating-newline analysis is
implemented; other enabled missing defaults produce real coverage deficits and
return `15`. Disabling all ordinary rules permits a clean `0` for valid UTF-8.
Available registered checks can return violations (`10`); interruptions retain
partial findings and known targets, with runtime precedence `14 > 13 > 15 > 10 > 0`.
Text reports are the default; `--format json` emits one undecorated schema-v1
result. Text/JSON results, including usage and configuration errors, go to stdout; stderr
is reserved for execution logs. Reports retain available findings, errors, skip
and coverage reasons, partial file outcomes, interruptions and omissions.
VS Code and GitHub reporters support the same canonical results and exits.
Reporter choice does not reduce analysis coverage. Their diagnostic and
operational streams follow the [result formats contract](../output-formats.md).

Directive state is resolved before scheduling checks. A configured-off rule can
be enabled by a permitted comment with its original options. Checks receive the
full original document; each valid finding is suppressed according to its start
anchor. File-wide findings use byte zero, while missing EOF material uses the
zero-width EOF anchor. Surplus terminal material uses its smallest offending
suffix after retaining the first ending. Missing implementations enabled in any applicable region,
including EOF, produce coverage rather than fabricated findings. Malformed
commands produce non-disableable `directive` violations; blocked requests produce
located notes identifying the governing permission fields. Global denial retains
inactive candidates without interpreting syntax as violations.

JSON retains suppressed findings separately with `suppressed_by.kind`, without
`directive_id`: lint does not publish a directive-record collection. Text prints
suppression and policy notes. Exact owner/token/raw candidate and every governing
configuration origin remain in the owned shared analysis model for later
inspection. Lint does not implement an inspect command or MD087 counterfactuals.
Incomplete directive processing withholds ordinary callbacks and reports reduced
coverage. All analysis shares the same parse, source snapshot and operation budget.

The remaining execution behavior on this page is the accepted target contract.

## Run a disposable project

From the repository root with Go 1.27.2 or newer and dependencies retrieved
(`go mod download` on the first build), use this POSIX shell example:

```sh
mdchk_demo=$(mktemp -d)
go build -o "$mdchk_demo/mdchk" ./cmd/mdchk
cat > "$mdchk_demo/.mdchk.yaml" <<'YAML'
version: 1
flavor: commonmark
YAML
printf '# Example\n' > "$mdchk_demo/example.md"
(
  cd "$mdchk_demo" || exit 1
  ./mdchk --help
  ./mdchk lint --format json example.md
  mdchk_status=$?
  printf 'lint status: %s\n' "$mdchk_status"
  test "$mdchk_status" -eq 15
)
```

The compiled binary reports `incomplete` and exits `15`: this configuration admits
the current CommonMark parser, while many enabled ordinary defaults still lack
production implementations. MD047 terminating-newline analysis is available. The catalog contains 89 entries, with 59 enabled
and 30 disabled by default; the mandatory UTF-8 gate is available independently
of ordinary checks. Explicitly requesting an unavailable rule returns `12`.
Omitting configuration selects unavailable default GFM and also returns `12`.

To obtain a clean `0` for valid UTF-8, disable unavailable ordinary rules and
satisfy or disable MD047. To isolate the decoding gate, disable every ordinary rule. The
compiled production fixture in
[`cleanConfiguration`](../../cmd/mdchk/configuration_acceptance_test.go)
generates that complete configuration from the actual catalog and verifies both
reporters; use the [contributor workflow](../contributing.md#exercise-a-newly-registered-rule-through-the-cli)
to run it or add a registered-rule fixture. It also describes the separate
compile-time synthetic registration used to demonstrate violations (`10`) and
validated options. A clean fixture with ordinary checks disabled establishes only
the requested reduced coverage, rather than full Markdown rule conformance.

Capture `$?` immediately after the executable, as above. `go run` reports a
wrapper status for nonzero program exits and cannot verify mdchk's semantic
status. The executable implements lint, fix, config validate, inspect, help and
version. GFM and Pandoc remain deferred.

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
| `--format FORMAT` | Choose `text` (default), `json`, `vscode` or `github` |

`--limit` accepts positive decimal integer counts; bytes accept decimal integers
in bytes or the exact suffixes `B`, `KiB`, `MiB`, and `GiB`. Durations use explicit
Go duration units, such as `500ms`, `60s`, or `2m`. Values must fit signed 64-bit
integers (durations in nanoseconds). Conflicting duplicates, unknown names,
zero, overflow, and incoherent bounds are usage errors. Equivalent repeats are
accepted. `fix-rounds` cannot exceed 10. The minimum report reserve is 65,536
bytes. Acquisition enforces `document-bytes` (default 1 MiB) and
`total-content-bytes` (default 8 MiB), sharing the operation work and elapsed-time
budget with mandatory decoding, parsing and registered checks. A known oversized file is a usage error (`11`);
runtime growth, aggregate exhaustion, cancellation or deadline leaves work
incomplete (`15`). I/O failures retain precedence (`13`). Reporting enforces the encoded
`response-bytes` bound (default 8 MiB), including JSON escaping and the final
newline, and its independent `reporting-time` budget (default 2 seconds).
Whole-record omissions are counted and force incomplete results. Error details
may become identity-preserving summaries with exact omission counts. Before
analysis, the reserve plus mandatory target manifest must fit the response
budget. CLI path arguments request selection rather than defining known
target records; directories and exclusions can yield zero targets. Preproject
feasibility checks only the known empty envelope. After discovery, an infeasible
actual target manifest stops before acquisition and analysis as incomplete `15`,
with exact file and coverage omission counts. Genuinely known request/envelope
infeasibility remains preflight usage `11`.
Encountered errors retain precedence. The final report can use the whole
response budget. Output delivery failures return `13` unless an established
internal error requires `14`.
Broader traversal and application limit enforcement remain pending.

The [global options](../index.md#global-options) also apply. See [configuration](../configuration.md) for discovery and settings, and [result formats](../output-formats.md) for output examples.

## Examples

```sh
mdchk lint docs/ --config .mdchk.yaml
mdchk lint . --format json
mdchk lint -- --draft.md
```

## Understand the result

Each finding identifies its location, rule, explanation, and whether a safe correction is available. Use [fix](fix.md) to preview or apply those corrections. Lint remains read-only regardless of configuration.

Exit code `0` means analysis completed and the selected files pass the enabled rules. Code `10` means a complete check found violations. Code `15` means analysis is incomplete; review coverage reasons and available findings. Codes `11` through `14` identify usage, configuration, I/O, and internal errors. See the [exit code guide](../exit-codes.md) for details and error precedence.
