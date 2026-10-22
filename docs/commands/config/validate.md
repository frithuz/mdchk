# mdchk config validate

Find and validate every `.mdchk.yaml` in the selected project, skipping Git-ignored paths and paths excluded by applicable mdchk configuration. Validation is read-only: it changes neither configuration nor Markdown files.

```text
mdchk config validate [options] [project]
```

## Select a project

Pass a project directory, or omit it to scan the working directory. The scan stays beneath that directory and includes configuration files in nested directories.

```sh
mdchk config validate
mdchk config validate /path/to/project
```

Each configuration is validated independently. This checks nested definitions without changing how lint and fix select their single configuration file.

## Options

Options may appear before or after path arguments; `--` ends option parsing.
Valid help/version requests bypass target requirements, while invalid option
syntax or scope still returns usage `11`. See the linked global options policy.

| Option | Effect |
| --- | --- |
| `--limit NAME=VALUE` | Set a finite operator [resource limit](../../engineering/limits.md); repeat for multiple limits |
| `--format FORMAT` | Choose `text` (default), `json`, `vscode` or `github` |

The [global options](../../index.md#global-options) also apply. There is no `--config` override: validation discovers all eligible configuration files in the selected project.

## Understand scan exclusions

The scan respects Git ignore patterns, including those in nested `.gitignore` files. Patterns apply regardless of whether a configuration is tracked by Git. Git metadata and symlinks are skipped. Ignored directories are not traversed, and individually ignored configuration files are not validated.

Within each eligible directory, mdchk validates its `.mdchk.yaml` before using that file's `exclude` settings to filter further scanning. Exclusions are relative to that configuration's directory and apply only beneath it. Valid ancestor exclusions remain in force; a nested configuration cannot reopen an excluded directory or override a Git ignore.

A configuration cannot exclude itself from validation. If a configuration is invalid, mdchk reports it and continues scanning without using its exclusions. Paths already excluded by a valid ancestor are skipped before their configurations are loaded. Reports identify skipped paths and why they were skipped; they do not list undiscovered files inside skipped directories.

A higher-level `allow_nested_excludes: false` makes nonempty exclusion lists in lower configurations invalid. A child's explicit true is valid but ineffective under a denial. Validation reports a prohibited exclusion list and the ancestor imposing the restriction, then continues without applying the invalid file's exclusions. See [configuration permissions](../../configuration.md#restrict-lower-level-exclusions).

Directive restrictions also carry down the scan: ancestor `directives.enabled: false` or per-rule `allow_directives: false` cannot be relaxed by a child. Child `true` values are valid but ineffective under a restriction. Policies above the selected project directory are not read. See [inherited directive restrictions](../../configuration.md#enforce-directive-restrictions-in-descendant-configurations).

## What validation checks

Validation checks YAML syntax and schema restrictions, the required version, canonical numeric rule IDs, value types, rule-specific options (even on disabled rules), exclusion-pattern syntax, and effective requested capabilities. It reports duplicate settings rather than silently choosing one. It does not require exclusion patterns to match existing files, and it does not lint Markdown or apply corrections. If this build lacks the selected dialect, effectively enabled directive processor, or an explicitly configured enabled rule, validation fails. Unmentioned unavailable default rules alone are not validation failures because this command does not execute their checks.

Problems include the configuration path and a line and column when available.
VS Code and GitHub report located definition errors; unlocated errors use
VS Code stderr or GitHub annotations. GitHub uses invocation CWD as the declared
checkout root, not the selected scan directory. JSON includes problems in the
`errors` array with category `configuration`. See
[result formats](../../output-formats.md) for integration examples.

```sh
mdchk config validate . --format json
```

## Understand the result

Exit code `0` means the scan completed and all discovered configurations are valid. Finding no configuration files also succeeds: the report says none were found, and defaults remain available.

Code `12` means one or more configurations are invalid or unreadable. Scanning continues so you can address multiple errors together. A directory traversal or ignore-file read error is `13`; an internal failure is `14`. During scanning, `14` takes precedence over `13`, which takes precedence over `12`. Invalid command usage or a project argument that is not a directory returns `11`. Markdown violation code `10` is not used. Runtime limits, deadlines or cancellation return `15` if no internal, I/O or configuration error overrides it; precedence is `14 > 13 > 12 > 15 > 0`. The report retains interruption details and actual scan completeness.

See [configuration](../../configuration.md) for valid definitions and [exit codes](../../exit-codes.md) for process-status handling.

## Current implementation

The command implements bounded discovery, independent validation, inherited
policies, scoped exclusions and all four report formats. It does not execute Markdown
rules. The compiled workflow acceptance suite verifies actual text/JSON process
statuses, independent strict validation, source locations and hashes, scoped
policy failures, Git negation and tracked files, skipped roots, symlinks, empty
scans, runtime quotas, tiny reporting deadlines, and no source writes. Permission
denial cases probe the OS first and skip only when the current user or platform
can still access mode-000 paths. Git constructs the tracked fixture during tests;
the command itself uses the internal matcher and does not invoke Git.

The shared `config_validation` summary reports `scan_complete`, completed
`definitions_checked`, `valid_definitions`, `invalid_definitions`, and known
`skipped_definitions`. Valid plus invalid equals the completed decision count.
`checked_files` instead lists acquired sources that entered validation, including
an interrupted attempt whose file analysis is `partial`. An interrupted attempt
has no valid/invalid decision. Unreadable definitions have failed file outcomes
without fabricated source snapshots. Readable invalid definitions have completed
analysis; a completely enumerated invalid scan can have `complete: true` and
`status: error`. Reporting omissions can make envelope completeness false while
preserving actual scan completeness and counts. Skipped directories describe
roots, never a count or listing of their unenumerated definitions.

Applicable overrides are `config-bytes`, `total-config-bytes`,
`directory-entries`, `path-bytes`, `path-depth`, `structured-input-depth`,
`syntax-depth` (alias traversal), `document-nodes`, `total-nodes`, `work-units`,
`operation-time`, `response-bytes`, `reporting-time`, and `report-reserve-bytes`.
Other limit names are rejected as inapplicable. Configuration bytes are independent
of Markdown document bytes. Structured depth can be raised or lowered through
4096; scan depth through 4096, path bytes through 1 MiB, aggregate configuration
bytes through 1 GiB and directory entries through 2^30. Unsupported or incoherent
profiles are usage errors before access. Only enforced applicable limits appear
in the report. Git source discovery retains its explicit unsupported-source and
nonsymlink-access errors rather than silently claiming complete ignore handling.

If completed configuration outcomes exceed the response budget, the read-only
report can omit whole `files` and `checked_files` records with exact known counts.
If located error identities alone cannot fit, it retains one explicit aggregate
summary per encountered category and records how many detailed errors were
omitted. Validation counts and actual scan completeness remain authoritative;
these reporting omissions make the envelope incomplete and preserve error
precedence. No contents of pruned directories are counted. Lint's admitted
outcome manifest is unchanged by this configuration-specific reduction.

Reporting reserves a small emergency result before starting the requested
`reporting-time` interval for full-fallback and detailed report construction.
This fixed-work reserve uses precomputed error-category counts and collection
lengths; it does not copy or walk source identities or error prose. Its record
count is bounded independently of the number of definitions. If the interval
expires during either full-fallback construction or optional detail work, the
already encoded reserve preserves actual scan/validity counts, encountered error
categories and exact known omissions. Positive finite reporting deadlines,
including very small values, remain supported; the fixed reserve construction
and output delivery are outside that timed detail interval. Output delivery
failure remains IO/13 unless internal/14 already applies. This is a finite
application-work bound, not a hard process scheduling or OS latency guarantee.
