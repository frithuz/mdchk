# mdchk

mdchk helps people and AI agents keep Markdown files consistent with their project's expectations. Check files without changing them, apply safe corrections when requested, and get a clear report of anything that still needs attention.

Start with the [documentation index](docs/index.md) for global options, commands, and integration guides.

## Check Markdown

Run `lint` on a file or directory to find problems without modifying files:

```sh
mdchk lint README.md
mdchk lint docs/
```

Each finding identifies the file, line, column, rule, and a useful explanation. Findings distinguish problems that can be fixed automatically from those that need a person or agent to decide what to change.

An illustrative finding:

```text
docs/guide.md:18:1 heading-increment: Heading jumps from level 2 to level 4. Choose the intended heading level. [manual]
```

## Apply safe corrections

Run `fix` to correct problems where the change can preserve the document's meaning:

```sh
mdchk fix docs/
mdchk fix docs/ --dry-run
```

The report shows which files changed, what was corrected, and which problems remain. `--dry-run` shows a proposed diff without writing files. After applying corrections, mdchk checks the resulting files again so reported locations match the saved content.

Safe corrections are small and specific. For example, mdchk can add a missing final newline. It preserves intentional Markdown hard breaks and leaves code examples, front matter, and ambiguous edits alone. It reports a skipped heading level rather than guessing the intended structure, and reports a missing code-block language rather than guessing the language.

## Inspect processing details

Use [`mdchk inspect`](docs/commands/inspect.md) to reveal metadata, checksums, the syntax tree, rules, findings, directives, and parsing notes for one file. Add `--lines N` or `--lines N-M` to focus on a region. Inspection explains processing without making a validation pass or fail decision.

## Define project expectations

See the [configuration guide](docs/configuration.md) to choose project rules, rule settings, and excluded paths.

Use [`mdchk config validate`](docs/commands/config/validate.md) to check all eligible configuration definitions in a project.

## Use with AI agents

An MCP server exposes `lint` and `fix` actions. Agents receive structured findings and fix results so they can check their work, apply safe corrections, and address the remaining problems.

Start the server for your project:

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

mdchk provides file and directory checks, safe fixes with previews, project configuration, results for people and integrations, and an MCP server.

Rules cover final newlines, redundant trailing whitespace, heading level jumps, missing fenced-code languages, and configured list marker consistency. Language labels and list marker style are opt-in; the other rules are enabled by default.

Full-document formatting, prose rewriting, remote link checks, dedicated editor extensions, and third-party rules are outside mdchk's scope.

See [the engineering design](docs/engineering.md) for confirmed constraints, proposed implementation choices, and decisions still to make.
