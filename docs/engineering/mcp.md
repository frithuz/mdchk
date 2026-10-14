# MCP server engineering design

The MCP server supports project-backed operations and standalone operations on supplied configuration and Markdown content. Both modes call the same parser, rules, and correction engine. The [user guide](../commands/mcp.md) defines their observable behavior.

## Confirmed requirements

- Expose `lint` and `fix` through MCP.
- Support a project root from which configuration is inferred.
- Support standalone requests containing configuration and file content, including use as a web service.
- Preserve the internal parser and minimal third-party dependency goals.

The schema and transport choices below define the design proposal. User documentation presents the intended product interface.

## Modes and transports

Choose exactly one operation mode at startup: `--root PATH` or `--standalone`. Reject missing or conflicting mode selection. Mode does not change during a server process's lifetime.

Support `--transport stdio|http`, defaulting to stdio, for either mode. HTTP uses a single `/mcp` endpoint with MCP Streamable HTTP, rather than a custom REST lint API. `--listen` applies only to HTTP and defaults to `127.0.0.1:8080`. Follow the [MCP transport specification](https://modelcontextprotocol.io/specification/latest/basic/transports), and pin the supported protocol revision in the implementation and compatibility tests.

Keep protocol adaptation separate from transport and operation mode. Compare a focused standard-library implementation with the [official Go SDK](https://github.com/modelcontextprotocol/go-sdk), considering conformance maintenance and transitive dependencies. HTTP is part of the required service support; it is not deferred.

## Tool inputs

Expose two tools named `lint` and `fix`. Publish mode-specific schemas through MCP tool discovery, following the [MCP tools specification](https://modelcontextprotocol.io/specification/latest/server/tools). Reject unknown fields and inputs from the other mode rather than silently ignoring them.

| Mode | Shared arguments | Fix argument |
| --- | --- | --- |
| Project | Optional `paths` array of relative paths | Optional `dry_run`, default false |
| Standalone | Required `config` object and nonempty `files` array | Optional `dry_run`, default false |

Each standalone file is an object with required UTF-8 strings `path` and `content`. Paths are unique, nonempty, relative logical names using `/` separators; reject absolute paths, traversal segments, and duplicate names. They identify documents and provide exclusion matching, never filesystem locations. Empty content is valid. Do not infer additional files or fetch referenced documents.

Standalone configuration uses the project configuration's semantic schema as a JSON object, independent of the on-disk serialization choice. Require `version: 1`; omitted settings inherit built-in defaults. Do not accept a config path or load defaults from server-local project or user configuration. Validate the entire input and configuration before processing any document.

## Project configuration and access

Resolve and validate the project root at startup. Infer `.mdchk.yaml` within that root using the shared configuration loader, without searching ancestors outside the root. Use built-in defaults when no configuration is present. Apply one resolved configuration snapshot to each operation; reload between calls so later operations see saved configuration changes. Invalid configuration fails the call before any writes.

Use shared exclusion- and directive-permission resolution within the configured root. Ancestor global or per-rule directive denials cannot be relaxed by descendant configuration; retain restriction provenance in processing notes. Standalone configuration has no parent hierarchy; a supplied `allow_nested_excludes` setting never causes filesystem policy discovery. Higher-level restrictions and directive permissions cannot be changed by Markdown content.

Resolve request paths beneath the startup root. Configuration exclusions apply to explicit and discovered paths. Share the CLI's discovery, skip reporting, and empty-selection behavior. Reject escapes and symlinks for both configuration and document access. Root restrictions also apply to references a future rule might inspect.

Serialize project fix operations within the server instance. Retain the engine's stale-snapshot checks and per-file write guarantees; serialization does not protect against external writers. Lint and fix must each process immutable content snapshots. Project settings never expand access permissions or turn lint into fix.

## Standalone execution

Construct in-memory documents from request contents. Parse, lint, and compute fixes without temporary document files or filesystem lookups. Exclusions match supplied names relative to a virtual request root. Report excluded documents as skipped; if every document is excluded, return an empty-selection usage error.

Configuration and document state are local to the call. Do not cache effective configuration or content across requests. Shared rule definitions must be immutable. Concurrent standalone calls can run independently within resource limits.

A standalone fix returns full corrected contents for every successfully processed document, whether changed or unchanged, plus changes and remaining findings. A dry run returns full proposed contents and diffs marked as proposed. Failed or skipped documents do not receive corrected content. The caller owns persistence and any check that its local files still match the submitted inputs.

## Results and errors

Adapt the shared result model into versioned structured MCP output: operation, mode, checked and skipped documents, diagnostics, changes, and errors. Include an overall status distinguishing `clean`, `violations`, and `error`, and an explicit completeness indicator. Standalone fixes additionally return per-file contents; distinguish returned, written, and proposed changes so clients never mistake an in-memory result for a saved file.

Use the shared error categories `usage`, `configuration`, `io`, and `internal`. Keep deterministic ordering. Findings alone are successful tool execution; a tool execution failure uses the protocol's error result mechanism and preserves available partial results. Malformed protocol messages use protocol errors. Do not place per-call outcomes in the server process's exit status or shut down on ordinary tool errors.

Use the same diagnostic coordinates and preservation checks as the CLI. After fixing, diagnostics refer to resulting content. Deferred conflicting or unsafe edits remain findings with explanations. A response must not claim completeness if work or output is truncated.

Lint is read-only in both modes. Project fix can write files; standalone fix only transforms submitted data. Publish mode-appropriate tool descriptions and annotations. Enforce these behaviors in server code rather than relying on annotations for permissions.

## Service boundaries and resource limits

Bind HTTP to loopback by default. For remote deployment, require an explicit authenticated HTTPS deployment boundary, document proxy trust, and implement the selected MCP revision's HTTP security requirements, including Origin validation. Protect project-mode access with deployment authorization as well as root confinement. The standalone request's `config` cannot alter server limits or authentication.

Bound request size, document count, per-document bytes, concurrent work, execution time, and response bytes. Configure limits at startup and return actionable errors when exceeded. Propagate cancellation through parsing and rule execution. Avoid returning partial corrected content strings; a size failure must clearly mark the response incomplete. Numeric defaults remain a design decision.

Keep submitted content and configuration out of normal logs and do not persist request payloads. Log request identifiers, timings, counts, and error categories. Reserve stdout for protocol messages under stdio. Never execute code from Markdown, invoke a shell, or fetch remote links as part of a tool call.

## Verification

Test CLI, project MCP, and standalone MCP parity for identical contents and effective configuration. Verify that standalone requests cannot read or write filesystem documents, settings do not leak between calls, and fixes return usable full contents with accurate locations.

Cover mode-specific schema rejection, missing configuration, duplicate names, exclusions, empty inputs, configuration reloads, traversal, symlinks, concurrent fixes, partial failures, cancellation, and request and response limits. Test both modes over both transports, including protocol-version handling, HTTP authentication boundaries, Origin validation, and separation of protocol traffic from logs.

## Open design decisions

- Supported MCP revision and client compatibility range.
- Internal protocol implementation or a justified SDK dependency.
- Authentication deployment details and server resource limits.
- Final structured result schema and diff representation.
