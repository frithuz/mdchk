# mdchk documentation

mdchk checks Markdown against your project's expectations and applies safe corrections when requested. Use it from the terminal, in editors and CI, or through an MCP server for AI agents.

## Current implementation

The executable currently provides general help, lint help, version output, lint
argument parsing, and configuration preparation. Build it from a checkout with
`go build -o mdchk ./cmd/mdchk`. Lint loads and validates the selected configuration
before target access; invalid or unavailable settings return `12`. This build's
available flavor is CommonMark; unavailable default GFM is never substituted.
Successful preparation returns `15` and explains that no files were checked.
See [lint's current implementation](commands/lint.md#current-implementation) for
the available configuration and remaining execution work. Help and version do
not access the project. The result reporters tracked by #128 remain pending;
`--format` parses the documented names but produces no formatted report. Errors
currently use plain stderr text.
The other commands below describe planned behavior and are rejected by this build.

## Get started (planned execution)

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

## Rules

The [ruleset to implement](rules/index.md) covers 89 rules, grouped by focus area, with one planning page per rule.

## Understand and integrate results

- [Markdown formats](markdown-formats.md) defines CommonMark, GFM, Pandoc Markdown, and independently optional frontmatter support.
- [File selection](file-selection.md) explains target paths, exclusions, Git ignores, and pattern syntax and cross-platform selection.
- [Troubleshooting](troubleshooting.md) pairs common symptoms with diagnostic commands and remedies.
- [Result formats](output-formats.md) explains readable text, JSON, VS Code problem matcher output, and GitHub Actions annotations.
- [Exit codes](exit-codes.md) explains clean checks, remaining violations, incomplete analysis, and execution errors.
- [Configuration](configuration.md) explains project expectations and configuration discovery.
- [Directives](directives.md) explains inline rule enablement, disablement, and configuration permissions.

## Engineering documentation

See the [engineering overview](engineering.md) for internal design choices and the [MCP engineering design](engineering/mcp.md) for server modes, transports, and request handling.

The [configuration validation design](engineering/configuration.md) covers project scanning and exclusion handling.

The [GFM compatibility contract](engineering/gfm-compatibility.md) defines the CommonMark 0.31.2 foundation, pinned extension sources, corpus ingestion, source spans, and conformance requirements.

The [Pandoc compatibility contract](engineering/pandoc-compatibility.md) and [extension manifest](engineering/pandoc-profile.json) define the fixed reader target, source preservation, and rule-consumer requirements.

The [resource contract](engineering/resources.md) defines project boundaries, standalone inventories, shared anchor/index settings, lookup outcomes, and fix snapshots.

The [inspection design](engineering/inspect.md) covers parser and rule instrumentation, line ranges, and directive attribution.

The [directive processing design](engineering/directives.md) defines shared recognition, rule-state tracking, and suppression behavior.

The [accepted contract index](engineering/contracts.md) links all policy decisions and owning issues. Detailed contracts cover [frontmatter](engineering/frontmatter.md), [integrations](engineering/integrations.md), [rule behavior](engineering/rule-behavior.md), [fix safety](engineering/fixes.md), [results and schemas](engineering/results.md), [resource limits](engineering/limits.md), and [HTTP deployment](engineering/http.md).
