# Troubleshooting mdchk

Start with the command's report and [exit code](exit-codes.md). Lint and fix use `10` for remaining rule violations, `11` through `14` for execution errors, and `15` for incomplete analysis. Inspection can succeed while reporting unresolved configuration or incomplete analysis, so its exit code alone is not a validation result.

## No files were selected

Lint and fix return `11` when selection is empty. Check the working directory, target paths, `.md` and `.markdown` extensions, and skip reasons. Configuration exclusions apply even to explicitly named files; symlinks and Git metadata are skipped.

Try a known file to narrow the problem:

```sh
mdchk lint README.md --format json
```

Review `checked_files`, `skipped_files`, and `errors`. Correct the target or the applicable exclusion rather than treating the empty selection as a clean check. See [file selection](file-selection.md).

## The wrong configuration seems to be in use

Lint and fix discover configuration from the working directory. Inspect discovers it from the resolved target's directory. Nested definitions do not override lint and fix settings, and validation of a nested definition does not make it active for a Markdown check.

Compare both operations with an explicit configuration:

```sh
mdchk lint docs/guide.md --config .mdchk.yaml
mdchk inspect docs/guide.md --config .mdchk.yaml
```

Review effective flavor, rule options, and configuration problems in inspection. Check the selected file's settings and any higher-level `allow_nested_excludes` restriction. Project MCP discovery stays within its startup root; standalone MCP uses only the configuration submitted with the request.

## Configuration cannot be loaded or validated

Lint and fix return `12` and stop before writing corrections. Check the configuration boundary and read permissions, integer `version: 1`, canonical MDxxx IDs, option types and values, duplicate settings, and higher-level exclusion restrictions. Explicit configs outside the operation's boundary, in another repository scope, or reached through a symlink are rejected. Also check whether the selected dialect, directive processing, or explicitly enabled rule is available in this build.

```sh
mdchk config validate . --format text
```

Validation reports all eligible definitions, not just the one selected by lint. Git ignores and valid ancestor exclusions can hide definitions from this scan. If the selected file was not scanned, review the skip report and run lint with an explicit `--config` to check its loading error. See [configuration](configuration.md).

## An ignored or excluded path is still being checked

A Git ignore entry affects `config validate`; Git ignore support for lint and fix is deferred. For Markdown checks, add the path to the selected configuration's `exclude` list. Resolve that pattern relative to the configuration's directory.

Explicitly naming a file does not bypass its exclusion, but `inspect` intentionally allows excluded targets. If a nested configuration contains your exclusion, remember that lint and fix do not use nested overrides. See [file selection and exclusions](file-selection.md).

## An inline directive has no effect

Inspect the affected lines, using bounds within the file:

```sh
mdchk inspect docs/guide.md --lines 20-40 --config .mdchk.yaml
```

Check whether the comment is recognized, names valid rules, and is allowed by `directives.enabled` and the rule's `allow_directives`. Check ancestor configurations too: their `false` permission values remain binding even if the selected file sets `true`. Inspection identifies the ancestor and setting responsible for a restriction. Comments inside code or front matter are inactive. Changes begin after the closing `-->`, affect only named rules, and continue until another allowed directive changes them or the file ends.

Malformed directives change no rule state. An enable directive explicitly enables a rule rather than restoring its configured default. Inspection includes earlier directives still affecting the selected region and identifies blocked requests. See [directives](directives.md).

## Fix leaves findings or a preview exits successfully

Fix applies only safe corrections. A heading jump or missing fenced-code language requires a manual decision; remaining findings return `10`.

```sh
mdchk fix docs/guide.md --dry-run --format text
```

Review the diff and remaining findings, then apply the safe corrections and make the manual edits. A dry run's `0` describes the predicted content; it does not mean the proposed changes were saved. Text and JSON include preview diffs. VS Code and GitHub formats show only remaining findings in the proposed content.

## Locations differ from the file on disk

Fix findings refer to saved content; dry-run findings refer to proposed content. Corrections can move later locations. For editor or CI findings that match the current file, run lint:

```sh
mdchk lint docs/guide.md --format vscode
```

Positions use one-based lines and Unicode code-point columns, with exclusive range ends. An integration using other character coordinates must convert them. See [result formats](output-formats.md).

## A stale-write or other file error occurred

A file changed after fix read it, or an access, encoding, or write failure prevented completion. File errors return `13`; other files may already have changed.

Review both successful changes and errors before retrying. For a stale edit, review the current file and coordinate with the process editing it, then generate a new preview. For other failures, correct the reported path, permissions, or encoding problem. A failed run does not roll back changes to other files. See [fix result handling](commands/fix.md#understand-the-result).

## No findings were reported, but processing is incomplete

Absent findings do not prove that every required check ran. Unavailable default-enabled rules, missing context, and uncertain syntax yield status `incomplete` and exit `15`; an overriding operational error retains its own status and exit code.

```sh
mdchk lint docs/guide.md --format json
mdchk inspect docs/guide.md --config .mdchk.yaml
```

Check JSON `complete`, `coverage`, and `errors`, then inspect parsing notes, uncertain regions, and rule execution status. Use the same explicit configuration for both commands when comparing results. Fix avoids uncertain regions. Resolve access or configuration errors first; for unsupported syntax, review the affected content manually and retain an example for a bug report. Inspection may return `0` even with incomplete or unresolved processing.

## A local link is unavailable rather than missing

A missing-target finding requires established absence. An omitted standalone resource is unknown unless its inventory is complete; supply an explicit `missing` entry or a complete inventory to establish absence. A file entry without content establishes existence, but fragment checks need Markdown content. Missing documents belong to MD057; MD051 does not duplicate that finding.

Check exact path case, the resource root, symlink components, and URL decoding. Root-relative `/path` references, root escapes, Git metadata, and all resource symlinks are blocked under the initial contract. Exclusions remove lint targets but do not prevent allowed read-only resource lookups. See the [resource contract](engineering/resources.md).

## An anchor or index option is rejected

Set `resources.anchor_style` and `resources.index_file` at configuration top level. The former rule-local options are rejected even on disabled rules. All anchor consumers share the selected style. Pandoc uses the [fixed profile](engineering/pandoc-compatibility.md); per-extension switches and automatic metadata recognition are not configuration options.

## An internal failure or unexpected status occurred

For `14`, or an unexpected process status, retain the command, working directory, `mdchk --version`, stdout, stderr, and a minimal input and configuration that reproduce the failure. Review those materials for private content before sharing them. A crash or output failure may prevent valid JSON output; capture the process status as well as the report.
