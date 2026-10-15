# mdchk inspect

Reveal how mdchk processes one Markdown file: its identity, parser output, rules, findings, directives, and any limits on reliable parsing. Inspect is read-only and reports processing details rather than determining whether the file passes validation.

```text
mdchk inspect [options] <file>
```

Exactly one file path is required. Paths may be absolute or relative to the working directory. Directories are not accepted.

```sh
mdchk inspect README.md
mdchk inspect /path/to/project/docs/guide.md
mdchk inspect docs/guide.md --lines 18-30
```

## Options

| Option | Effect |
| --- | --- |
| `--lines N` | Focus on one line |
| `--lines N-M` | Focus on an inclusive range of lines |
| `--config PATH` | Select a configuration inside the target's discovery boundary |
| `--format text\|json` | Choose readable inspection output or structured data; defaults to `text` |

The [global options](../index.md#global-options) also apply. Line numbers start at `1`. Bounds must be positive, ordered, and within the file. Omit `--lines` to inspect the whole file. Empty files can be inspected without a line range.

## File and processing details

Every inspection includes:

| Detail | Meaning |
| --- | --- |
| Resolved path | Full absolute path on the host, resolving symlinks |
| Binary version | Version of mdchk performing the inspection |
| File size | Human-readable size followed by the exact byte count, such as `1.5 KiB (1536 bytes)` |
| SHA-1 | Checksum of the full file's original bytes |
| SHA-256 | Checksum of the same original bytes |
| Markdown flavor | Effective flavor from configuration, including whether it came from defaults |
| Abstract syntax tree | Parsed nodes, nesting, source locations, and relevant attributes |
| Rules run | Rule identifiers, effective options, execution status, and coverage relevant to the inspected area |
| Findings | Reported issues with locations, rule identifiers, and fix availability; suppressed findings are identified separately |
| Rule counts | Total active and inactive rules in the inspected area |
| Directives | Exact directive text, source locations, affected rules, and effective scope |
| Parsing notes | Unsupported or ambiguous syntax, uncertain regions, and other reasons processing may be incomplete |

Metadata and checksums describe the entire file even when a line range is selected. A note that no findings were reported does not imply complete parsing coverage; check the parsing notes and rule execution details.

## Focus on a region

The whole document is parsed before the display is narrowed. This preserves context from enclosing lists, code blocks, and directives elsewhere in the file.

With `--lines`, the AST includes nodes overlapping the selected lines, plus their ancestors for context. Source locations remain the original file's locations. Rule details and findings focus on that region. File-wide rules appear only when their relevant source location overlaps it.

Counts use the known rule catalog, including unavailable implementations: a rule is active when enabled for at least part of the selected region, and inactive when disabled throughout it. Partially disabled rules count once as active, with disabled subranges listed. Active does not necessarily mean executed: the rule may not apply to the syntax there, its implementation may be unavailable, or parsing uncertainty may prevent a reliable check. Execution status explains the difference.

The directive list includes directives within the region and any earlier directives still affecting it. When directives disable rules, inspect names the exact rules and the precise source directives responsible, including their locations and affected lines. A directive outside the selected lines is labeled as context. Unknown or malformed directives appear with explanatory notes rather than disappearing from the report.

See [directives](../directives.md) for comment syntax, onward scope, and global or per-rule permissions. Inspect distinguishes allowed changes from requests blocked by configuration.

## Configuration and excluded files

Without `--config`, inspect discovers configuration upward from the resolved file's directory, stopping at the repository root when identifiable, or the filesystem root otherwise. This lets an absolute target outside the working directory use its own project's expectations.

Inspect can reveal an explicitly named file even when normal lint or fix selection excludes it. The report identifies the exclusion, then processes the file using the effective rules. It never broadens MCP access permissions.

If configuration is invalid, inspect shows the configuration problems and marks flavor and rule data as unresolved. It still reports available file metadata, checksums, and raw directive candidates. It does not silently substitute default parsing or claim an AST based on the requested configuration.

See [configuration](../configuration.md) for settings and [result formats](../output-formats.md) for structured inspection output.

## Understand the result

A completed inspection returns `0`, regardless of Markdown findings, directive problems, configuration problems shown in the report, or parsing notes. It does not return validation codes `10`, `12`, or `15` for those observations. Use [lint](lint.md) or [config validate](config/validate.md) when you want a validation result.

Errors that prevent inspection still use the operational codes: `11` for missing or extra arguments, a directory target, or invalid line bounds; `13` for an unreadable or missing target or output failure; and `14` for an internal execution failure. An unreadable configuration is shown as unresolved configuration within the inspection when the target itself is readable. See [exit codes](../exit-codes.md).
