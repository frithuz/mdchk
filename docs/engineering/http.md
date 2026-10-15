# HTTP deployment and authorization contract

Status: accepted on 2026-10-15; implementation and end-to-end verification remain pending.
These requirements incorporate the revised **1 MiB per-file limit** where applicable.
See the [contract index](contracts.md) for related specifications and issue ownership.

## Deployment profiles

### Local

- Default when no operator HTTP policy is supplied: listen on 127.0.0.1:8080, plain HTTP, /mcp, without credentials. Only literal loopback bind addresses are allowed for this profile; wildcard, unspecified and non-loopback addresses fail startup.
- This profile trusts native processes and users able to reach the local listener. It is not an OS-user isolation mechanism. Project callers have lint/preview/apply access within the startup root. Use stdio or the authenticated profile on a host whose local users/processes are not trusted.
- Default allowed browser origins is empty: reject every present Origin on /mcp unless explicitly allowlisted. A missing Origin is allowed for native clients, subject to exact Host and binding checks. Origin checking does not authenticate native clients.

### Direct tls

- Explicit operator policy is required. mdchk serves HTTPS itself using configured certificate/key files and an exact public_url; require TLS 1.2 or newer. Remote MCP requests always require OAuth access tokens verified inside mdchk.
- Require a literal bind address and valid listener/certificate/authentication configuration before accepting requests. No automatic certificate issuance, HTTP-to-HTTPS redirect listener, plaintext fallback or remote anonymous profile.

### Proxy tls

- Explicit operator policy is required. A colocated reverse proxy terminates public HTTPS and forwards to an mdchk listener bound to a literal loopback address. The initial backend transport is same-host loopback only; plaintext forwarding between machines is outside this profile.
- Require an explicit nonempty list of allowed immediate proxy peer IP addresses, all loopback. Verify the actual socket peer against that list. The proxy preserves the configured public Host and the original Authorization and Origin values, and forwards the MCP protocol headers unchanged.
- Derive external URLs and the expected authority solely from the operator public_url. Forwarded, X-Forwarded-* and proxy identity headers grant no identity, host, scheme, root or client-IP authority and are not used for authorization. mdchk independently verifies the access token on every request.
- The proxy must not retry MCP POSTs, cache results, log tokens or payloads, buffer request/response bodies to disk, or hide client disconnects. Preserve upstream statuses and WWW-Authenticate. Its timeouts and body limits must permit the configured mdchk bounds; document deployment settings and verify them in #204.
- Replace the SDK localhost Host heuristic with the explicit public-authority and socket-peer checks for this profile. Disabling the SDK heuristic alone is not a valid configuration.

## Operator configuration

- Add --http-policy PATH, valid only with --transport http. The file is strict version:1 JSON owned by the server operator and read at startup. It is independent of .mdchk.yaml, standalone config and tool arguments. Reject unknown fields, malformed Unicode, duplicate names and conflicting profile settings. Limit the file to 1 MiB and nesting depth 64.
- Fields are version, deployment (local/direct_tls/proxy_tls), allowed_origins (default []), and profile-specific public_url, oauth, tls, trusted_proxy_ips. --listen remains the bind option and defaults to 127.0.0.1:8080. Relative certificate/key/JWKS paths resolve beside the operator policy file.
- Remote public_url is an exact HTTPS MCP resource URL with canonical authority, an absolute path ending in /mcp, and no userinfo/query/fragment. The internal endpoint remains /mcp; a proxy may map the one configured external prefix to it. Metadata URLs are derived statically from public_url, never request headers.
- Remote oauth requires issuer and jwks_file. issuer is one exact HTTPS authorization-server issuer. jwks_file is an operator-provisioned public signing-key set, read once at startup; token-controlled key URLs, dynamic issuer discovery and token introspection are unsupported initially. Rotating trusted keys requires updating the file and restarting; overlap old/new keys as appropriate.
- direct_tls requires tls.cert_file and tls.key_file and forbids trusted_proxy_ips. proxy_tls requires trusted_proxy_ips and forbids direct TLS keys. local forbids remote authentication/TLS/proxy fields; its allowed origins may contain only explicit loopback HTTP origins. Remote allowed origins must be HTTPS.
- Security policy and keys are immutable during the process lifetime. Changing deployment or rotating keys is an operator restart, not a document/config capability. Server startup reports policy errors through the existing configuration/startup error handling.

## Authentication

- Use the accepted Go SDK auth integration with a strict mdchk TokenVerifier. For the initial remote profile support signed OAuth JWT access tokens conforming to RFC 9068, using RS256 or ES256 and trusted public keys. Pin github.com/golang-jwt/jwt/v5 v5.3.1 for signature/claim verification; it is already present in the selected SDK module graph. Inventory its license/notices in the dependency review.
- Accept one Authorization: Bearer token header only. Reject duplicate/ambiguous Authorization fields. Do not accept credentials in query parameters, cookies, request JSON or forwarded identity headers. No static shared API keys or ID-token substitution in the initial remote profile.
- Require typ at+jwt or application/at+jwt, a valid supported signature and matching configured issuer. aud must identify the exact configured public_url, either as a string or one member of a valid string array. Require the RFC 9068 claims iss, exp, aud, sub, client_id, iat and jti with their proper types; validate nbf when present. Scope is a space-delimited string.
- Require unique nonempty key IDs in the configured JWKS and a matching token kid. Validate key type/use/algorithm, accept only public signing keys, require RSA 2048 bits or more or ES256 P-256. Reject none, symmetric algorithms, unsupported critical headers, unsigned/encrypted tokens and token-supplied key material/URLs. Reject duplicate JSON names, invalid UTF-8 and unpaired surrogates in JOSE/claims before a lossy decoder.
- Use 30 seconds clock tolerance for exp/nbf/future iat, and cap exp-iat at one hour. This profile has no per-token revocation/introspection store: a valid token remains usable until expiry unless its signing key is removed and the service restarted. Token expiry during an already admitted call does not retroactively cancel that call; every new HTTP request is checked again.
- Bound a bearer token to 16 KiB and the provisioned JWKS to 1 MiB/64 keys, with bounded JSON parsing. These are finite operator security limits; support coherent overrides through --limit rather than document config. No request-time network fetch is needed for token verification.
- The authorization server handles login/consent/client registration/token issuance. mdchk is the protected resource, not an authorization server. Publish RFC 9728 metadata at the derived /.well-known/oauth-protected-resource path including the external resource URL, configured issuer and minimum base scopes; use WWW-Authenticate resource_metadata and scope challenges.
- Metadata GET/OPTIONS is public, contains only configured public resource/issuer/scope data and may use wildcard CORS as permitted for public OAuth metadata. It never includes root paths, effective document config, credentials or private key material. This exception does not apply to /mcp.

## Authorization

### Standalone

Require mdchk:run for discovery and both tools. Calls transform only the supplied isolated request
data, with no document filesystem access.

### Project read

Require mdchk:read for discovery, lint and fix with dry_run=true. The preview may read all resources
permitted by the startup root and shared policy.

### Project apply

Require BOTH mdchk:read and mdchk:write for fix with dry_run=false or omitted. A token with write
alone does not imply read. Check this after strict tool argument validation and before
configuration/resource reads, edit planning or writes.

### Root binding

Each project process has one startup root and one public resource audience. Issuing tokens for that
audience with these scopes explicitly grants the corresponding root-wide operations. There are no
per-file user ACLs or request-selected roots. Use separate instances/public resource URLs for
different project access boundaries.

### Discovery

Keep lint/fix definitions stable for the configured mode, including for read-only project callers;
the fix description states the required write scope for apply. Annotations and discovery visibility
do not authorize execution.

### Scopes

Match scope tokens exactly, with no inferred hierarchies or automatic additional grants. Keep root
confinement, symlink refusal, inherited policy restrictions and safe-write checks even after
authentication. Remote scope grants cannot relax them.

### Token isolation

An issuer/audience mismatch never executes a tool. Do not forward tokens to links, resources or
unrelated services. Normal logs omit raw tokens, token claims, subject IDs, file paths,
document/config content and validation excerpts.

## Host origin and cors

- Validate Host/HTTP2 :authority against an explicit authority set before tool processing. Local allows the exact configured loopback address and localhost at the listener port; remote allows only public_url authority. Canonicalize ASCII host case, bracketed IP spelling and default ports; reject malformed authorities, trailing-dot aliases, userinfo, duplicate Host fields and unexpected ports. Never resolve request Host via DNS.
- Validate every present Origin on /mcp, including on requests carrying valid tokens. Require one syntactically valid serialized origin with scheme/host/port, no path/query/fragment/userinfo, and an exact match to the operator allowlist after canonical origin serialization. Reject null, multiple origins, suffix matches and wildcards. Missing Origin remains valid for native clients.
- Do not treat a Sec-Fetch-Site cross-site request with missing/unapproved Origin as a native client. Reject it. A deliberately allowlisted browser origin remains eligible with its bearer token.
- For an allowed browser origin only, return its exact Access-Control-Allow-Origin and Vary: Origin. Preflight OPTIONS validates Host, Origin and requested method/headers before responding, and need not carry a token. Permit POST and only the required Content-Type, Authorization, Accept, MCP-Protocol-Version, Mcp-Method and Mcp-Name headers. Expose WWW-Authenticate and relevant MCP response headers. Do not enable cookie credentials or wildcard CORS on /mcp.
- CORS affects browsers only and never substitutes for OAuth authorization. Reject invalid Host/Origin with 403 without echoing attacker-controlled field values in the body or logs.

## Http and protocol

- Retain the accepted stateless /mcp endpoint and two protocol revisions. POST carries one complete JSON-RPC request/notification object. No batches, compressed request bodies or content encodings except absent/identity; responses are uncompressed JSON. Require application/json with UTF-8 and the protocol Accept requirements.
- GET and DELETE /mcp return 405 with Allow; OPTIONS is only the bounded CORS preflight. No redirect or trailing-slash alternate MCP endpoint. Public resource metadata is the separate safe discovery endpoint. Do not serve project files, an arbitrary health/debug endpoint, or a custom lint REST API.
- No MCP session IDs are created or trusted. Reject unexpected MCP-Session-Id and Last-Event-ID headers as unsupported instead of reviving a session/replay path. Disable SDK compatibility switches that restore sessions; fail startup for conflicting enabled MCPGODEBUG behavior.
- For 2026-07-28 require MCP-Protocol-Version, Mcp-Method and applicable Mcp-Name, comparing decoded values to raw-validated JSON body metadata before dispatch. Keep the standard HeaderMismatch/UnsupportedProtocolVersion responses. Header values do not independently authorize a tool.
- For 2025-11-25 allow a headerless initialize carrying its protocolVersion; subsequent requests must identify 2025-11-25 in MCP-Protocol-Version. Do not silently infer unsupported 2025-03-26. Preserve valid initialization/version fallback rules and do not impose new-revision header requirements on legacy requests.
- Enable SDK PropagateRequestCancellation for 2026-07-28 so the request context reaches queueing/analysis/planning and cancellation stops new commits. When the connection is gone no delivery is promised, and a replacement already in progress may finish.
- Legacy stateless HTTP has a compatibility limitation: an out-of-band cancellation POST has no persistent session identifying the original call and may be ignored as unknown. Do not implement a global request-ID lookup that can cancel another caller. A legacy caller must not infer cancellation or no writes from disconnect; work can run to the finite operation deadline. Use the current protocol or stdio when prompt cancellation propagation is required.
- This makes the generic cancellation policy revision-specific rather than claiming a guarantee the stateless legacy SDK cannot supply. HTTP tests must cover both behavior paths and the absence of unauthorized cross-call cancellation.
- On shutdown stop admitting calls, cancel queued/active operation contexts and use the accepted reporting/cleanup allowance; finish/report in-flight replacement outcomes where possible. Do not add a hidden drain window that starts more writes. Forced process termination has the existing no-report guarantee.

## Transport limits

### Http header bytes

#### Default

32768

#### Unit

bytes

#### Meaning

Aggregate request header bound, enforced with server HTTP parsing; smaller bearer-token bound
applies separately.

### Http header time

#### Default

5

#### Unit

seconds

#### Meaning

Deadline to receive headers; includes a bounded TLS handshake for direct HTTPS.

### Http body time

#### Default

30

#### Unit

seconds

#### Meaning

Absolute deadline to receive the full bounded JSON body after headers, not an idle timer reset by
slow trickles.

### Http idle time

#### Default

60

#### Unit

seconds

#### Meaning

Idle keep-alive connection timeout, not the analysis deadline.

### Http write time

#### Default

15

#### Unit

seconds

#### Meaning

Deadline for delivering a fully constructed bounded response, beginning when delivery starts; do not
use a shorter whole-handler WriteTimeout that hides accepted operation/reporting time.

### Http connections

#### Default

64

#### Unit

connections

#### Meaning

Server-wide accepted connections including idle/handshake connections; bound HTTP2 streams in
addition to TCP connections.

### Http inflight requests

#### Default

16

#### Unit

requests

#### Meaning

Global HTTP request admission bound across connections and HTTP2 streams; retain capacity for small
protocol/control requests apart from four active and eight queued tool calls.

### Http body readers

#### Default

4

#### Unit

requests

#### Meaning

Concurrent ordinary body readers; reserve a bounded control path so large uploads cannot consume all
cancellation/discovery handling capacity. Hints cannot bypass body-size or execution admission
checks.

### Http control bytes

#### Default

65536

#### Unit

bytes

#### Meaning

Maximum request body admitted to the reserved control path. A tools/call never runs in that path; a
misleading header is rejected before dispatch.

### Auth token bytes

#### Default

16384

#### Unit

bytes

#### Meaning

Encoded bearer token bound, within HTTP header size.

### Auth jwks bytes

#### Default

1048576

#### Unit

bytes

#### Meaning

Startup public signing-key file bound.

### Auth jwks keys

#### Default

64

#### Unit

keys

#### Meaning

Startup trusted public signing keys.

### Auth token lifetime

#### Default

3600

#### Unit

seconds

#### Meaning

Maximum exp-iat for accepted JWT access tokens.

### Auth clock skew

#### Default

30

#### Unit

seconds

#### Meaning

Tolerance for temporal JWT claim verification.

## Limit integration

- These add transport/authentication bounds to the accepted limits; they do not replace 16 MiB raw request, 8 MiB serialized response, 4 active calls, 8 queued calls, 10-second queue wait, 60-second operation and 2-second reporting allowances. The operation clock begins once a bounded message is received for processing, including validation and queueing.
- Allow finite coherent operator --limit overrides using the accepted parser; never accept limit/authentication overrides in project configuration or tool arguments. Report applicable effective limits without publishing issuer keys, credentials or private paths. Treat these as initial defaults requiring load tests, not a memory/throughput SLA.
- Cap global requests and reserved control slots even for HTTP2. Do not let SDK transport goroutines or proxy buffering create an unbounded secondary queue. Implementation must verify cancellation responsiveness under saturated upload/tool traffic.
- Connection/admission exhaustion before an operation starts returns 503 with a short Retry-After when a response is possible, without starting work. Do not transparently retry a project fix; an explicit pre-admission refusal is distinguishable from an uncertain lost response.
- For a complete, authenticated, known-tool call admitted to the operation queue, queue expiry/runtime limits use the accepted structured incomplete result and interruption records when deliverable. Output-size reservation and all mandatory write outcomes remain unchanged.

## Response mapping

### 400

Malformed request/framing or required MCP header mismatch/version error; include the correct
JSON-RPC error where a trustworthy envelope/id is available. Never echo rejected payload excerpts.

### 401

Missing/invalid/expired token with Bearer challenge and resource_metadata; invalid tokens get
invalid_token as appropriate.

### 403

Insufficient scope with the full minimum scope challenge, or invalid Host/Origin/socket peer without
an authentication challenge.

### 404

Wrong endpoint; for 2026-07-28 unknown RPC methods use the standard method-not-found JSON-RPC
response with HTTP 404.

### 405

Unsupported endpoint method with Allow.

### 408

Network header/body deadline when the HTTP parser can still send a response; otherwise close the
connection.

### 413

Request body exceeds the configured raw body bound, before tool execution.

### 415

Unsupported media type/charset/content encoding.

### 431

Header block exceeds its bound when supported by the HTTP parser; otherwise close the connection.

### 503

Pre-operation capacity/refused admission or shutting down; no new operation begins.

### 200

Delivered tool result, including domain usage/configuration/io/internal errors and incomplete
results, using the accepted isError and result semantics. Valid notifications use the
revision-correct 202 empty response instead.

### Delivery failure

A lost/truncated network response is not a valid result and does not imply no writes. Do not
generate a success response after a write failure or automatically replay the request.

## Verification

Source review and contract consistency checks only; no HTTP/authentication implementation exists in
mdchk and no production deployment tests are claimed.

- Implement the strict operator-policy schema/startup checks, Host/Origin/CORS middleware, bearer verifier and base/per-operation scope checks around the SDK. Verify no filesystem access occurs before applicable authorization.
- Audit SDK defaults: explicit 16 MiB request limit, stateless JSON responses, current-revision cancellation propagation, explicit Host protection, content-free errors/logging and no legacy session switches.
- Use pinned local issuer/client fixtures to verify OAuth metadata discovery/challenges, both signature algorithms, wrong issuer/audience/type/key/scopes, expiry/clock tolerance, malformed JOSE/claims and key rotation by restart. Do not claim production identity-provider interoperability until tested.
- Run the required HTTP acceptance matrix with both modes and both revisions, actual direct TLS and a real loopback reverse proxy, pinned independent clients, partial-write/cancellation faults, hostile Origin/Host/proxy headers, slow uploads/readers and saturated queues. Include race/vet/appropriate integration checks.
- Document the trusted-local assumption, initial RFC 9068 JWT/public-key provisioning boundary, legacy HTTP cancellation limitation, exact proxy configuration, safe token/key handling and per-project endpoint/scopes. Keep setup docs within existing issues without adding release artifacts or installation scope.

The [acceptance matrix](../contracts/http/acceptance-cases.json) defines required tests;
it is not a record of executed deployment tests.

## Operator policy examples

See the [local, direct TLS, and proxy TLS policy examples](../contracts/http/operator-policy-examples.json).
Relative certificate/key/JWKS files resolve beside the policy file.
Remote deployments require provisioned issuer keys and the scopes above before tools can run.

## Pinned references

- https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http
- https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization
- https://modelcontextprotocol.io/specification/2025-11-25/basic/transports
- https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/cancellation
- https://www.rfc-editor.org/rfc/rfc9068.html
- https://www.rfc-editor.org/rfc/rfc9728.html
- https://github.com/modelcontextprotocol/go-sdk/blob/3f3b699b2b67e1ed033a63d6651671dab53c2d32/auth/auth.go
- https://github.com/modelcontextprotocol/go-sdk/blob/3f3b699b2b67e1ed033a63d6651671dab53c2d32/mcp/streamable.go
