# mdchk

mdchk helps people and AI agents keep Markdown files consistent with their project's expectations. Check files without changing them, apply safe corrections when requested, and get a clear report of anything that still needs attention.

Start with the [documentation index](docs/index.md) for global options, commands, and integration guides.

## Current status

mdchk is under active development. This build provides:

- **Markdown formats.** [CommonMark 0.31.2](docs/markdown-formats.md) passes all 652 specification examples. GFM uses CommonMark plus all five pinned extensions and passes 2,117 approved comparison units, including every published case; see the [parser verification record](docs/engineering/gfm-verification.md). GFM is the default. Optional YAML and TOML frontmatter is supported in both formats.
- **Rules.** 82 of the [87 rules](docs/rules/index.md) are implemented for both formats; the table rules MD055, MD056, MD058, MD060 and MD075 apply to GFM only.

  - Disabled by default: MD013, MD026, MD033, MD036, MD040, MD043, MD044, MD054, MD060, MD061, MD063, MD070, MD072, MD073, MD074, MD080, MD081, MD082, MD083, MD085, MD087, MD088, MD089, MD090, MD091 and MD093.
  - Not yet implemented: MD066, MD067, MD068, MD084 and MD095.
- **Corrections.** MD047, a missing final newline, is the only automatic correction so far. Every other rule reports findings for a person or agent to resolve.
- **Commands.** `lint`, `fix`, `inspect` and `config validate` are available. The [MCP server](#use-with-ai-agents) is planned.

## Check Markdown

First follow the [current-build quick start](docs/index.md#get-started-with-this-build). It uses the published CommonMark+MD047 configuration, which explicitly disables other checks. Without configuration, GFM is selected and every rule enabled by default runs, so `lint` reports complete coverage and exits with `0` or `10`.

Run `lint` on a file or directory to find problems without modifying files:

```sh
mdchk lint README.md
mdchk lint docs/
```

Each finding identifies the file, line, column, rule, and a useful explanation. Findings distinguish problems that can be fixed automatically from those that need a person or agent to decide what to change.

An MD001 finding reports the offending heading without offering a correction:

```text
docs/guide.md:18:1 MD001: Heading level must not increase by more than one. [manual]
```

## Apply safe corrections

Run `fix` to correct problems where the change can preserve the document's meaning:

```sh
mdchk fix docs/
mdchk fix docs/ --dry-run
```

The report shows which files changed, what was corrected, and which problems remain. `--dry-run` shows a proposed diff without writing files. After applying corrections, mdchk checks the resulting files again so reported locations match the saved content.

Safe corrections are small and specific. For example, mdchk can add a missing final newline. It preserves intentional Markdown hard breaks and leaves code examples, front matter, and ambiguous edits alone. Some rules, such as heading levels and code block languages, will always report instead of correcting, because a fix would have to guess the intended structure or language.

Applying corrections works on Linux, macOS and Windows, including builds without cgo; other operating systems refuse writes with exit code `13`. Concurrent writers are detected but not locked out; see [native writes](docs/engineering/native-writes.md).

## Inspect processing details

Use [`mdchk inspect`](docs/commands/inspect.md) to reveal metadata, checksums, the syntax tree, rules, findings, directives, and parsing notes for one file. Add `--lines N` or `--lines N-M` to focus on a region. Inspection explains processing without making a validation pass or fail decision.

## Define project expectations

See the [configuration guide](docs/configuration.md) to choose project rules, rule settings, and excluded paths.

Use [`mdchk config validate`](docs/commands/config/validate.md) to check all eligible configuration definitions in a project.

## Use with AI agents

The MCP server is planned and not available in this build; `mdchk mcp` is currently rejected. Its accepted contract exposes `lint` and `fix` actions, returning structured findings and fix results so agents can check their work, apply safe corrections, and address the remaining problems.

```sh
mdchk mcp --root .
```

Project mode infers configuration from the project root and operates on its files. Standalone mode accepts configuration and file contents with each request and returns corrected content for the caller to save. Both modes support safe fixes and previews; standalone mode can also run as a web service.

See the [MCP command guide](docs/commands/mcp.md) for setup, request examples, and result handling.

## Use in continuous integration

```sh
mdchk lint . --format json
```

See the [result formats guide](docs/output-formats.md) for readable output, JSON, VS Code problem matcher output, and GitHub Actions annotations. Use the [exit code guide](docs/exit-codes.md) to distinguish a clean result, remaining Markdown problems, and errors that prevented a complete run.

## Rules and scope

The [rules index](docs/rules/index.md) defines 87 checks with stable numeric IDs and reserved gaps, grouped by focus area. Each rule page specifies behavior, default enablement, options, examples, and correction policy. The [resource contract](docs/engineering/resources.md) defines how link and anchor checks look up other files.

Full-document formatting, prose rewriting, remote link checks, dedicated editor extensions, and third-party rules are outside mdchk's scope.

See [the engineering design](docs/engineering.md) for accepted contracts and remaining implementation work, and the [contract index](docs/engineering/contracts.md) for the complete policy set.

## Contribute

See the [contributor guide](docs/contributing.md) for the Go 1.27.2 minimum, build and test commands, the Git hook setup, and integration tests.
