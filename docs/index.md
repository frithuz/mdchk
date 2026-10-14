# mdchk documentation

mdchk checks Markdown against your project's expectations and applies safe corrections when requested. Use it from the terminal, in editors and CI, or through an MCP server for AI agents.

## Get started

```sh
mdchk lint .
mdchk fix . --dry-run
mdchk fix .
```

Start with a read-only check. Preview safe corrections, then apply them. Reports identify remaining problems that need your attention. Defaults work without configuration; use the [configuration guide](configuration.md) to choose rules and excluded paths.

## Command syntax

```text
mdchk [global options] <command> [command options] [arguments]
```

For lint and fix, arguments are file or directory paths. Options can appear before or after path arguments. Use `--` to end option parsing when a path begins with a dash.

## Global options

| Option | Effect |
| --- | --- |
| `--help` | Show general help, or help for the selected command, and exit successfully |
| `--version` | Print the mdchk version and exit successfully |

```sh
mdchk --help
mdchk lint --help
mdchk --version
```

Help and version output do not load project configuration or process Markdown files. Configuration, result formats, previews, and server settings are command options described in the command guides.

## Commands

| Command | Purpose | Guide |
| --- | --- | --- |
| `lint` | Report Markdown problems without changing files | [Lint command](commands/lint.md) |
| `fix` | Apply safe corrections and report remaining problems | [Fix command](commands/fix.md) |
| `inspect` | Reveal processing details for one file or a selected line range | [Inspect command](commands/inspect.md) |
| `config` | Work with configuration, including project-wide validation | [Config commands](commands/config.md) |
| `mcp` | Provide lint and fix tools to AI agents in project or standalone mode | [MCP command](commands/mcp.md) |

## Understand and integrate results

- [Markdown formats](markdown-formats.md) defines CommonMark, GFM, Pandoc Markdown, and independently optional frontmatter support.
- [File selection](file-selection.md) explains target paths, exclusions, Git ignores, and selection details awaiting implementation.
- [Troubleshooting](troubleshooting.md) pairs common symptoms with diagnostic commands and remedies.
- [Result formats](output-formats.md) explains readable text, JSON, VS Code problem matcher output, and GitHub Actions annotations.
- [Exit codes](exit-codes.md) explains clean checks, remaining violations, and execution errors.
- [Configuration](configuration.md) explains project expectations and configuration discovery.
- [Directives](directives.md) explains inline rule enablement, disablement, and configuration permissions.

## Engineering documentation

See the [engineering overview](engineering.md) for internal design choices and the [MCP engineering design](engineering/mcp.md) for server modes, transports, and request handling.

The [configuration validation design](engineering/configuration.md) covers project scanning and exclusion handling.

The [inspection design](engineering/inspect.md) covers parser and rule instrumentation, line ranges, and directive attribution.

The [directive processing design](engineering/directives.md) defines shared recognition, rule-state tracking, and suppression behavior.
