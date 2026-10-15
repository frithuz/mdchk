# MCP server contract

Status: accepted on 2026-10-15; implementation and end-to-end verification remain pending.
These requirements incorporate the revised **1 MiB per-file limit** where applicable.
See the [contract index](contracts.md) for related specifications and issue ownership.

The [command guide](../commands/mcp.md) describes startup and tool use.
HTTP authorization and network policy follow the [HTTP contract](http.md);
all adapters share the [result schema](results.md) and [limits](limits.md).

## Sdk

### Module

github.com/modelcontextprotocol/go-sdk

### Version

v1.8.0

### Commit

3f3b699b2b67e1ed033a63d6651671dab53c2d32

### Choice

Use the official SDK for protocol lifecycle, discovery, cancellation and transport framing. Keep
shared operations, strict raw-input validation, output validation, limits and deployment
authorization in mdchk adapters.

### Comparison

- A focused standard-library implementation avoids SDK dependencies but would require maintaining two lifecycle models, version-specific wire shapes, Streamable HTTP, cancellation and conformance fixes. These are substantial continuing responsibilities for a two-tool service.
- The official SDK already implements both selected revisions, exposes raw arguments and configurable version support, and passed the isolated feasibility check. Its dependency graph and transport defaults still need explicit integration review; importing it does not enforce mdchk policies automatically.

### License record

The pinned LICENSE describes an Apache-2.0 transition with pre-existing contributions remaining MIT
where consent was not obtained. Preserve applicable license/notices and inventory the exact linked
dependencies; do not label the whole SDK simply MIT or simply Apache-2.0.

### Dependencies

Pin the exact module and transitive dependency checksums in go.mod/go.sum during implementation and
inventory the linked modules and applicable licenses. The SDK declares Go 1.25.0; the isolated probe
used Go 1.27.2. Choose and document a supported project Go toolchain.

### Upgrades

No floating latest dependency. Updates are routine reviewed changes requiring compatibility,
raw-validation, cancellation and limit regression checks before release; do not silently expand the
advertised protocol set.

## Protocol

### Supported revisions

- 2026-07-28
- 2025-11-25

### Preferred

2026-07-28

### Lifecycle

- Implement 2026-07-28 per-request metadata and server/discover; do not require an initialize handshake for that revision. Implement initialize/initialized only for the supported 2025-11-25 legacy revision.
- Set ServerOptions.SupportedProtocolVersions explicitly. Unsupported legacy initialization requests receive a supported legacy version as required by negotiation; clients unable to speak it disconnect. Unsupported modern versions and mismatched HTTP metadata cannot execute a tool.
- Serve both revisions over stdio and stateless Streamable HTTP at /mcp. HTTP uses complete application/json responses, no persistent MCP session ID, no replay/event store, no background tasks or persistent SSE stream. Authentication, Origin/Host/proxy rules and timeouts follow the HTTP contract.
- Exactly one startup mode: --root or --standalone. Exactly lint and fix, in a deterministic order. Expose only the tools capability with listChanged=false; explicitly disable SDK-default logging and unused prompts/resources/subscriptions/sampling/roots/elicitation features. The request resources inventory is tool input, not an MCP resources capability.
- No custom x-mcp-header annotations exposing document/config/path values. Set discovery/list cache policy to ttlMs=0 and private rather than inheriting the SDK public default. Tool results are not cached.
- Do not replay project fix automatically after a timeout/disconnect; a lost reply can follow a completed write. A caller must inspect actual saved results before choosing to issue a new call.

### Independent client fixtures

#### Repository

https://github.com/modelcontextprotocol/python-sdk

#### Tag

v2.3.0

#### Commit

2118f14f8a19bc158d8a1cf90af58d85d187f849

#### Policy

Pin this independent client and its dependency lock for the initial test matrix; also use explicit
captured/raw protocol fixtures for both revisions, malformed requests and negotiation. Passing
Go-to-Go smoke tests is not independent interoperability evidence. Advertise compatibility with
these protocol revisions and tested clients, not every product brand or every older SDK.

## Tool inputs

- Use four schema variants: project lint {paths?}; project fix {paths?,dry_run?}; standalone lint {config,files,resources?}; standalone fix {config,files,resources?,dry_run?}. dry_run is a strict Boolean default false and is forbidden on lint. Root/mode/authentication/limit/config-file options are never tool arguments.
- Use JSON Schema 2020-12, explicit types and additionalProperties=false for each closed request object. Reuse the shared complete configuration/resource schemas and validators, including exact version:1 and known rule IDs; do not independently duplicate rule option definitions. Schemas must be self-contained with local refs when published through discovery.
- Omitted project paths selects the root. A present paths array must be nonempty; entries are nonempty relative target spellings, not globs. Allow . for the root and a single terminal slash for a directory; other segments are canonical slash-separated names and reject parent/empty/dot segments, absolute/drive/UNC forms, backslashes, controls and malformed Unicode. A terminal slash requires a directory. Deduplicate overlapping targets through shared discovery without bypassing exclusions or confinement.
- Standalone files remains nonempty with strict {path,content} records and canonical logical names. The already accepted resources, duplicates, exclusion, configuration and content limits apply without change. No filesystem fallback or cross-call content/configuration cache.
- Validate bounded original bytes before lossy decoding: strict UTF-8, paired Unicode escapes, JSON syntax, depth and duplicate names after escape decoding. Validate the whole message before the SDK can normalize its envelope; retain Arguments as json.RawMessage and use Server.AddTool raw handlers, then schema/shared semantic validation before any operation. Never detect malformed Unicode by rejecting a valid literal U+FFFD.
- For stdio use a bounded validating frame reader/IOTransport; for HTTP bound and validate the original request body before SDK handling. Apply the accepted 16 MiB raw-message and 64-level JSON limits explicitly, not the SDK defaults. Keep malformed-message errors content-free and prevent recovery from executing partially parsed input.

## Tool annotations

### Lint both modes

#### Readonlyhint

true

#### Destructivehint

false

#### Idempotenthint

true

#### Openworldhint

false

### Fix project

#### Readonlyhint

false

#### Destructivehint

true

#### Idempotenthint

true

#### Openworldhint

false

### Fix standalone

#### Readonlyhint

true

#### Destructivehint

false

#### Idempotenthint

true

#### Openworldhint

false

### Meaning

Project fix always advertises possible mutation even if one call previews. Idempotence assumes
unchanged relevant content/configuration; it is not retry authorization. Annotations describe
behavior and never grant permissions.

## Results and errors

- Set structuredContent to the accepted schema_version:1 shared result object, with mode/operation-specific output schema. Include exactly one TextContent block containing its serialized JSON for compatibility. Both copies, escaping, JSON-RPC and revision metadata count toward the accepted 8 MiB total response cap.
- isError=false for domain clean/violations and true for domain incomplete/error. Findings alone are successful execution. A completed wire response can have resultType=complete under 2026-07-28 while the domain result has complete=false; protocol completion must not overwrite analysis completeness.
- Malformed JSON uses a parse error; valid JSON with an invalid/ambiguous protocol envelope uses invalid-request/invalid-params as appropriate; unknown methods use method-not-found and unknown tools invalid-params. Preserve standard revision-specific version errors. If a complete frame is available, reject it without killing service; disconnect/unrecoverable oversized framing can terminate that connection.
- Once a valid tools/call identifies a known tool, strict tool argument/schema/name/content validation failures return the shared usage error result with isError=true; invalid configuration uses configuration, file failures io, and operation faults internal. Unknown or mixed-mode tool arguments are rejected before execution. Do not convert these into server process exit codes.
- Runtime limits/deadlines/cancellation retain the accepted interruptions, coverage, partial writes and status precedence. Emit a bounded structured result when the selected protocol/connection permits; a cancelled or disconnected client may not receive it. Never claim a rollback or no-write solely because no response arrived.
- Bound serialization and preserve all mandatory per-file write outcomes before committing any changes. Use the accepted complete-record omission policy; never truncate returned content strings. Report effective operator limits in effective_limits. No hidden pagination/spooling or durable request storage.
- Use stderr for stdio diagnostics and reserve stdout for protocol frames. Log generated/request identifiers, counts, timings and stable error categories only; no raw document/config/token payloads or content-bearing validation messages. Identifiers originating from clients must be bounded and sanitized.

## Project configuration and access

Resolve and validate the project root at startup. Infer `.mdchk.yaml` within that root using the shared configuration loader, without searching ancestors outside the root. Use built-in defaults when no configuration is present. Apply one resolved configuration snapshot to each operation; reload between calls so later operations see saved configuration changes. Invalid configuration fails the call before any writes.

Use shared exclusion- and directive-permission resolution within the configured root. Ancestor global or per-rule directive denials cannot be relaxed by descendant configuration; retain restriction provenance in processing notes. Standalone configuration has no parent hierarchy; a supplied `allow_nested_excludes` setting never causes filesystem policy discovery. Higher-level restrictions and directive permissions cannot be changed by Markdown content.

Resolve request paths beneath the startup root. Configuration exclusions apply to explicit and discovered paths. Share the CLI's discovery, skip reporting, and empty-selection behavior. Reject escapes and symlinks for both configuration and document access. The startup root also confines every referenced resource, including excluded or unselected files; reading a resource does not lint or fix it. Enforce exact logical-name spelling and reject all resource symlink components, Git metadata, and unsupported types according to the [resource contract](resources.md).

Serialize project fix operations within the server instance. Retain the engine's stale-snapshot checks and per-file write guarantees; serialization does not protect against external writers. Lint and fix must each process immutable content snapshots. Project settings never expand access permissions or turn lint into fix.

## Standalone execution

Construct in-memory documents from request contents. Parse, lint, and compute fixes without temporary document files or filesystem lookups. Exclusions match supplied names relative to a virtual request root. Report excluded documents as skipped; if every document is excluded, return an empty-selection usage error.

Resource inventory validation precedes analysis. Preserve authoritative absence separately from unknown context and existence separately from content availability. File entries without content satisfy existence checks only. Charge submitted files and resource entries, lookup misses, parsing, and outputs against operation limits.

Configuration, document, and resource state are local to the call. Do not cache effective configuration or content across requests. Shared rule definitions must be immutable. Concurrent standalone calls can run independently within resource limits.

A standalone fix returns full corrected contents for every successfully processed document, whether changed or unchanged, plus changes and remaining findings. A dry run returns full proposed contents and diffs marked as proposed. Failed, skipped or explicitly withheld documents do not receive corrected content. Budget-driven withholding is recorded per target; never truncate a content string. Compute all candidate documents into one overlay before cross-file post-fix checks; resource-only entries retain submitted snapshots. Return only successfully processed targets. The caller owns persistence and any check that its local files still match the submitted inputs. Project mode instead rechecks the actual saved overlay after partial writes, invalidating stale dependencies; see [snapshot handling](resources.md#snapshots-caching-fixes-and-limits).

## Verification

### Scope

Isolated official SDK feasibility probe; no mdchk implementation or production conformance claim.

### Toolchain

go1.27.2 darwin/arm64

### Sdk

v1.8.0

### Matrix

- in-memory newline JSON / 2026-07-28
- in-memory newline JSON / 2025-11-25
- stateless HTTP JSON / 2026-07-28
- stateless HTTP JSON / 2025-11-25

### Checked

Discovery plus five raw-argument cases per combination: ordinary, duplicate key, escape-equivalent
duplicate, unpaired surrogate, nested duplicate. All 20 cases preserved the original argument JSON
bytes for subsequent validation.

### Passed cases

20

### Not checked

- actual process stdio
- independent Python client
- full raw-message rejection middleware
- invalid raw UTF-8 handling
- authentication/Origin/Host/proxies
- production queue/timeout/response accounting
- cancellation and partial-write transport behavior

- Freeze mode-specific schema documents using the accepted shared config/resource/result contracts; add positive and negative fixtures and output semantic validation in #189/#190/#197.
- Implement and fuzz strict bounded raw validation around SDK framing; cover escaped duplicate names, UTF-8, surrogate pairs, unknown fields, malformed envelope IDs, limits and continued service after errors.
- Run both modes over both actual transports with independently pinned Python and raw wire fixtures at both revisions. Test initialization/discovery differences, unsupported versions, cancellation/disconnects, errors, annotations, output-schema validation and structured/text parity.
- Review exact linked dependency licenses/notices and security updates, preserve go.sum and record why SDK defaults were overridden. Audit SDK logs for payload leakage.
- Keep full transport/rule parity in #196/#200/#206 to avoid making individual rule completion depend on all transport implementation.
- Apply the [HTTP deployment contract](http.md) for authentication, authorized roots, Origin/Host validation, proxies and protocol I/O timeouts before #202-#204 completion.

## Pinned references

- https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.8.0
- https://github.com/modelcontextprotocol/go-sdk/blob/3f3b699b2b67e1ed033a63d6651671dab53c2d32/mcp/server.go
- https://github.com/modelcontextprotocol/go-sdk/blob/3f3b699b2b67e1ed033a63d6651671dab53c2d32/mcp/protocol.go
- https://github.com/modelcontextprotocol/go-sdk/blob/3f3b699b2b67e1ed033a63d6651671dab53c2d32/mcp/streamable.go
- https://github.com/modelcontextprotocol/go-sdk/blob/3f3b699b2b67e1ed033a63d6651671dab53c2d32/LICENSE
- https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning
- https://modelcontextprotocol.io/specification/2026-07-28/server/tools
- https://github.com/modelcontextprotocol/python-sdk/releases/tag/v2.3.0
