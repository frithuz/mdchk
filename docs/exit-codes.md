# mdchk exit codes

Use the exit code to decide whether a check passed, Markdown problems remain, or the command could not complete successfully. Read the accompanying report for file locations, changes, and error details.

| Code | What it means | What to do |
| --- | --- | --- |
| `0` | A check completed without violations, configuration validation succeeded, or inspection completed. | Review the report; inspect does not make a validation decision. |
| `10` | The command completed, but rule violations remain. | Review the findings. Use `fix` for available safe corrections and address other findings manually. |
| `11` | The command usage is invalid, or lint or fix selected no files. | Check the command, flags, paths, and exclusions. |
| `12` | Configuration could not be loaded or validated. | Check the config path, permissions, settings, rule names, and schema version. |
| `13` | A file or I/O error prevented a complete run. | Review failed reads or writes, missing files, unsupported encodings, or output errors. If a file changed during fixing, check its current contents before retrying. |
| `14` | mdchk encountered an internal execution failure. | Keep the error report and command details for a bug report. |

## Interpreting lint and fix results

For `lint`, `0` means the selected files pass the enabled rules; `10` means problems were found. Lint does not change files.

For `fix`, the code describes the files after corrections: `0` means no violations remain, and `10` means some still need attention. A violation that cannot be safely fixed is a finding, not an execution error.

For `fix --dry-run`, the code describes the predicted result after the proposed corrections. A `0` can therefore accompany a diff that still needs to be applied. No files are written; inspect the preview before running `fix` to apply it.

An error during fixing can occur after other files have already changed. Check both the changes and failures in the report before retrying. A failed write returns `13`, even if the proposed correction would have resolved the violation.

## When multiple problems occur

Invalid usage stops the command before processing. Invalid configuration stops lint and fix before file processing. An unreadable configuration file is a configuration error (`12`), rather than a general file error.

During processing, an internal failure (`14`) takes precedence over a file or I/O error (`13`), and either takes precedence over rule violations (`10`). The report includes all encountered problems; the exit code summarizes the most serious outcome.

## Interpreting configuration validation

[`config validate`](commands/config/validate.md) returns `0` when every discovered definition is valid, including when no configuration files are found. It returns `12` for invalid or unreadable configurations and continues scanning other eligible files. It does not use Markdown violation code `10`.

If scanning also encounters traversal or I/O errors, `13` takes precedence over `12`; an internal failure (`14`) takes precedence over both. Check the report for all errors and whether the scan was complete.

## Interpreting inspection

[`inspect`](commands/inspect.md) returns `0` when it produces an inspection, even if the report contains Markdown findings, unresolved configuration, directive problems, or parsing notes. Those are processing observations rather than failed validation. Codes `10` and `12` are not used for observations in an inspection report.

Invalid arguments or line bounds return `11`. Target access or output errors that prevent inspection return `13`; internal execution failures return `14`. Use lint or config validate for a validation result.

## Using the codes in automation

Run the compiled `mdchk` executable and capture its status directly. Treat `0` as success, `10` as a completed check with Markdown problems, and `11` through `14` as errors requiring investigation. For detailed processing, use `--format json` and inspect the findings and error categories. The [result formats guide](output-formats.md) also covers VS Code problem matcher output and GitHub Actions annotations; selecting a format does not change the exit codes.

Codes `1` and `2` are reserved for failures outside mdchk's normal result handling. Any other unexpected status should also be investigated using the error output; do not interpret it as a rule violation. A launcher, runtime failure, or process termination may produce such a status.
