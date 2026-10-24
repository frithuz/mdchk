# Project and logical-resource contract

Status: approved on 2026-10-14. This defines resource inputs, policies, snapshots,
and outcomes for [#170](https://github.com/frithuz/mdchk/issues/170). The shared
engine contract (#123) is delivered. Issue #249 adds the typed read-only request,
observation and immutable view contracts with result/coverage fixtures in
[internal/resource](../../internal/resource/README.md). Production adapters,
acquisition/cache, anchor indexes and consumers remain pending; these
types do not advertise resource lookup as implemented. Issue #170 adds the shared
strict supplied-inventory input boundary and embeddable schema; inventory lookup
and MCP argument integration remain pending. Issue #250 adds shared portable
destination normalization, independent fragment facts and a one-level index plan;
it performs no resource acquisition or production consumer registration.

Resource lookup is read-only, confined, and shared by CLI, inspect, project MCP,
and standalone consumers. Lint selection is separate from resource permission.
Only established absence becomes a missing-target finding; unknown or blocked
context is incomplete coverage. A standalone inventory is partial unless the
caller explicitly declares it complete.

One configuration owns `resources.index_file` and `resources.anchor_style`.
One operation owns immutable resource observations and coordinated candidate/saved
fix views. No resource lookup performs network requests or registers a write.

## Roots and access boundaries

Keep resource roots distinct from report-path bases, exclusion bases, target
selection, and configuration-discovery limits. Resources use root-relative logical
keys; reporting still follows the existing CLI/MCP path conventions.

| Operation | Resource root |
| --- | --- |
| CLI lint/fix inside a repository | Nearest enclosing repository root discovered from the working directory, including worktree `.git` files |
| CLI lint/fix outside a repository | Selected config's directory if it contains the working directory; otherwise the working directory |
| Inspect | Apply the same rule from the resolved target's parent rather than the caller's working directory |
| Project MCP | Validated startup `--root`, even if it is narrower than a repository |
| Standalone | A virtual request root; no host path, configuration discovery, or filesystem fallback |

A selected configuration file located below or beside the invocation directory
does not enlarge the resource root. Merely searching ancestors for configuration does not
make the filesystem root a resource root. If the user deliberately runs from
the filesystem root or explicitly chooses it as a server root, that is a separate
explicit operation scope.

Existing explicit CLI document arguments remain eligible even outside the resource
root. Analyze their supplied snapshots and same-document anchors normally, but
mark cross-file resource checks unavailable for those sources. Do not enlarge the
root to their common ancestor or discover a new config per document. Running from
the appropriate project directory establishes the intended cross-file context.
Resource scope does not expand merely because an explicit target was supplied.

An excluded or unselected file inside the root can be a resource. Reading it for
existence or anchors does not lint it, apply its directives to the source document,
make it a fix target, or return its content in MCP output. Even excluded submitted
standalone documents remain available as resource snapshots. The existing
all-targets-excluded usage error still applies.

Apply operation-level flavor/frontmatter settings to referenced Markdown. Do not
discover nested configuration or execute its rules. Parsing a resource must not
recursively lint its outgoing links; direct fragment lookup needs only that target's
own parse and anchors. This keeps cycles and dependency expansion bounded.

Initially reject encountered symlinks in every resource path component, including
dangling links and links that stay inside the root. Reject non-regular resources
when a file is required; directory handling is explicit. Skip `.git` components.
Inspect's existing explicit-target symlink exception remains limited to acquiring
its selected target; it does not permit following arbitrary linked resources.

Use a read-only adapter with traversal-resistant operations, not a generic `fs.FS`
assumed to be confined and not `EvalSymlinks` followed by an unrestricted open.
`os.Root` is the preferred standard-library foundation. It confines traversal but
allows contained symlinks and does not itself enforce all of these policies.
Component/type checks and stable handle/identity acquisition remain necessary;
ordinary string-prefix checks are insufficient. Require deterministic race,
symlink-substitution, special-file, and cancellation tests in #171 before claiming
the adapter enforces this contract. Do not treat a preliminary `Lstat` as proof
against later substitution. [Go's traversal-resistant API guidance](https://go.dev/blog/osroot),
[Root contract](https://pkg.go.dev/os@go1.27.2#Root).

## Logical names and URL resolution

Logical names are case-sensitive UTF-8 with `/` separators. Do not case-fold or
Unicode-normalize them. `Guide.md` and `guide.md`, or distinct Unicode normalization
forms, are distinct keys. The project adapter must compare actual directory-entry
spellings rather than accept a case-insensitive host alias. Native `Stat` success
alone cannot enforce portable resource identity on a case-insensitive filesystem.

Submitted file/resource keys must already be canonical: nonempty and relative,
with no empty, `.` or `..` segments, leading/trailing slash, backslash, drive-qualified
prefix, NUL/control characters, or invalid Unicode scalar values. Keys are literal
names, not URLs: `a%20b.md` names percent characters, while `a b.md` names a space.
Do not percent-decode map keys. Use `.` only internally for the virtual root, never
as a submitted file entry.

For a link, begin with the parser's semantic destination after Markdown escape and
entity processing, while retaining the original source range. Then:

1. Classify schemes/authority and native path forms before decoding. Ignore remote,
   protocol-relative, data, mail, and other non-file schemes without network calls.
   Treat `file:` URLs, drive paths, UNC/backslash forms, and leading `/` paths as
   unsupported/blocked local references. In this initial contract `/guide.md` is
   neither a host-root file nor silently a project-root URL. #174 may later supply
   an explicit site/docs-root interpretation for an integration.
2. Separate raw path, query, and fragment. Discard query for local resource lookup.
   Decode path and fragment independently once, using path-unescape semantics;
   `+` remains `+`. Do not unescape `url.URL.Path` again after `url.Parse` has
   already decoded it. Invalid path percent escapes/UTF-8 are invalid-reference
   outcomes; invalid fragment escapes/UTF-8 follow MD051's unresolved-fragment
   contract. A malformed fragment must not prevent an independent file-existence
   check of an otherwise valid path.
3. Reject encoded path separators `%2F` and `%5C` rather than changing hierarchy.
   Decode other path characters once, then process `.` and `..` with an explicit
   segment stack. Permit parent references inside the root; reject any attempt to
   pop above it. Normalize repeated separators in relative URL paths. Preserve a
   directory hint for a trailing slash or final `.`/`..`.
4. Resolve relative to the source document's directory, keeping the logical key
   and original destination distinct. Empty paths, query-only references, and a
   normalized explicit path back to the source reuse its snapshot. Empty fragments
   refer to the document itself and do not require a named anchor.
5. Enforce root/name/type policy before lookup. Translate valid logical keys to
   native paths only inside the project adapter. Reject platform-unsupported
   names as unavailable/blocked rather than treating Windows device names, alternate
   streams, trailing-dot aliases, or other special paths as ordinary files. A
   standalone logical map does not inherit the server OS's filename aliases.

RFC 3986 distinguishes reserved delimiters from encoded data and warns against
repeated decoding. Our encoded-separator rejection and root-relative-path policy
are explicit mdchk choices, not claims that the RFC mandates them.
[RFC 3986 §§2.2–2.4](https://www.rfc-editor.org/rfc/rfc3986#section-2.4),
[Go PathUnescape](https://pkg.go.dev/net/url@go1.27.2#PathUnescape),
[Go Localize](https://pkg.go.dev/path/filepath@go1.27.2#Localize).

Representative results from source `docs/guide.md`:

| Destination | Interpretation |
| --- | --- |
| `../README.md` | Root-relative key `README.md` |
| `%2e%2e/README.md` | Same result after one decode |
| `../../secret.md` | Blocked root escape |
| `%252e%252e/a.md` | Literal key `docs/%2e%2e/a.md`; never decode a second time |
| `a+b.md` | Literal plus in `docs/a+b.md` |
| `a%23b.md#part` | File `docs/a#b.md`, fragment `part` |
| `a%2Fb.md` | Blocked encoded separator |
| `#%2520` | Same document, literal fragment `%20` |
| `guide.md#part` | Same-document check even with MD051 cross-file checks disabled |
| `folder/` | Resolve the configured index within `docs/folder` |

## Standalone resource map

Add optional request argument `resources`, separate from `config.resources` policy.
This is supplied context for `lint`/`fix`, not MCP's separate Resources protocol.
Its structure is:

```json
{
  "complete": false,
  "entries": [
    {"path": "docs/guide.md", "kind": "file", "content": "# Install\n"},
    {"path": "assets/logo.png", "kind": "file"},
    {"path": "empty-directory", "kind": "directory"},
    {"path": "gone.md", "kind": "missing"},
    {"path": "private", "kind": "unavailable"}
  ]
}
```

Require `entries` when the object is present; `complete` defaults to false. Reject
unknown fields and wrong types. `content` is optional only for `kind: file` and
is an exact UTF-8 string; an empty string supplies a real empty file. No binary
payload is necessary for image-existence checks. Missing `content` proves file
existence but cannot supply Markdown anchors or metadata parsing.

Seed this namespace from every submitted `files` entry, including excluded ones.
Reject overlap between `files` and `resources.entries`, even if content matches;
there must be one authoritative supplied version. Reject duplicate entry paths,
duplicate JSON object names, and contradictory ancestor relationships. A file,
missing path, or unavailable subtree cannot also have declared descendants.
Positive file/directory entries imply their parent directories; absence assertions
do not prove that their parents exist. Explicit directory entries can represent directories without supplying children;
in a partial map, that does not prove the directory is empty. The virtual root
always exists.

Map lookup order: known entry, known blocking/absent ancestor, then completeness.
An omitted key is unknown for a partial map and absent for a complete map. Explicit
`unavailable` entries describe unavailable subtrees even in a complete inventory;
their descendants remain unknown/unavailable. Explicit `missing` entries establish
absence in a partial map too. `complete: true` asserts a full name/type inventory,
not that every file's content is supplied. It covers all resources under the
virtual root, not only lint targets or Markdown files.

An omitted `resources` object is equivalent to a partial map seeded only by `files`.
This preserves existing standalone request shapes without falsely asserting project
completeness. Emit coverage only for checks that actually need unresolved context,
not merely because the map is partial. A check with no required resource lookups incurs no resource coverage deficit. Never fall back to server files, a project-mode cache, or the network.

Validate raw JSON UTF-8, Unicode escape pairing, and duplicate names before ordinary
decoding loses information. `encoding/json` accepts duplicate names and replaces invalid UTF-8 or unpaired
surrogate escapes with U+FFFD.
A valid literal U+FFFD is allowed; do not reject it after decoding as a substitute
for inspecting the original input. #189 must ensure its MCP adapter preserves the
raw argument validation opportunity. [Go JSON decoding behavior](https://pkg.go.dev/encoding/json@go1.27.2#Unmarshal).

## Outcomes and rule ownership

Existence and content availability are separate capabilities. Preserve known
existence even when content is missing or cannot be decoded. One cached
content failure must not erase a successful existence check.

| Outcome | Rule/result treatment |
| --- | --- |
| Found regular file | MD057 can complete; load content only when a consumer needs it |
| Found directory | Resolve exactly one configured index filename; no README/index.html/extension guessing |
| Authoritative absence | MD057/eligible navigation consumer reports a finding |
| Unknown omitted entry in partial map | Resource-unavailable coverage; not a missing-target finding |
| Explicit unavailable subtree, root escape, denied symlink, Git metadata, unsupported path/type | Coverage deficit; do not assert absence |
| Known file but content not supplied | Existence check can pass; fragment/content-dependent check is incomplete |
| Actual filesystem read/decode failure | Attributed `io` error plus affected coverage; preserve unrelated findings |
| Required parsed structure uncertain | Affected anchor/content queries are incomplete; do not infer absent IDs |
| Invalid supplied schema, duplicate keys, conflicting map entries, malformed request Unicode | Reject before analysis through usage/protocol validation; no writes |
| Acquisition/execution budget exceeded or cancelled | Explicit interruption and affected incomplete coverage; no invented absence or truncated-success result |

MD051 checks nonempty fragments only when the destination document exists and its
anchor interpretation is available. Known missing documents belong to MD057;
the fragment check is inapplicable after that established absence, not a second
missing-file finding or an unknown-context deficit. Disabling MD057 deliberately
disables missing-file diagnostics; MD051 does not silently take over that rule.
The [MD051](../rules/links-and-images/md051.md) and
[MD057](../rules/links-and-images/md057.md) contracts share this ownership. Unknown existence is different:
MD051 cannot establish applicability and reports reduced coverage when requested.

MD051's existing `cross_file: false` default skips distinct-document fragments.
Equivalent paths to the current document still use same-document checking. MD057
continues independently. Remote links are outside these checks. MD057's existing
`check_images` option remains independent of fragment checks on links.

For implicit resource discovery, parse `.md`/`.markdown` case-insensitively by
extension, without changing key case. An explicitly selected document remains
Markdown regardless of extension. Other file types can satisfy existence checks;
their fragment semantics are unavailable in the initial contract. Do not invent
HTML/PDF/SVG anchors by treating arbitrary supplied text as Markdown. #174 can add
explicit integration formats separately. Resource-only Markdown uses the operation's
flavor/frontmatter settings, and the same parser/source-aware anchor service.

Use the established error precedence: operational error, incomplete coverage,
violations, clean. Unknown/blocked resources normally produce exit 15; actual I/O
failures produce exit 13 and retain coverage. Request/configuration errors keep
their existing categories/codes. Known oversized or malformed input detected in preflight is usage (11). Runtime
quota exhaustion, deadlines and cancellation are explicit interruptions with
incomplete status (15), unless internal or I/O failure takes precedence. The
[results](results.md), [limits](limits.md) and [MCP](mcp.md) contracts define delivery.
Locate coverage at requesting source links and deduplicate underlying resource I/O
errors by resource identity. Resource-only paths do not enter `checked_files`.

## Shared settings and anchor consistency

Configuration once the capability is implemented:

```yaml
version: 1
resources:
  index_file: index.md
  anchor_style: github
rules:
  MD051:
    cross_file: true
```

`index_file` is one nonempty filename, not a path, glob, URL, `.` or `..`; reject
separators, query/fragment syntax, drive prefixes, and control characters. Permit
alternatives such as `README.md`. It must be representable by the active project
adapter; no directory scan chooses a fallback filename. A directory with unknown
index availability remains unknown; a known directory alone does not establish
that its index is absent.

A trailing slash requires a directory; a known regular file at that name cannot
satisfy it. Likewise an index entry that is itself a directory cannot satisfy the
required index file. These are established target-type mismatches for MD057,
not permission failures. Resolve only one index level; never recurse through
`index.md/index.md` or scan alternate names.

The shared settings replace MD057's `index_file` and MD051/MD080's
`anchor_style` options. The old per-rule spellings are unknown options, including
on disabled rules; no aliases or precedence rules are defined. This approved
pre-implementation revision retains schema version 1 and all existing defaults. Explicitly
requested unavailable capabilities must still fail configuration validation;
unmentioned unavailable default rules retain the approved incomplete behavior.

`github` and `explicit` keep the existing defined algorithms, explicit-heading-ID
precedence, once-only fragment decoding, and case-sensitive ID comparison. All
anchor consumers receive the shared policy rather than another rule's mutable
options. MD080 still compares unsuffixed heading bases; MD073 still includes only
eligible headings, not every arbitrary ID-bearing node. Duplicate suffix allocation
must respect occupied explicit targets deterministically under #172's contract.

MD074 consumes the shared index policy.
Its configured docs root is a subroot of authorized resource access. The [MkDocs integration](integrations.md#mkdocs-navigation)
defines nav URLs, absolute-path handling and omitted-file inventories; generic lookup does not infer MkDocs plugins,
rewrites, `.html`→`.md` conversion, or arbitrary YAML execution. MkDocs' own link
validation depends on its configuration, reinforcing the need for an explicit
integration contract. [MkDocs configuration reference](https://www.mkdocs.org/user-guide/configuration/#validation).

## Snapshots, caching, fixes, and limits

Resolve configuration once per operation. Acquire target snapshots before running
rules. Lazily acquire requested resource observations and content, validating stable
identity/type and original bytes; store each result for the rest of the operation.
Cache successes, authoritative misses, blocked/unknown outcomes, read failures,
and acquired content separately as needed. Use bounded acquisition retry and fail
on continued instability. An open root handle is not a filesystem snapshot: external updates may change
contents read through the same handle.

This is a first-observation, per-resource snapshot, not a claim that every file
existed at one simultaneous instant. A later appearance of a previously missing
resource does not retroactively change the request's cached observation. New MCP
calls reacquire configuration and resources; no mutable content, misses, or settings
are shared across calls. Immutable grammar/catalog data can be shared.

Cache parsed documents/anchor indexes by source digest, parser/profile revision,
frontmatter interpretation, and shared anchor policy. Path alone is insufficient.
Keep original and candidate snapshots separate; use one acquisition/result for
concurrent duplicate lookups. Resource reading cannot register a write operation.

Fix phases:

1. Collect safe per-target proposals against the original snapshots. Do not run
   each target's final cross-file checks against a mixture of old/new neighbors.
2. Build one candidate overlay: selected documents with valid candidates replace
   their original versions; unchanged targets and resource-only entries retain
   originals. Reparse changed documents and run post-fix analysis against this
   overlay. A preview describes this state.
3. Standalone fix returns that candidate state for successfully processed targets;
   it does not write resource-only entries or return their content as corrected
   files. Missing context defers only edits whose safety depends on it, not every
   independently safe style correction.
4. Project/CLI writes use #146's stale-source/write preconditions, including any
   resource snapshots used as edit-safety dependencies. Detecting changed inputs
   invalidates dependent proposals. This contract does not claim to solve
   the remaining platform check-to-write race or add whole-project transactions.
5. Build the final result view from successfully saved candidates and confirmed
   unchanged originals, then rerun relevant cross-file checks. If a failed write
   was refused because external content changed, that document is unavailable
   until a fresh stable snapshot is acquired; never pretend the old or candidate
   content is saved. Earlier successful writes remain reported. Errors dominate
   a clean candidate's status, as required by #148.

Do not merge target snapshots by inode alone: hard links may share initial bytes
but a sibling-file replacement changes only one directory entry. Keep logical
target identities and write outcomes distinct. Do not claim protection from
privileged mount changes or an atomic multi-file filesystem view. #146 remains
the owner of write guarantees.

Budgets cover supplied files and resource entries, total/per-resource bytes,
distinct lookups (including misses), path depth/length, parsed content/AST work,
directory enumeration, acquisition retries, concurrency, wall time, and response
bytes. Existence-only image checks need no binary loading. Repeated cache hits
must not trigger new I/O, but request/entry byte limits still count all supplied
data, including excluded files. Neither config nor inline directives can raise
server limits. The [shared limits](limits.md) set 1 MiB per text, 8 MiB aggregate content and
response, and 16 MiB raw MCP requests. The [MCP](mcp.md) and [HTTP](http.md) contracts
define protocol-specific cancellation, including the legacy HTTP limitation.

Preflight known request limits before analysis/writes. If acquisition hits a limit,
record an explicit runtime interruption and affected incomplete coverage. Do not mark unvisited names
missing or an interrupted directory enumeration complete. Resource-only content
must not bloat fix responses; never return a partial corrected string as a full
document. A navigation omitted-file check requires an exhaustive authorized
inventory or an explicit incomplete outcome, even if all listed links were found.

## Verification and handoff

The URL, map, configuration, and outcome examples above specify expected behavior.
Production work requires the following independent checks:

| Area | Required cases |
| --- | --- |
| Roots | Repository/worktree/no-repository discovery, config below/above cwd, explicit targets outside resource scope, inspect canonical target, narrow MCP root, standalone isolation |
| Identity | Exact case, Unicode spellings, literal percent names, duplicate JSON/entry names, file-directory ancestor conflicts, native unsupported names |
| URL parsing | Single decoding, plus signs, encoded delimiters, encoded traversal, queries/fragments, empty/self paths, malformed escapes, invalid UTF-8, schemes/drive/UNC forms |
| Access | Excluded/unselected resources, every-component symlinks, dangling links, root escapes, read denial, disappearing files, non-regular files, injected races, no network/no writes |
| Maps | Partial/complete omission, explicit missing/unavailable subtrees, inferred parents, empty directories/content, existence-only images, overlap with targets |
| Consumers | MD057/MD051 ownership, same-file normalization, cross-file off, Markdown typing, common index/style, explicit IDs, missing/uncertain anchor data |
| Fix/results | Candidate overlay, partial writes, stale dependency, hard-link identity, resource-only preservation, request cache isolation, actual/proposed/returned distinctions |
| Limits | Distinct miss budget, cache-hit behavior, per-file/total bytes, cancellation in acquisition, partial enumeration, oversized result without false completeness |

Assert equivalent results for equivalent CLI/project-MCP/supplied-map snapshots,
without assuming equivalent host filesystem aliases. Run native Linux/macOS/Windows
tests before portability claims. Fuzz normalization
and schema validation, inject acquisition/write races deterministically, and verify
both absence and content-availability state transitions. #123 owns engine contracts;
#171 owns the confined adapter; #172 owns shared anchors; #173 integrates consumers.
Coordinate fix overlays with #143/#146/#148 and standalone schema with #189/#197.
