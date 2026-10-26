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

This build supports CommonMark, the full GFM profile and directive processing.
Omitted flavor, including no configuration, selects GFM. Optional contexts and
resource handling remain unavailable. Both supported flavors complete preparation.
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
the original source and validated options. MD047 terminating-newline, MD001 heading-increment, MD003 heading-style, MD018 missing opening-hash spacing, MD019 extra opening-hash spacing, MD020 missing terminal-hash spacing and MD021 closed-heading delimiter spacing
analysis are implemented; other enabled missing defaults produce real coverage deficits and
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
configuration origin remain in the owned shared analysis model. The standalone
[inspect command](inspect.md) exposes those details; lint itself does not publish
an inspection record. MD087 counterfactual checks remain unavailable.
Incomplete directive processing withholds ordinary callbacks and reports reduced
coverage. All analysis shares the same parse, source snapshot and operation budget.

The command behavior below is delivered unless explicitly marked unavailable or deferred.

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
  cd -P "$mdchk_demo" || exit 1
  ./mdchk --help
  ./mdchk lint --format json example.md
  mdchk_status=$?
  printf 'lint status: %s\n' "$mdchk_status"
  test "$mdchk_status" -eq 15
)
```

The compiled binary reports `incomplete` and exits `15`: this configuration admits
the current CommonMark parser, while many enabled ordinary defaults still lack
production implementations. MD047 terminating-newline, MD001 heading-increment, MD003 heading-style, MD018 missing opening-hash spacing, MD019 extra opening-hash spacing, MD020 missing terminal-hash spacing, MD021 closed-heading delimiter spacing, MD022 heading blank-line analysis, MD023 heading indentation analysis, MD024 repeated heading analysis, MD025 multiple-title analysis, MD026 heading-end punctuation analysis, MD036 emphasis-as-heading analysis, MD041 opening-heading analysis, MD043 required-outline analysis, MD063 heading-capitalization analysis, MD080 heading-anchor collision analysis and MD082 empty-section analysis are available. MD026, MD036, MD043, MD063, MD080 and MD082 are available but disabled by default. The catalog contains 87 entries, with 59 enabled
and 28 disabled by default; the mandatory UTF-8 gate is available independently
of ordinary checks. Explicitly requesting an unavailable rule returns `12`.
Omitting configuration selects GFM and also retains missing default-rule coverage (`15`).

For a runnable reduced selection, use the published
[CommonMark+MD047 configuration](../examples/editor-ci/.mdchk.yaml) and
[exact source fixture](../examples/editor-ci/guide.md), as described in the
[quick start](../index.md#get-started-with-this-build). The original returns `10`;
adding one newline returns `0`. Preview returns `0` with a proposed change while
the original bytes stay unchanged. To obtain a clean `0` for other valid UTF-8,
disable unavailable ordinary rules and satisfy or disable the available checks. To isolate the decoding gate, disable every ordinary rule. The
compiled production fixture in
[`cleanConfiguration`](../../cmd/mdchk/configuration_acceptance_test.go)
generates that complete configuration from the actual catalog and verifies text/JSON
reporters; use the [contributor workflow](../contributing.md#exercise-a-newly-registered-rule-through-the-cli)
to run it or add a registered-rule fixture. It also describes the separate
compile-time synthetic registration used to demonstrate violations (`10`) and
validated options. A clean fixture with ordinary checks disabled establishes only
the requested reduced coverage, rather than full Markdown rule conformance.

Capture `$?` immediately after the executable, as above. `go run` reports a
wrapper status for nonzero program exits and cannot verify mdchk's semantic
status. The executable implements lint, fix, config validate, inspect, help and
version.

The selection and option examples below assume the
[current reduced setup](../index.md#get-started-with-this-build) and readable
fixture paths. The disposable example above intentionally demonstrates incomplete
CommonMark defaults instead.

## Select files

Pass one or more files or directories. Directories are searched recursively for `.md` and `.markdown` files. With no paths, lint checks the working directory.

```sh
mdchk lint
mdchk lint README.md docs/
```

Configuration exclusions apply even to explicitly named files. Git metadata and symlinks are skipped. Missing or unreadable paths are errors; selecting no files is also an error. The report identifies skipped files and any checks that could not complete.

Git ignore support for lint and fix is deferred; use configuration exclusions for paths you want skipped. See [file selection](../file-selection.md) for path resolution, differences between commands, and the defined pattern grammar and case handling, or [troubleshooting](../troubleshooting.md) for unexpected results.

## Options

Options may appear before or after path arguments; `--` ends option parsing.
Valid help/version requests bypass target requirements, while invalid option
syntax or scope still returns usage `11`. See the linked global options policy.

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
