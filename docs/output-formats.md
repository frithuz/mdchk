# mdchk result formats

Choose a result format with `--format`. Lint currently implements readable text
(the default) and JSON for automation. Results go to stdout; stderr is reserved
for execution logs. VS Code and GitHub reporters are planned and currently return
unsupported usage before project access. Fix, inspect, config validation and MCP
remain planned; their format behavior below describes the accepted contract.

| Format | Use |
| --- | --- |
| `text` | Read findings, corrections, and a summary in the terminal |
| `json` | Parse complete results in a script or another application |
| `vscode` | Send file locations and messages to a VS Code problem matcher |
| `github` | Annotate files in GitHub Actions workflow results |

Formats change presentation, not the rules, fixes, or [exit codes](exit-codes.md).
The complete contract gives `lint`, `fix`, and
[`config validate`](commands/config/validate.md) all four formats; this build
implements only lint text and JSON.

[`inspect`](commands/inspect.md) accepts `text` and `json` for its detailed processing report. VS Code and GitHub annotations do not carry its AST and other inspection data.

## Read results in the terminal

```sh
mdchk lint docs/
mdchk fix docs/ --format text
```

Each finding includes a file, line, column, rule, explanation, and whether it can be fixed automatically. For example:

```text
docs/guide.md:18:1 MD001: Heading jumps from level 2 to level 4. Choose the intended heading level. [manual]
Checked 3 files. Found 1 violation.
```

Fix reports show corrections and remaining findings. Dry runs show proposed diffs. Summaries identify skipped files and errors as well as violations. Lint text
also identifies coverage reasons, interruptions and omissions. CR and LF in
paths, explanations and other line-oriented fields are escaped so user data
cannot forge another output record; JSON retains the original string values.

## Parse results as JSON

```sh
mdchk lint docs/ --format json > result.json
```

JSON output is one object, without terminal decorations. The normative
[schema](contracts/results/result.schema.json) defines every field. See the
[complete lint example](contracts/results/violations-lint.json),
[preview example](contracts/results/fix-preview.json),
[partial-write example](contracts/results/partial-project-writes.json), and
[inspection example](contracts/results/inspection-invalid-utf8.json).

Every result contains `schema_version: 1`, `operation`, `mode`, `status`, `complete`,
and arrays `checked_files`, `skipped_files`, `files`, `diagnostics`, `coverage`,
`changes`, `deferred_fixes`, `errors`, `notes`, `interruptions`, `omissions`,
`documents`, `suppressed_diagnostics`, and `effective_limits`, even when empty.
`dry_run` is required only for fix. `checked_files` always contains path strings;
`files` supplies authoritative analysis, write and content-return outcomes.
Located records identify their exact original, candidate or saved snapshot by
SHA-256 and byte count. See the [semantic contract](engineering/results.md).

For lint/fix checks, `status` is `clean`, `violations`, `incomplete`, or `error`. An operational error selects `error`; otherwise coverage deficits, interruptions or omissions select `incomplete`, then remaining findings select `violations`, and a complete check without findings selects `clean`. Error entries retain their categories: `usage`, `configuration`, `io`, or `internal`. Check `complete`, `coverage`, and `errors` before treating absent findings as a clean result. File failures or reduced analysis coverage make the result incomplete.

Inspection JSON uses operation `inspect` and status `inspected` when a report is produced. Its `inspection` object contains resolved path, binary version, `size_display` and integer `size_bytes`, full-file checksums, flavor, AST, rules, active and inactive counts, directives, parsing notes, selected lines, and data availability. Reported findings remain in `diagnostics`; suppressed observations appear in `suppressed_diagnostics`. Unresolved processing data is marked unavailable, rather than represented as a successful empty check. An inspection can return success with `complete: false` and configuration errors in the report because it reveals processing details rather than validating them.

Positions use one-based lines and Unicode code-point columns; tabs count as one column. Range ends are exclusive. Paths are relative to the command's working directory. Fix diagnostics describe the saved content; dry-run diagnostics describe the proposed content. Changes distinguish written corrections from proposed corrections and include diffs for previews.

Check `schema_version` before consuming fields. Ordinary command and configuration errors also produce a JSON result when `--format json` is valid; a crash or output-delivery failure can prevent a complete JSON object from being written. Always capture the process status as well.

## Incomplete analysis

Lint, fix, and previews return status `incomplete`, `complete: false`, and exit `15` when requested analysis is incomplete and no operational error overrides it. Findings and safe changes remain available. An unavailable default-enabled rule is a coverage deficit even if it would have found no violations. Explicitly requested unavailable capabilities instead fail configuration validation before analysis. Disabled or dialect-inapplicable rules are not coverage deficits.

The `coverage` array is present even when empty. Each entry has required string fields `reason` and `message`, with required `id` and optional `rule` (MDxxx), `path`, `range`, `snapshot`, and `cause_ids` when attributable. Ranges use the same coordinates as findings. A build-wide unavailable rule needs no fabricated document location. Entries are deterministic: order by path (absent first), position (absent first), rule, then reason; deduplicate identical records.

| Reason | Meaning |
| --- | --- |
| `rule-unavailable` | An applicable enabled rule has no executable implementation, including one enabled by an unmentioned default or a permitted directive |
| `parser-uncertain` | Unsupported or ambiguous parsing prevents reliable analysis of a region |
| `context-unavailable` | An enabled check lacks required resource or integration context |

Additional coverage reasons are `decoding-failed`, `saved-view-unavailable`,
`fix-nonconvergent`, `cancelled`, `deadline-exceeded`, and `limit-exceeded`.
Runtime interruption also has an `interruptions` record so config validation can
report stopped scanning while keeping Markdown `coverage` empty. Completed
inspect can retain `inspected`/exit 0 for observations; interrupted inspect uses
`incomplete`/exit 15 unless an operational error takes precedence.

Coverage deficits are distinct from Markdown violations and operational errors. A read or decoding failure remains an operational error and may coexist with coverage records. They cannot be suppressed by inline directives; actual rule disablement or inapplicability determines whether a check is required.

Text reports identify incomplete coverage and its reasons. VS Code emits located coverage records with identifier `coverage` and sends location-free records to stderr. GitHub emits coverage error annotations, omitting source coordinates when none exist. All retain the nonzero exit so incomplete work cannot appear as a clean CI check. Inspect retains status `inspected` and successful exit when its report is produced; config validation does not run Markdown checks.

Resource coverage belongs to the source document and affected check. Unknown partial-map entries, blocked paths, and missing required content use `context-unavailable`; established target absence belongs to MD057 or the applicable navigation rule. MD051 does not duplicate a missing-file finding. Actual read/decode failures also produce an attributed `io` error. Shared resource failures are deduplicated without losing affected source coverage. See [resource outcomes](engineering/resources.md#outcomes-and-rule-ownership).

Cross-file preview and standalone fix findings use the coordinated candidate view. Project fix findings use the actual saved view after writes, with stale or uncertain dependencies explicitly invalidated. Resource-only content is never returned as corrected target content.

## Highlight findings in VS Code (planned)

```sh
mdchk lint . --format vscode
```

The VS Code format emits one plain line per finding:

```text
docs/guide.md:18:1: error: MD001: Heading jumps from level 2 to level 4. Choose the intended heading level.
```

Lines use `path:line:column: severity: rule: message`. There are no colors, headings, summaries, or diffs on stdout. A clean check emits no diagnostic lines. Execution errors appear on stderr; capture the exit code so a failed check is not mistaken for a clean one.

Configure a VS Code task to parse that pattern and run the command from the project's root. VS Code [problem matchers](https://code.visualstudio.com/docs/debugtest/tasks#_defining-a-problem-matcher) can display matched findings inline and in the Problems panel. For example, add this task to `.vscode/tasks.json`:

```json
{
  "version": "2.0.0",
  "tasks": [
    {
      "label": "Check Markdown",
      "type": "process",
      "command": "mdchk",
      "args": ["lint", ".", "--format", "vscode"],
      "options": {"cwd": "${workspaceFolder}"},
      "problemMatcher": {
        "owner": "mdchk",
        "fileLocation": ["relative", "${workspaceFolder}"],
        "pattern": {
          "regexp": "^(.*):(\\d+):(\\d+): (error): (MD[0-9]{3}|[a-z][a-z0-9-]*): (.*)$",
          "file": 1,
          "line": 2,
          "column": 3,
          "severity": 4,
          "code": 5,
          "message": 6
        }
      }
    }
  ]
}
```

This provides findings when the task runs. Use JSON for an integration that needs exact ranges or its own conversion to editor-specific character coordinates.

## Annotate GitHub Actions results (planned)

```sh
mdchk lint . --format github
```

Run this command after checkout and installation of mdchk, with the repository root as the working directory. In a workflow step:

```yaml
- name: Check Markdown
  run: mdchk lint . --format github
```

The format emits GitHub Actions [error annotations](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-commands#setting-an-error-message) with repository-relative file paths, line numbers, and rule titles:

```text
::error file=docs/guide.md,line=18,title=MD001::Heading jumps from level 2 to level 4. Choose the intended heading level.
```

GitHub associates annotations with files in the workflow results. Findings use line locations so GitHub can highlight the affected lines. Execution errors also produce error annotations, without an invented source location. mdchk escapes annotation values so file names and messages are treated as data.

The exit code controls whether the workflow step succeeds; annotations provide locations and explanations. A clean check emits no error annotations. After `fix`, annotations describe remaining problems; use `lint` to annotate committed content without changing it. GitHub controls how many annotations are displayed, so retain JSON results if you need a complete machine-readable report.

## Output streams and previews

For `config validate`, JSON uses operation `config validate` and reports definition errors in `errors` with category `configuration`; `diagnostics` and `coverage` remain empty because it does not execute Markdown checks. Located configuration errors also appear as VS Code problem-matcher lines and GitHub annotations, using identifiers such as `config-definition`. Errors without source locations remain stderr messages in VS Code output. Text includes validation counts, including successful scans with no configuration files.

Results go to stdout and execution logs go to stderr. Text also displays errors in its report; JSON includes them in `errors`; GitHub emits them as annotations. VS Code format keeps operational errors without source locations on stderr.

For `fix --dry-run`, text and JSON include proposed changes. VS Code and GitHub formats report only findings remaining in the proposed content, so locations may differ from the files on disk. Use `lint` for editor and CI locations that match current files, and text or JSON to review a fix preview.

MCP calls return structured tool results rather than selecting a terminal format. They share the diagnostic and error meanings described here; see the [MCP guide](commands/mcp.md).

## Bounded reporting

Lint enforces the default encoded response budget of 8 MiB, including escaping
and the final newline, and an independent reporting deadline of 2 seconds.
Use `--limit response-bytes=4MiB` or `--limit reporting-time=3s` to change
these finite operator budgets. Whole records are retained deterministically.
Detailed errors can become summaries with exact `/errors` omission counts,
preserving categories, IDs, paths, snapshots and ranges; encountered errors
and file outcomes remain authoritative. Before analysis, the report reserve
plus mandatory target manifest must fit. Genuinely known request/envelope
infeasibility is preflight usage `11`. CLI paths request selection and can name
directories or excluded entries, so preproject feasibility checks only the
known empty envelope. After discovery, an infeasible actual target manifest
stops before acquisition/analysis as incomplete `15`, with exact `/files` and
`/coverage` omissions. The final report can use the full response budget.
Reporting pre-encodes an essential partial fallback for its independent deadline.
A failed output write,
including a short write, returns `13` unless an established internal error
requires `14`; a failed delivery can leave a partial report.

The full contract for other operations is: Omit whole optional diff/AST sections
first, then whole ordered records with exact omitted counts when known. Unknown
counts are omitted, never fabricated as zero. Omission makes `complete: false`.
Reserve every target write outcome before commits; no size reduction can discard
actual write results. Standalone fix can withhold a whole file content record with
an explicit `return_status: withheld`; it never truncates a content string.

`effective_limits` reports applicable operator/default values. MCP's structured
result and serialized JSON text fallback both count, including escaping. See
[resource limits](engineering/limits.md) and the
[withheld-content example](contracts/results/withheld-whole-content.json).
