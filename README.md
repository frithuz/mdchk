# mdchk

mdchk helps people and AI agents keep Markdown files consistent with their project's expectations. Check files without changing them, apply safe corrections when requested, and get a clear report of anything that still needs attention.

Start with the [documentation index](docs/index.md) for global options, commands, and integration guides.

GFM uses CommonMark 0.31.2 and all five pinned extensions: 2,117 approved
comparison units pass, including every published case. Default and explicit GFM
selection share the real lint, fix and inspection pipeline, including source
facts, directives, protected regions and candidate reparsing. MD047 is the first
production correction; MD001 heading-increment, MD003 heading-style, MD004 bullet-marker
analysis, MD005 sibling-list alignment analysis, MD007 bullet nesting indentation
analysis, MD029 numbered-list prefix analysis, MD030 list-marker separator analysis,
MD032 list-boundary blank-line analysis, MD069 repeated-list-marker analysis, MD076 list-item spacing analysis, MD077 list-continuation indentation analysis, MD009 trailing-whitespace analysis, MD010 tab-character analysis, MD012 blank-body-run analysis, MD013 maximum-line-width analysis, MD027 blockquote-separator analysis, MD028 blockquote-gap analysis, MD064 repeated-prose-space analysis, MD085 paragraph-continuation analysis, MD089 CJK/Latin-spacing analysis, MD035 horizontal-rule-style analysis, MD037 emphasis-whitespace analysis, MD018 missing opening-hash spacing, MD019 extra
opening-hash spacing, MD020 missing terminal-hash spacing, MD021 closed-heading
delimiter spacing, MD022 heading blank-line analysis, MD023 heading indentation
analysis, MD024 repeated heading analysis, MD025 multiple-title analysis, MD026
heading-end punctuation analysis, MD036 emphasis-as-heading analysis, MD041
opening-heading analysis, MD043 required-outline analysis, MD063 heading-capitalization
analysis, MD080 heading-anchor collision analysis, MD082 empty-section analysis, MD090
thematic-break-before-heading analysis and MD093 inline-formatting-in-headings analysis
are also available. MD013, MD026, MD036, MD043, MD063, MD080, MD082, MD085, MD089, MD090 and MD093 are
available but disabled by default. Other named rules remain unavailable. See the [parser
verification record](docs/engineering/gfm-verification.md).

## Check Markdown

First follow the [current-build quick start](docs/index.md#get-started-with-this-build):
use the published CommonMark+MD047 configuration, which explicitly disables other
ordinary checks. This reduced selection is not full Markdown conformance.
Without configuration, GFM is selected and missing default checks return `15`.
CommonMark has the same missing-rule limitation.

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

Safe corrections are small and specific. For example, mdchk can add a missing final newline. It preserves intentional Markdown hard breaks and leaves code examples, front matter, and ambiguous edits alone. Protected EOF contexts remain manual. Heading-level and code-language checks remain planned; their policies forbid guessing the intended structure or language. Apply currently requires Darwin arm64+cgo/local APFS; other native adapters refuse writes with `13`.

## Inspect processing details

Use [`mdchk inspect`](docs/commands/inspect.md) to reveal metadata, checksums, the syntax tree, rules, findings, directives, and parsing notes for one file. Add `--lines N` or `--lines N-M` to focus on a region. Inspection explains processing without making a validation pass or fail decision.

## Define project expectations

See the [configuration guide](docs/configuration.md) to choose project rules, rule settings, and excluded paths.

Use [`mdchk config validate`](docs/commands/config/validate.md) to check all eligible configuration definitions in a project.

## Use with AI agents

The planned MCP server is unavailable in this build. Its accepted future contract exposes `lint` and `fix` actions. Agents receive structured findings and fix results so they can check their work, apply safe corrections, and address the remaining problems.

Future server invocation (currently rejected):

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

mdchk provides file and directory checks, safe fixes with previews, project configuration, results for people and integrations, and a planned MCP server.

The [ruleset to implement](docs/rules/index.md) defines 87 checks with stable numeric IDs and reserved gaps. Rule pages are grouped by focus area. Each page specifies behavior, default enablement, options, examples, and correction policies.

The approved [format contracts](docs/markdown-formats.md) define CommonMark 0.31.2 and the fixed GFM profile. The [resource contract](docs/engineering/resources.md) defines bounded link/anchor lookup and standalone context. The internal CommonMark core passes all 652 specification examples in normalized and strict comparisons. CLI lint now selects and acquires immutable source snapshots, runs mandatory UTF-8 validation, parses Markdown, resolves inherited directive permissions and suppression, and executes available registered checks. It emits readable text by default or one bounded schema-v1 JSON result on stdout, including errors and partial outcomes. Lint, fix and config validate support all four reporters; inspect supports text/JSON. MD047 is the first production ordinary rule and correction, with native application bounded as above. Missing default rule implementations remain explicit coverage deficits. GFM's five extensions and independently optional opaque YAML/TOML frontmatter use the shared lint, fix and inspection pipeline. On-demand source-mapped YAML/TOML interpretation is available internally; MD001, MD025 and MD041 consume exact nonblank top-level titles on opt-in; MD003 checks heading syntax without metadata baselines; MD004 checks actual bullet markers against one document-wide first-bullet style or an explicit marker; MD005 compares structural marker indentation against each actual group's first direct item, retaining parent baselines across nesting; MD007 checks each actual bullet against its actual parent marker and padding or numbered content target, with exact root indentation and retained quote ancestry; MD018 checks missing opening-hash spacing in parser-owned paragraph candidates; MD019 checks excess spaces in actual ATX opening separators; MD020 checks missing separators before unescaped terminal hash runs; MD021 checks closed-heading delimiter gaps; MD022 checks minimum blank body lines around actual headings; MD023 checks leaf indentation on every actual heading line; MD024 compares normalized semantic heading text; MD043 matches an exact configured heading outline with wildcard entries; MD063 checks heading capitalization while protecting code and configured proper names; MD080 compares shared unsuffixed heading anchor bases under local anchor policy; MD082 checks global actual sections for substantive content; MD090 checks thematic breaks immediately before same-container headings with explicit slide exemptions; MD093 reports disallowed actual inline constructs inside headings; broader metadata-rule/inspection integration, other ordinary and named rules, MCP integration and production rendering remain separate deliveries; see the [delivered parser contract](docs/engineering.md#delivered-commonmark-core).

Full-document formatting, prose rewriting, remote link checks, dedicated editor extensions, and third-party rules are outside mdchk's scope.

See [the engineering design](docs/engineering.md) for accepted contracts and remaining implementation work. See the [contract index](docs/engineering/contracts.md) for the complete policy set.

## Contribute

See the [contributor guide](docs/contributing.md) for the Go 1.27.2 minimum,
build/test commands, current parser foundation, and native Git commit hook setup
and integration tests.
