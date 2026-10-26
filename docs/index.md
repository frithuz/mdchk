# mdchk documentation

mdchk checks Markdown against your project's expectations and applies safe corrections when requested. Use it from the terminal, in editors and CI, or, once implemented, through the planned MCP server for AI agents.

## Current implementation

The executable currently provides general and command-specific help, version
output, lint argument parsing, configuration preparation, bounded source acquisition,
mandatory UTF-8 validation, parsing, inherited directive suppression and
registered-rule execution. Build it from a checkout with
`go build -o mdchk ./cmd/mdchk`. Lint loads and validates the selected configuration
before target access; invalid or unavailable settings return `12`. This build's
available flavors are CommonMark and GFM; omission selects the full GFM profile.
After preparation, target I/O and invalid UTF-8 return `13`; successful empty
selection returns `11`. MD047 terminating-newline, MD001 heading-increment, MD003 heading-style, MD018 missing opening-hash spacing, MD019 extra opening-hash spacing, MD020 missing terminal-hash spacing, MD021 closed-heading delimiter spacing, MD022 heading blank-line analysis, MD023 heading indentation analysis and MD024 repeated heading analysis are implemented; missing
enabled defaults for other rules still return `15`. Valid UTF-8 with all ordinary rules disabled can
return clean `0`; available registered checks retain findings and coverage.
See [lint's current implementation](commands/lint.md#current-implementation) for
the available configuration and remaining execution work. Help and version do
not access the project. Lint reports readable text by default or one schema-v1
JSON object with `--format json` on stdout; these formats reserve stderr for execution logs.
Reports retain findings, coverage reasons, errors, interruptions and omissions.
Lint, fix and config validate support VS Code diagnostics and GitHub annotations
with safe workflow command encoding. Inspect remains text/JSON only.
`config validate` now scans project definitions read-only and reports bounded
all four result formats, including completed validity counts, skipped paths, located
errors and actual scan completeness. See its [command guide](commands/config/validate.md).
`inspect` reports one immutable target in labeled text or versioned JSON, with
physical-line selection, AST, effective rule counts, directives, provenance and
explicit availability. Completed observations return inspected/0; runtime or
reporting interruption returns incomplete/15. See its [command guide](commands/inspect.md).
`fix --dry-run` uses the same selection/configuration and reports validated
candidate outcomes in all four formats without writing files; only text/JSON
include preview diffs. MD047 supplies the first production correction, with minimal terminal-newline
changes and conservative protected-content deferrals. Apply uses verified native replacement on Darwin arm64+cgo/local APFS and reports actual saved content; unsupported adapters refuse writes. MCP remains planned and is rejected by this build.

## Get started with this build

Build `mdchk` as above and put the executable on PATH. In a disposable project,
copy the [current example configuration](examples/editor-ci/.mdchk.yaml) to
`.mdchk.yaml` and the [example document](examples/editor-ci/guide.md) to
`docs/guide.md`. This explicitly selects CommonMark and MD047 while disabling the
other ordinary checks. It is a reduced rule selection, not full Markdown
conformance. Then run:

```sh
mdchk lint docs/guide.md
mdchk fix docs/guide.md --dry-run
mdchk fix docs/guide.md
```

The example has no terminal newline: lint returns `10` with MD047 at line 1,
scalar column 5. Preview returns `0` with a proposed newline and leaves the file
unchanged. Apply saves that newline on the verified Darwin arm64+cgo/local APFS
adapter; unsupported adapters refuse with `13` and leave the file unchanged.
After a successful apply, lint returns `0`.

Without configuration, the supported default GFM profile analyzes the document,
but unavailable default checks make lint and fix incomplete (`15`). CommonMark
alone has the same missing-rule coverage. Inspect can complete its observation
with exit `0` while reporting that coverage; unavailable selected contexts or
explicit rules fail configuration with `12`.
See the [configuration guide](configuration.md) before choosing another rule
selection, and the [runnable integration setup](output-formats.md#runnable-integration-setup)
for editor and CI examples.

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

Help and version output do not load project configuration or process Markdown files.
Command identity and option syntax/scope are checked first: unknown commands,
unsupported options or formats, malformed limits/ranges, and missing option
operands return usage `11` even with `--help` or `--version`. Valid metadata
requests bypass target count and target existence checks; help wins if both
flags appear. Metadata may precede the command or follow it (including between
`config` and `validate`). After `--`, these spellings are literal path arguments.

The executable preserves a supplied Go main-module version, including release
and pseudo-versions. Absent build information, an empty version, or `(devel)`
uses `dev`; VCS settings do not independently override that choice. A normal
checkout build may have a Go-synthesized pseudo-version, while a
`go build -buildvcs=false` checkout build uses `dev`. Inspect's `Binary version`
is exactly the same selected value printed after `mdchk ` by `--version`.
 Configuration, result formats, previews, and server settings are command options described in the command guides.

## Commands

| Command | Purpose | Guide |
| --- | --- | --- |
| `lint` | Report Markdown problems without changing files | [Lint command](commands/lint.md) |
| `fix` | Preview with `--dry-run`; apply on the verified native adapter | [Fix command](commands/fix.md) |
| `inspect` | Reveal processing details for one file or a selected line range | [Inspect command](commands/inspect.md) |
| `config` | Work with configuration, including project-wide validation | [Config commands](commands/config.md) |
| `mcp` | Planned; unavailable in this build | [MCP command](commands/mcp.md) |

## Rules

The [ruleset to implement](rules/index.md) covers 87 rules, grouped by focus area, with one planning page per rule.

## Understand and integrate results

- [Markdown formats](markdown-formats.md) defines CommonMark, GFM, and independently optional frontmatter support.
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


The [resource contract](engineering/resources.md) defines project boundaries, standalone inventories, shared anchor/index settings, lookup outcomes, and fix snapshots.

The [inspection design](engineering/inspect.md) covers parser and rule instrumentation, line ranges, and directive attribution.

The [directive processing design](engineering/directives.md) defines shared recognition, rule-state tracking, and suppression behavior.

The [accepted contract index](engineering/contracts.md) links all policy decisions and owning issues. Detailed contracts cover [frontmatter](engineering/frontmatter.md), [integrations](engineering/integrations.md), [rule behavior](engineering/rule-behavior.md), [fix safety](engineering/fixes.md), [results and schemas](engineering/results.md), [resource limits](engineering/limits.md), and [HTTP deployment](engineering/http.md).
