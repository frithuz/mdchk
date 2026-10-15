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

| Option | Effect |
| --- | --- |
| `--limit NAME=VALUE` | Set a finite operator [resource limit](../../engineering/limits.md); repeat for multiple limits |
| `--format FORMAT` | Choose `text`, `json`, `vscode`, or `github`; defaults to `text` |

The [global options](../../index.md#global-options) also apply. There is no `--config` override: validation discovers all eligible configuration files in the selected project.

## Understand scan exclusions

The scan respects Git ignore patterns, including those in nested `.gitignore` files. Patterns apply regardless of whether a configuration is tracked by Git. Git metadata and symlinks are skipped. Ignored directories are not traversed, and individually ignored configuration files are not validated.

Within each eligible directory, mdchk validates its `.mdchk.yaml` before using that file's `exclude` settings to filter further scanning. Exclusions are relative to that configuration's directory and apply only beneath it. Valid ancestor exclusions remain in force; a nested configuration cannot reopen an excluded directory or override a Git ignore.

A configuration cannot exclude itself from validation. If a configuration is invalid, mdchk reports it and continues scanning without using its exclusions. Paths already excluded by a valid ancestor are skipped before their configurations are loaded. Reports identify skipped paths and why they were skipped; they do not list undiscovered files inside skipped directories.

A higher-level `allow_nested_excludes: false` makes nonempty exclusion lists in lower configurations invalid. A child's explicit true is valid but ineffective under a denial. Validation reports a prohibited exclusion list and the ancestor imposing the restriction, then continues without applying the invalid file's exclusions. See [configuration permissions](../../configuration.md#restrict-lower-level-exclusions).

Directive restrictions also carry down the scan: ancestor `directives.enabled: false` or per-rule `allow_directives: false` cannot be relaxed by a child. Child `true` values are valid but ineffective under a restriction. Policies above the selected project directory are not read. See [inherited directive restrictions](../../configuration.md#enforce-directive-restrictions-in-descendant-configurations).

## What validation checks

Validation checks YAML syntax and schema restrictions, the required version, canonical numeric rule IDs, value types, rule-specific options (even on disabled rules), exclusion-pattern syntax, and effective requested capabilities. It reports duplicate settings rather than silently choosing one. It does not require exclusion patterns to match existing files, and it does not lint Markdown or apply corrections. If this build lacks the selected dialect, effectively enabled directive processor, or an explicitly configured enabled rule, validation fails. Unmentioned unavailable default rules alone are not validation failures because this command does not execute their checks.

Problems include the configuration path and a line and column when available. VS Code and GitHub formats attach configuration errors to those locations; JSON includes them in the `errors` array with category `configuration`. See [result formats](../../output-formats.md) for integration examples.

```sh
mdchk config validate . --format json
mdchk config validate . --format vscode
mdchk config validate . --format github
```

## Understand the result

Exit code `0` means the scan completed and all discovered configurations are valid. Finding no configuration files also succeeds: the report says none were found, and defaults remain available.

Code `12` means one or more configurations are invalid or unreadable. Scanning continues so you can address multiple errors together. A directory traversal or ignore-file read error is `13`; an internal failure is `14`. During scanning, `14` takes precedence over `13`, which takes precedence over `12`. Invalid command usage or a project argument that is not a directory returns `11`. Markdown violation code `10` is not used. Runtime limits, deadlines or cancellation return `15` if no internal, I/O or configuration error overrides it; precedence is `14 > 13 > 12 > 15 > 0`. The report retains interruption details and actual scan completeness.

See [configuration](../../configuration.md) for valid definitions and [exit codes](../../exit-codes.md) for process-status handling.
