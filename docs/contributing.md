# Contributing

Follow the commit message conventions in [AGENTS.md](../AGENTS.md). Commit
messages must not contain `Co-Authored-By` trailers.

## Build and test the Go foundation

Install [Go 1.27.2 or newer](https://go.dev/dl/). Go 1.27.2 is the supported
minimum, recorded in `go.mod`; CI reads that file and tests the exact minimum.
The bootstrap uses a current supported Go release with its security fixes; see
the [Go release history and support policy](https://go.dev/doc/devel/release).

From a clean checkout, run:

```sh
go build ./...
go test ./...
go vet ./...
```

On platforms that support Go's race detector, also run:

```sh
go test -race ./...
```

The module is `github.com/frithuz/mdchk`. Configuration loading uses the pinned
`github.com/goccy/go-yaml v1.19.2` dependency recorded in `go.mod` and `go.sum`.
A fresh module cache needs network access to retrieve and verify it first:

```sh
go mod download
```

With the supported toolchain installed and that cache populated, verify without
automatic toolchain downloads, workspace overrides, network access or module-file
edits:

```sh
GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly go test ./...
GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly go vet ./...
```

The [Go workflow](../.github/workflows/go.yml) runs test, vet, and the CommonMark
corpus checks below on pushes and pull requests. Its actions are pinned to commit
IDs. Go package builds include the bootstrap CLI. Build and run its metadata
commands:

```sh
go build -o mdchk ./cmd/mdchk
./mdchk --help
./mdchk lint --help
./mdchk --version
```

The version is `dev` for local source builds and uses Go module build metadata
when a version is available. Help and version do not access the project. Lint
parses arguments and loads and admits configuration before target access;
configuration failures return `12`, including unavailable default GFM when no
configuration exists. A valid CommonMark configuration completes preparation, selects targets and acquires immutable
source snapshots. The mandatory UTF-8 gate reports malformed source as I/O (`13`),
independently of rule enablement; successful empty selection returns `11`. Acquired
snapshots enter the shared parser and available registered checks. Missing
enabled default implementations produce coverage deficits and return `15`;
valid UTF-8 with all ordinary rules disabled can return clean `0`. Acquisition
uses document/aggregate byte limits and shares its work/time budget and caller
context with the engine. The executable propagates interrupts through
`cli.RunContext`. Lint emits text by default or one schema-v1 JSON result on
stdout, including ordinary usage/configuration failures. Reporting bounds actual
encoded bytes and uses an independent cleanup deadline; stderr carries execution
logs. VS Code/GitHub reporters and other commands remain pending;
see [current implementation](index.md#current-implementation).
The conformance adapter below remains a separate test command.

## Exercise a newly registered rule through the CLI

Start with the [disposable lint project](commands/lint.md#run-a-disposable-project)
to build and exercise the current production executable. The repository includes
compiled-process acceptance tests with exact operating-system exit statuses:

```sh
go test ./cmd/mdchk -run 'Test(Configuration|Lint)' -count=1 -v
```

`configuration_acceptance_test.go` supplies the bounded build/process helpers,
full-catalog `cleanConfiguration` and before/after fixture byte checks.
`workflow_acceptance_test.go` covers successful target forms, option placement,
metadata bypass, validated callback options and a genuinely permission-denied
input alongside a readable finding. The permission case probes the operating
system and explicitly skips when the user/platform can still read mode-000 files.
The configuration suites cover strict validation, discovery, policy, both
reporters and retained findings under incomplete, I/O and internal outcomes.
JSON checks validate linked records and original-byte identities. For the full
JSON Schema and semantic validator, install the development validator described
in [result contracts](contracts/results/README.md) and set
`MDCHK_RESULT_VALIDATOR_PYTHON` to that environment's Python when running tests.

For a synthetic callback integration test, reuse
`buildConfigurationFixtureExecutable`, `fixtureConfiguration` and
`runConfiguration` from `cmd/mdchk` tests. The build helper writes a temporary Go
`-overlay` replacing only `internal/cli/providers.go` with
`cmd/mdchk/testdata/configuration_providers.go`. That file registers explicit
MD001 and MD003 synthetic callbacks against all 89 catalog entries; a normal
build excludes it. These probes demonstrate findings, failure and option delivery,
and do not implement those rules' Markdown semantics. Keep implicit defaults and
explicit enablement distinct, disable other absent ordinary checks through the
full-catalog helper, and assert status, reporter contents and unchanged bytes.
The MD003 probe consumes validated `style`, so default `consistent` and override
`atx` pass while `setext` produces its controlled finding. Disabling it suppresses
the callback even with `style: setext`. Its controlled internal-failure case
remains covered separately.

For a production rule, implement an `engine.Check`, register its canonical ID in
`internal/cli/providers.go` through `engine.NewCatalogRegistry`, and exercise the
normal compiled executable with an otherwise admissible CommonMark fixture.
Use the existing catalog metadata/options rather than inventing a smaller catalog
or adding placeholder callbacks for unavailable rules. No dispatcher or reporter
change is needed for a registration. Synthetic fixture results do not establish
production availability. See the
[engine contracts](engineering.md#shared-engine-and-package-boundaries) for callback
and coverage requirements.

## Static analysis

Run the same pinned gate as CI from the checkout root (POSIX shell, including
Git Bash on Windows):

```sh
sh tools/lint.sh
sh tools/lint.sh --self-test
```

The script installs **golangci-lint v2.15.0** into a private temporary directory,
verifies [the checked-in configuration](../.golangci.yml), then checks all Go
packages including tests. It uses the installed Go toolchain with
`GOTOOLCHAIN=local`, disables workspaces, and runs analysis with
`GOFLAGS=-mod=readonly`. A missing toolchain, download failure, invalid
configuration, or finding fails the command. It never selects a golangci-lint
binary from `PATH`. The first install needs network access to the configured Go
module proxy and checksum database; subsequent installs reuse Go's module and
build caches. Go 1.27.2 and this exact linter version were verified together.
The older v2.13.2 binary cannot decode Go 1.27.2's export data.

The explicit analyzer set is govet, staticcheck (excluding optional QF style
suggestions), unused, ineffassign, errcheck, errorlint, exhaustive, nolintlint,
depguard, forbidigo, and gosec. Exhaustive accepts explicit default branches for
visitors that deliberately handle only a subset of node kinds; switches without
such a branch must cover the enum. Gosec remains enabled for filesystem and
service work as those packages arrive, as well as existing code and tests.

Depguard enforces the adopted boundaries: production parser code uses only the
standard library; production packages cannot import the conformance renderer;
YAML imports stay behind `internal/config` and MCP SDK imports behind
`internal/mcp`. The approved external import families are the selected
`github.com/goccy/go-yaml` and `github.com/modelcontextprotocol/go-sdk` modules.
YAML v1.19.2 is pinned in `go.mod`; the MCP SDK's accepted v1.8.0 pin is for its
future implementation. Depguard checks import paths, not module versions;
review changes to module pins against the accepted contracts. Suggested future
package layout does not impose an additional dependency graph.

Forbidigo restricts `os.Exit` and standard `log.Fatal*` functions/methods to
`cmd/mdchk/main.go` and the test adapter's
`internal/conformance/cmd/specadapter/main.go`, including renamed imports.
Other packages return errors or exit codes to their entry point.

Suppress only a reviewed false positive at the narrowest line with a named
analyzer and an explanation, for example `//nolint:gosec // G304: ...` with
the specific security rule and evidence. Nolintlint rejects blanket, unexplained,
and unused suppressions. Gosec's `#nosec` bypass is disabled. The existing G602
test suppressions cite prior equal-length assertions; G304 suppressions cover
committed fixture paths. There are no global security-rule exclusions.
One SA1012 suppression preserves the line index's intentional nil context and
its documented empty zero value.

`--self-test` creates and removes a disposable Go module. It verifies that all
eleven analyzers and each import boundary reject representative violations,
that legitimate adapter imports and entry points pass, and that configuration
errors fail. Local replacement modules exercise approved and unapproved imports
without downloading runtime dependencies or altering the checkout.

The tool is a development dependency only: version-qualified `go install` uses
the upstream module graph outside mdchk's module. It does not modify the root
`go.mod` or `go.sum`, or link tool dependencies into mdchk. Its capability is
the maintained analyzer integration and configuration schema, which would be
costly to reimplement internally. Provenance and the full transitive dependency
graph are recorded in the pinned upstream
[go.mod](https://github.com/golangci/golangci-lint/blob/v2.15.0/go.mod) and
[go.sum](https://github.com/golangci/golangci-lint/blob/v2.15.0/go.sum).
The tool is [GPL-3.0 licensed](https://github.com/golangci/golangci-lint/blob/v2.15.0/LICENSE);
its analyzer dependencies retain their own licenses. Source installation is
isolated and was validated here; upstream generally
[recommends binary installation](https://golangci-lint.run/docs/welcome/install/local/).
When upgrading the pin, review its transitive graph and licenses, Go export-data
compatibility, configuration changes, and both gate commands. Never independently
upgrade the tool's transitive dependencies or use an unpinned `latest` install.

### Vulnerability scanning

Run the same pinned vulnerability gate as CI from the checkout root:

```sh
sh tools/vulncheck.sh
sh tools/vulncheck.sh --self-test
```

The POSIX shell script installs **govulncheck v1.9.0** into a private temporary
`GOBIN`, then runs `govulncheck -format=text -scan=symbol -test
-show=traces,version ./...` against the complete root Go module. It includes test
packages, standard-library vulnerabilities applicable to the installed Go
version, and runtime dependency vulnerabilities as dependencies are added.
It uses `GOTOOLCHAIN=local`, `GOWORK=off` and analysis with
`GOFLAGS=-mod=readonly`, never a scanner from `PATH`. Go 1.27.2 and this exact
scanner pin were verified together. Installation needs the configured module
proxy and checksum database; scanning needs the live Go vulnerability database
at `https://vuln.go.dev`. Failures in installation, database access, loading or
analysis return nonzero. The default text format also returns nonzero for
applicable symbol findings and prints vulnerability IDs, affected/fixed versions
and call traces directly to the terminal and CI logs. Do not pipe through a
command that masks that status. JSON, SARIF and OpenVEX formats do not provide
the same finding exit behavior and must not replace this gate without explicit
status handling.

The independent, 15-minute `vulnerabilities` job in the existing Go workflow
runs on pushes, pull requests, manual dispatch and the existing Monday 04:23 UTC
schedule. It uses the minimum Go version in `go.mod` on Ubuntu amd64. Local
scans cover the current `GOOS`/`GOARCH`, build tags and installed toolchain; CI
covers its Linux amd64 build. Neither is an all-platform proof. Before shipping
other platforms or custom tags, repeat source scans with the corresponding Go
build environment. See upstream's [usage and analysis limitations](https://github.com/golang/vuln/blob/v1.9.0/cmd/govulncheck/doc.go),
including conservative interface analysis and reflection/unsafe blind spots.

The scanner version is reproducible; the live vulnerability database evolves
independently so newly published findings can fail the scheduled job without a
code change. The self-test uses disposable modules and synthetic local v1
[database fixtures](https://go.dev/security/vuln/database), never the live feed.
It checks clean and nonapplicable symbols, applicable production and test-only
calls with actionable output, database corruption and package loading failures.
Its fixture IDs describe invented vulnerabilities and are never suppressions.

For a finding, use the reported call trace and fixed version to upgrade the Go
toolchain to a supported patched version or update the affected dependency and
its checksums, then rerun scans and the normal compatibility checks. If the
supported minimum itself is affected, update `go.mod` and contributor/toolchain
requirements together. Retain the failed scan output with the remediation
record. Any exception requires explicit reviewed justification naming the
vulnerability ID, affected build/version, reachability evidence, owner and
expiry/recheck date. There are no current exceptions and no automatic scanner
suppression or `continue-on-error` bypass. An exception must not silently turn a
failed scan into a clean result.

Govulncheck supplies the maintained Go vulnerability database client and
call-graph analysis; reimplementing either would add a separate security
maintenance burden. This is development tooling only. Version-qualified
installation uses the exact upstream [go.mod](https://github.com/golang/vuln/blob/v1.9.0/go.mod)
and [go.sum](https://github.com/golang/vuln/blob/v1.9.0/go.sum) graph and leaves
mdchk's runtime module files unchanged. The Go team's scanner uses a
[BSD-style license](https://github.com/golang/vuln/blob/v1.9.0/LICENSE);
transitive dependencies retain their licenses. Review upstream maintenance,
release/security changes, the full graph and licenses, Go export-data
compatibility, and both gate commands whenever upgrading the pin. Do not use
`latest` or independently override the scanner's transitive dependencies.

### Package boundaries and current capability

`internal/parser` provides immutable source snapshots, physical-line/source
mapping, structural indentation helpers and a staged CommonMark 0.31.2 parser.
`NewSource` copies input bytes; `Bytes` returns a copy. Half-open Source spans
retain snapshot ownership and original Unicode, NUL, tabs and LF/CRLF/CR bytes.
After size admission and work-budgeted validation, a typed InvalidUTF8 failure
points to the first malformed scalar's original leading byte. Established encoding
failure precedes cancellation/deadline; prior exhaustion can stop validation.
See [parser accounting](engineering/parser-limits.md). Cancellation and deadline
remain separate incomplete outcomes, with deterministic checkpoint tests.

The block pass recognizes paragraphs, ATX/Setext headings, thematic breaks,
indented/fenced code, seven HTML-block forms, nested quotes/lists and leading
reference definitions. It retains exact envelopes, markers, logical content and
container ownership, including lazy continuation and partial-tab provenance.
List parents preserve initial ordered starts and independently derived tightness;
non-rendered definitions retain item boundary evidence. Reference records remain
source ordered, including duplicates; lookup selects the first normalized label.

After the completed block/reference handoff, the inline pass traverses paragraph
and heading payloads, constructing Text children from their logical Content
segments. Literal semantics remain separate from original-byte provenance; omitted
container prefixes and heading markers never enter text. NUL has explicit
Normalized provenance; no semantic offset is used as an original byte offset.
Code and raw HTML payloads remain opaque. Model constructors/accessors freeze or
detach every mutable layer, including returned Text handles and segment slices.

Supported text now includes one-pass ASCII punctuation escapes, all 2125
semicolon-terminated HTML5 named entities and strict numeric references. Decoded
characters remain semantic text rather than new Markdown structure. Text carries
explicit Escape/Entity spans; definition destinations/titles and fence info also
have known decoded values. Raw reference labels and first-definition lookup remain
unchanged. Code and raw HTML payloads stay opaque.

Code spans match maximal equal-width raw backtick runs with the first closer,
consume opaque payloads before Text decoding and retain original payload ranges,
delimiter width and markers separately from normalized Literal. Each internal
physical ending becomes one space; exactly one ASCII edge space is removed only
when both edges qualify and the result is not all spaces. Container prefixes stay
outside payload, and trimmed source bytes retain explicit transformation provenance.
Per-block raw-run indexes and monotonic source cursors avoid repeated suffix or
segment-prefix searches. Private trees publish only after successful inline work.

Nested emphasis/strong, inline links and full/collapsed/shortcut references now
complete, together with inline and reference images.
Inline HTML is now recognized atomically; malformed HTML resumes ordinary interpretation.
External physical endings produce semantic break
nodes, while endings consumed inside code remain code normalization. Unsupported interpretation retains
whole-document uncertainty and cannot render. Empty, definition-only and other
inline-free inputs retain their affirmative completion guards; constructed trees
cannot manufacture the private block handoff.

The [model contract](../internal/parser/model.md) defines exact outcomes,
byte envelopes, raw/semantic mappings, reference ownership and property meanings.
Retained upstream block tests independently assert paragraph/heading/container
structure, markers and original ranges, with
Text/CodeSpan/break/InlineHTML/autolink checks
where applicable.
`TestCodeSpanUpstreamAcceptanceAndNewPasses` checks unchanged 327–349 and newly
unlocked cases with independent node/literal/marker/payload assertions. Outside
providers remain explicit: 334 completes with a soft break, 344 completes with
raw HTML,
and 346 completes through its opaque autolink.
Nested/lazy mappings, both heading styles, NUL/Unicode, LF/CRLF/CR/EOF, opaque blocks,
mixed decoding, immutable accessors and block/call isolation have focused checks.
Deterministic cancellation/deadline sweeps protect unfinished inline publication;
established invalid UTF-8 retains precedence over cancellation/deadline within
the admission and validation budgets above. `FuzzInlineTextProvenance` reconstructs
Text, CodeSpan, break, InlineHTML and autolink semantics independently from
original-byte
contributions.

All 652 unchanged cases remain in the ledger: **652 pass, 0 pending**, retaining
all 629 previous passes and adding the 23 image-dependent cases with independent
AST/source assertions.
The unchanged CommonMark corpus passes in full; extension conformance is separate. Coverage owners are downstream
completion providers, never reverse blockers on this increment. Full parser work/depth budget hardening is implemented; see
[parser accounting](engineering/parser-limits.md).

`TestBreakUpstreamAcceptance` checks unchanged 633–649 and preserves actual
single-level emphasis and genuine remaining providers.
`TestBreakOriginsMappingAndCoordinates` and
`TestBreakBlockBoundariesAndOpaqueContexts` independently assert kinds/origins,
complete physical endings and markers, LF/CRLF/CR scalar positions, tabs/spaces,
escaped backslashes, entity-generated characters, heading/paragraph termination,
nested/lazy containers, code opacity and immutable accessors. Soft-break trailing
space remains a Normalized empty Text contribution; hard markers are separate from
ending Content. Break checkpoint sweeps protect cancellation/deadline publication
and encoding precedence. Existing inline/list fuzz consumers accept mixed children
while preserving independent source oracles. Bounded smoke commands:

```sh
go test ./internal/parser -run '^$' -fuzz '^FuzzInlineTextProvenance$' -fuzztime=2s -parallel=1
go test ./internal/parser -run '^$' -fuzz '^FuzzListMarkerSourceFacts$' -fuzztime=2s -parallel=1
```

Keep filesystem loading, configuration, rules, CLI/MCP, reporting, rendering and
writes outside the production parser. The HTML adapter is test infrastructure.
Initial packages remain internal and runtime dependencies remain standard-library
only. Public positions use one-based lines/Unicode scalar columns; tabs count as
one diagnostic column, independently of structural tab stops.

### Scanner and mapping capabilities

Build `parser.NewLineIndex(source)` once and reuse it. Its `LineCount` and `Line`
methods retain physical LF, CRLF and CR content/ending spans even on malformed
bytes. Empty input has zero selectable lines; a final ending adds no selectable
line. `Position`/`Offset` convert strict-valid UTF-8 scalar boundaries, excluding
CRLF interiors, and reject invalid requests with `ErrPosition`. They never clamp
or guess. Terminator start is the preceding line's end position; terminator end
is next-line column 1. Empty EOF is (1,1); a final ending creates a virtual EOF
position on the next line without making that line selectable. Byte Span/Text
access remains available for arbitrary bounded ranges.

Malformed sources retain a typed InvalidUTF8 failure at the first invalid scalar's
original leading byte; ordinary text conversion and semantic/indentation helpers
are unavailable. `FailurePosition` derives that diagnostic point only from the
valid prefix. It does not enable normal parsing or replace malformed bytes.

`SemanticText` replaces valid NUL with semantic U+FFFD using explicit Normalized
provenance over the original byte; literal source U+FFFD stays Exact. `Indentation`
uses separate zero-based structural columns, four-column tab stops and residual
PartialTab spans across repeated consumption and caller-interpreted markers.
Remaining content keeps internal tabs literal. Cursor copies advance independently;
the shared Source and LineIndex remain immutable. These helpers recognize no syntax.

For example, `é\t\x00\r\nZ` has seven original bytes. NUL [3,4) contributes U+FFFD,
but `Z` stays at original byte 6 / diagnostic (2,1), and EOF at byte 7 / (2,2).
The tab counts as one diagnostic column; only structural indentation consumption
uses tab stops. Semantic replacement length cannot be used to locate source edits.
For `>\t\tfoo`, advancing the marker, consuming one delimiter column and four
indentation columns leaves two semantic spaces backed by the second tab [2,3).

Construction takes O(N) time and O(L) boundary storage. Position lookup takes
O(log L + B), reverse lookup O(B), for the selected line-prefix byte count B;
neither scans earlier lines or copies the whole source. Repeated long-line lookups
may repeatedly decode their prefix. Semantic helpers take work/storage proportional
to selected bytes. Optional measured follow-ups include checkpoints and benchmarks.

Tests independently verify expected coordinates and rejection cases before round
trips, raw line counts/ranges, exact CommonMark character classes, actual helper
tab/NUL provenance, repeatability and original-byte preservation. The separate
`scanner.json` registry names retained Tabs IDs 1–11 as scanner/indentation targets,
checking identity, section and exact input fidelity. It adds no HTML passes:
example 11's supported HTML comes from actual thematic recognition. Other scanner
targets now all pass actual HTML, independently of these scanner-only assertions.

### Angle URI and email autolinks

Autolinks use the pinned CommonMark scheme/email grammar and consume valid raw
interiors before ordinary escape/entity or delimiter processing. Labels retain
literal backslashes/entities and original angle/address spans; email destinations
add a source-absent Synthetic `mailto:` prefix. URI semantics are unencoded, with
URL/HTML escaping performed only by the test adapter. Failed autolinks resume
ordinary text interpretation, including recognized inline HTML where applicable.
Bare
URI/email forms stay Text. See the [model contract](../internal/parser/model.md#inline-text-and-staged-providers)
for marker roles, source ownership and stage boundaries.

`TestAutolinkUpstreamAcceptanceAndNewPasses` pairs unchanged 594–612 and all newly
unlocked cross-feature cases with independent AST/source facts and actual
Parse/Evaluate output. Grammar, opacity, mapped container/heading source,
Unicode/NUL, ownership and cancellation/deadline fixtures supplement the corpus.
The existing inline/list fuzz consumers retain independent grammar/provenance
checks for Links as well as their previous source assertions. Focused checks:

```sh
go test ./internal/parser -run 'Autolink|Inline|CodeSpan|Decoding'
python3 -B tools/commonmark_conformance.py --strict --section '^Autolinks$'
```

### Inline raw HTML

Raw inline tags, attributes, comments, processing instructions, declarations and
CDATA follow CommonMark 0.31.2. Recognition consumes accepted interiors opaquely;
malformed candidates resume ordinary Markdown interpretation. InlineHTML retains
original ordered Content and exact byte envelopes through container gaps, with
confirmed-comment opening/closing markers documented in the model contract.
The short comments `<!-->` and `<!--->` have explicit closing boundaries.
No directive interpretation or production rendering is added.

All Raw HTML examples 613–632 now pass actual Parse/Evaluate. Inline HTML adds
28 passes overall: 187, 344, 475–477, 524, 536, 613–617, 619–632 and 642–643.
Literal fallback 618 was already supported. Independent AST/source tests cover
each promotion alongside unchanged HTML, including raw entity/backslash spelling,
quoted punctuation, malformed fallback, block-first boundaries, heading/container
mapping, Unicode/NUL, physical endings and immutable accessors. Deterministic
cancellation/deadline sweeps protect indexing, scanning and mapping publication;
the inline/list fuzz oracles now validate raw HTML provenance too. Focused checks:

```sh
go test ./internal/parser -run 'InlineHTML|HTML|Inline'
python3 -B tools/commonmark_conformance.py --strict --section '^Raw HTML$'
```

### Nested emphasis

The delimiter pass classifies original star/underscore runs using the pinned
Unicode whitespace/punctuation rules, intraword restrictions and modulo-three
eligibility. A linked stack with partitioned failed-search floors bounds opener
searches. Scoped stack-bottom calls preserve earlier delimiters and retire only
the processed suffix; direct tests cover repeated calls and empty boundaries.

Pairs form a private forest, then explicit frames build nested Emphasis/Strong
without recursive construction calls or copying accumulated child prefixes.
Indexed raw Content and an advancing flat-node cursor retain consumed/leftover
markers. Parent Content includes inner syntax bytes; exact child markers, payload
and decoded Text semantics remain distinct. Opaque interiors and generated
markers never enter delimiter dispatch. Detached accessors and original snapshots
remain immutable; nested work inherits cancellation/deadline checkpoints.

The original nested-emphasis partition required 126 of 132 examples: 90
established and 36 nested passes. Inline-link delivery completes the other six
404/419/422/433/473/474, so all 132 emphasis examples now pass. Case 516 also
completes, as does reference case 530. No delivered #25–#27 ownership remains.

TestEmphasisApprovedPartitionAndUpstream compares actual Parse/Evaluate with
unchanged upstream HTML and independently counted opening/closing ranges.
Recursive provenance checks reconstruct raw parent Content from child source
coverage. Separate mapped nested fixtures cover headings, nested/lazy containers,
NUL/Unicode, LF/CRLF/CR/EOF, partial tabs, opacity, long runs and detached handles.
Scoped-boundary and deterministic interruption tests verify the private handoff
and unfinished-tree publication. Inline/list fuzz consumers retain source oracles
and add nested seeds. Focused commands:

```sh
go test ./internal/parser -run 'Emphasis|Inline|CodeSpan|Break'
python3 -B tools/commonmark_conformance.py --section '^Emphasis and strong emphasis$'
```

The full unchanged CommonMark corpus now passes. Numeric work/depth accounting is documented in
[parser accounting](engineering/parser-limits.md). Optional measurements should distinguish unavoidable nested
payload output from redundant copying; no machine-specific threshold is imposed.

### Inline links

Active bracket openers recognize inline links before reference fallback. Successful
links and autolinks deactivate earlier link openers without repeatedly walking an
inactive prefix. Label emphasis runs above the opener's saved stack boundary;
scoped suffix retirement and later delimiter appends preserve earlier state.
Destinations/titles obey the pinned raw grammar and decode escapes/entities once.
Source-backed components retain absent versus present-empty titles, known-empty
destinations, original delimiters and immutable mapped Content. Stored URLs remain
unencoded; only the test adapter encodes and escapes them.

The 47 sole-#26 cases plus inline-precedence 567 are required independently of
implementation success. The full run also proves 14 literal cases:
197/199/201/209/211/212/213/511/523/546/547/548/551/552. All 513 prior passes
remain; reference uses and images now advance the gate to 652 pass/0 pending.
A completed inventory settles reference matches and literal misses.

`TestInlineLinkApprovedPartitionAndUpstream` pairs actual Parse/Evaluate over all
62 promoted examples with independently supplied raw component fixtures and source
assertions. Origin-aware inline/list/emphasis/decoding oracles preserve existing
autolink checks. Additional fixtures cover mapped headings and containers, all
endings/EOF, Unicode/NUL, empty values, raw/decoded spellings, opacity, malformed
recovery, nested-link rejection, label emphasis, detached accessors and interruption
through private construction. Focused checks:

```sh
go test ./internal/parser -run 'InlineLink|Emphasis|Decoding|Autolink'
python3 -B tools/commonmark_conformance.py --section '^Links$'
```

Malformed overlapping bare destinations use a lazily built raw lexical endpoint
index instead of rescanning each suffix. The index adds linear per-block storage,
preserves escaped/balanced parentheses without a nesting limit, and retains later
valid links after rejected candidates. `TestInlineLinkFailedDestinationWork` and
`TestInlineLinkFailedDestinationRecovery` check cumulative work under doubled input
sizes with independent literal/source and recovered-link assertions. Optional local
measurement (source construction excluded from timing):

```sh
GOMAXPROCS=2 go test ./internal/parser -run '^$' -bench '^BenchmarkInlineLinkFailedDestinations$' -benchtime=1x -count=3
```

The full contributor checks and bounded inline/list fuzz commands above still
apply. Comprehensive numeric work/depth hardening is implemented; cumulative
bracket/candidate measurements are optional, with no machine-specific threshold.

### Reference links

Full/collapsed/shortcut forms borrow the completed private registry and share raw
Unicode label normalization, first-definition selection, bracket activation and
scoped emphasis. Literal misses with unrelated definitions complete normally;
valid inline 567 retains priority, and 568–571 verify failed-inline/subsequent-label
consumption. No URL is dereferenced.

Links retain exact use-site syntax, separate visible Label and optional raw
ReferenceLabel, and a NormalizedLabel association to the owning document's first
selected record. Destination/title semantics are spanless Synthetic projections;
the authoritative source ranges remain on that definition. See the [model
contract](../internal/parser/model.md#reference-links-and-definition-identity)
for marker roles, absent/empty values, local binding validation and ownership.

The fixed 53 sole-#27 cases plus escaped-marker 593 pass unchanged upstream HTML
with independent AST/component/marker ranges and definition-selection assertions.
Raw spelling, pinned full folding, 999/1000-character bounds, mapped headings and
containers, all physical endings, duplicate definitions, detached accessors, foreign
snapshots, projection mismatch and deterministic interruption have focused tests.
Inline/list fuzz seeds exercise all three origins with independent provenance
oracles. All previous passes remain; images complete the 652-case corpus.

```sh
go test ./internal/parser -run 'ReferenceLink|InlineLink|Reference|FullCaseFold'
python3 -B tools/commonmark_conformance.py --strict --section '^(Links|Link reference definitions)$'
```

The contributor checks and bounded inline/list provenance fuzz commands above
remain required for changes to those consumers. Optional reference-use/definition
scaling measurements introduce no machine-specific threshold; general hardening is documented in [parser accounting](engineering/parser-limits.md).

### Images

Images use the same inline destination/title grammar and full/collapsed/shortcut
reference resolver as links. Each Image envelope includes its `!` with a separate
`image-marker`; label Content excludes that marker and the brackets. Children
retain parsed formatting, nested images and links. Image openers stay active when
a link succeeds inside their description; completing an image leaves earlier link
openers active. Links still cannot contain links.

Reference images share first-definition identity and spanless Synthetic projections
with reference links. Both model and document constructors reject malformed or
unbound resolved reference images. The test renderer flattens inline semantic
children for alt text, including code and breaks, while omitting raw HTML tags.

`TestImageUpstreamAcceptance` pairs unchanged 572–593 and 517/520/531 with
independent AST, marker, component and source facts. Additional tests cover mapped
headings/containers, physical endings, Unicode/NUL, empty components, full folding,
duplicate definitions, missing explicit keys, escaped markers and immutable handles.
The existing inline/list fuzz oracles now validate images; interruption sweeps
include both inline and reference images.

```sh
go test ./internal/parser -run 'Image|ReferenceLink|InlineLink'
python3 -B tools/commonmark_conformance.py --strict --require-complete
```

## Regenerate and verify the CommonMark corpus

Use Python 3.9 or newer, with only its standard library. From the repository root:

```sh
python3 -B tools/commonmark_corpus.py
python3 -B tools/commonmark_corpus.py --check
python3 -B -m unittest discover -s tools -p 'test_commonmark_corpus.py' -v
```

The first command regenerates
`internal/parser/testdata/commonmark/0.31.2/examples.json` from the unmodified,
pinned specification and upstream test software retained alongside it in
`upstream/`. The second command verifies retained input hashes and independently
regenerated bytes against the committed fixtures, without writing files. It
returns nonzero on drift or damaged inputs; CI runs it and the integration tests.
Those tests exercise exact field fidelity, repeated generation, and separate
fixture/input corruption failures in disposable copies.

Both tool modes work offline without an external renderer or developer-local
spec file. From another working directory, invoke the tool by its absolute path;
all input and output paths are resolved relative to the tool. Status messages do
not enter the generated JSON. See the [corpus README](../internal/parser/testdata/commonmark/0.31.2/README.md)
for provenance, original licenses, all six upstream fields, source-line semantics,
and drift recovery. Corpus ingestion includes every upstream example; it does
not claim parser or correction conformance.

## Run CommonMark regression checks

From the repository root, with Go and Python 3.9 or newer installed:

```sh
python3 -B tools/commonmark_conformance.py --require-complete
python3 -B tools/commonmark_conformance.py --strict --require-complete
python3 -B tools/commonmark_conformance.py --example 207 --json
python3 -B tools/commonmark_conformance.py --section '^Tabs$'
python3 -B -m unittest discover -s tools -p 'test_commonmark_conformance.py' -v
```

The runner verifies the retained corpus/helper hashes, builds the test adapter
once in a temporary directory, and invokes it once for each selected original
example. It uses the real parser and a test-only renderer over its AST, without
a native cmark library, third-party parser or production rendering command.
Inputs and paths are resolved from the script, so invocation by absolute script
path also works from another directory. The checks work offline; use the local
toolchain/module environment above to forbid automatic downloads explicitly.

Every unfiltered run currently reports **652 pass, 0 fail, 0 error, 0
pending**. The separately maintained `foundation.json` lists every supported ID
and remaining provider exactly once. Angle autolinks add 24 passes while retaining
all 331 established IDs: 20, 346, 480–481, 526, 538, 594–610 and literal HTML
fallback 618. All 19 Autolinks examples 594–612 now complete; bare 611–612 remain
ordinary Text. Examples 638–639 now complete through single-level emphasis;
344/642/643 pass with opaque inline HTML, and 640–641 retain code opacity.
Images complete 517/520/531 and 572–591. Reference uses
now settle both matching links and literal fallback, including escaped-marker 593.
No pending case is counted as a conformance pass. All 652 cases pass without exemptions. These coverage owners are not reverse native
prerequisites for staged delivery. Independent constructed AST
tests verify renderer output for the core model catalog, escaping/URL
encoding, raw segments, lists and image labels without claiming that Parse
produced those trees. Malformed UTF-8, adapter execution/protocol failures and
interruption cannot become syntax-pending. Raw HTML output preserves its
logical spelling after prefix/tab handling and CommonMark's mandatory NUL to
U+FFFD substitution; original source bytes/spans remain unchanged. Unsupported or unknown
transformed raw contributions are refused rather than silently decoded.

Default comparisons use the unchanged pinned upstream normalizer and its terminal
newline convention. `--strict` compares exact HTML output instead. The report
includes original example/section/spec-line identities, mismatch diffs, explicit
pass/fail/error/pending counts and selected/filtered-out scope. JSON also supplies
every original selected ID and `full_conformance`; staged success with pending
cases always leaves that flag false.
Ordinary per-example protocol or comparison exceptions become explicit error
outcomes with the original identity and error metadata; remaining selected cases
are still evaluated. A retained-normalizer error never becomes a pass or a
fallback comparison. Process-level interruption is not swallowed as an ordinary
comparison error.

Exit 0 means the staged regression gate passed; pending cases are allowed. Exit 1
means a mismatch, error, ratchet violation or unmet `--require-complete` gate.
Exit 2 means setup, corpus/ledger validation or argument selection failed; no
successful report is produced. `--require-complete` also fails on selected pending
cases. Final all-case completion requires an unfiltered run with all 652 passes,
no failures/errors/pending and `full_conformance: true`.

Repeat `--example` to select several original IDs; `--section` is a case-insensitive
regular expression and combines with example selection. Unknown IDs, invalid
regexes and empty selections are errors. A filtered run validates the full ledger
first, reports its smaller scope and never writes or weakens the baseline.

`foundation.json` is mdchk's separately reviewed pass/pending ledger. It
accounts for all 652 IDs exactly once. Its supported list records the 652
passes above. A supported case becoming fail/error/pending is a regression;
missing/duplicate IDs invalidate the ledger. A newly passing case also fails
the gate until its ID moves from pending to `supported_html_examples` in a
reviewed syntax delivery with independent AST/source-span tests. Keep existing
supported IDs and inspect any proposed ledger change for weakening. The runner
never updates it automatically or edits generated expectations. CI runs the
unfiltered `--require-complete` gate and tests synthetic pass baselines to detect
lost passes and unrecorded new passes independently of the real corpus counts.

To reuse a compiled executable, build outside the checkout and pass its path:

```sh
adapter_dir=$(mktemp -d)
go build -o "$adapter_dir/specadapter" ./internal/conformance/cmd/specadapter
python3 -B tools/commonmark_conformance.py --adapter "$adapter_dir/specadapter" --require-complete
python3 -B internal/parser/testdata/commonmark/0.31.2/upstream/test/spec_tests.py \
  --spec internal/parser/testdata/commonmark/0.31.2/upstream/spec.txt \
  --program "$adapter_dir/specadapter"
python3 -B internal/parser/testdata/commonmark/0.31.2/upstream/test/spec_tests.py \
  --spec internal/parser/testdata/commonmark/0.31.2/upstream/spec.txt \
  --program "$adapter_dir/specadapter" --no-normalize
rm -r "$adapter_dir"
```

Both upstream comparisons must exit 0 and report exactly `652 passed, 0 failed,
0 errored, 0 skipped`. The conformance tooling tests run these unfiltered commands
against a freshly built adapter in CI and assert both exit status and the entire
summary; upstream exit status alone allows skipped cases. The
[pinned corpus provenance](../internal/parser/testdata/commonmark/0.31.2/README.md)
records source commit `9103e341a973013013bb1a80e13567007c5cef6f` and spec SHA-256
`257c41ad946f7a1414a499aca402a1aa8fdac3678532266611348c1cf54f4b80`.

For focused diagnostics, add `--number 207`: it is a recognized definition-only
document with legitimately empty HTML. The unchanged upstream helper still has no
pending category; `--number 467` now succeeds through nested emphasis. Use
`--number 531` to check a reference link containing an image.
Reference case 530 now succeeds.
Examples 419/516/567 now succeed through actual inline-link recognition.
Example 344 now succeeds through actual inline HTML recognition. Example 334 now
passes with an external soft break. The adapter's default protocol
emits HTML alone on stdout only for complete rendered input. Pending input exits 2
with an outcome on stderr, errors exit 1, and interruptions exit 3. Upstream maps
all nonzero exits to error, including pending; use mdchk's runner for staged
accounting. Complete empty and definition-only 207 inputs succeed with empty
stdout only after affirmative complete parsing; pending text must not falsely
pass by returning empty output. The retained
helper splits `--program` on whitespace, so use an executable path without spaces
for that helper. mdchk's runner uses an argument vector and accepts spaced paths.

The adapter's `--json` protocol is for the test runner: transport success exits 0
with a versioned `rendered`, `pending`, `error` or `interrupted` outcome. That exit
alone says nothing about conformance. HTML is meaningful only for `rendered`.
The focused Go/Python tests exercise actual empty/pending/encoding outcomes,
retained-helper success and execution-error paths, both comparison modes, case
207, filter errors, and simulated lost/new passes. Synthetic transport fixtures
are separate from the immutable upstream corpus.


### Escapes and character references

The shared decoder supplies inline Text, reference destination/title Values and
fence info while retaining original source spans and existing NUL/newline/partial-
tab contributions. It never reinterprets generated characters as syntax or decodes
them twice. Numeric references require decimal 1–7 or hexadecimal 1–6 digits and
semicolons; zero and invalid Unicode scalars become U+FFFD. Other valid scalars,
including controls, are preserved without browser numeric remapping. Unknown or
malformed references remain literal. Physical backslash-newline is recognized
by the break handler before ordinary text decoding.

[WHATWG entity provenance and license](../internal/parser/testdata/html5/README.md)
cover the complete case-sensitive named set and offline regeneration. Run:

```sh
python3 -B tools/html_entities.py --check
go test ./internal/parser -run 'NamedEntities|Decoding'
```

`TestNamedEntitiesAgainstAuthoritativeData` reads every name/value from the pinned
JSON independently of the generated production table and checks actual Parse spans.
`TestDecodingUpstreamAcceptanceAndNewPasses` consumes unchanged upstream inputs;
other decoding tests assert one-pass structure, exact nested source contributions,
opaque boundaries, reference phases/identity, ownership and deterministic context
interruption. The inline fuzz oracle uses retained JSON and independent regexp/
strconv interpretation of raw contributions, rather than the production decoder.

### Reference definitions and Unicode folding

Leading definitions are collected document-wide before inline resolution, outside
AST child traversal. All duplicate/unused records retain original components and
markers; normalized lookup selects the earliest. Invalid candidates and ordinary
remainders retain their paragraph/Setext/container structure. Actual Parse tests
exercise every unchanged definition example 192–218, plus list 317 and independent
boundary/source, full-fold, ownership and deterministic interruption fixtures.
Definition-only 207 completes, and literal remnants 208/210 now render with Text.
All definition examples 192–218 now render through actual Parse/Evaluate.
Forward references resolve against the completed document-wide registry; no
blanket exemption or delivered provider remains in `foundation.json`.

Unicode matching uses the pinned Unicode 17.0.0 full default folding table with
standard-library-only runtime code. No host locale/Python database or runtime
fetch influences matching. See [provenance and license](../internal/parser/testdata/unicode/17.0.0/README.md).
Regenerate/check offline with Python 3.9+ and gofmt:

```sh
python3 -B tools/unicode_casefold.py
python3 -B tools/unicode_casefold.py --check
go test ./internal/parser -run 'Reference|FullCaseFold'
```

Lazy-continuation regressions also assert that incomplete definitions cannot
consume missing quote/item prefixes, while genuine lazy prose is retained.
Deterministic work counts and a focused benchmark cover growing multiline titles:

```sh
go test ./internal/parser -run 'ReferenceComponents|ReferenceLazy|ReferenceInterruption' -count=1
GOMAXPROCS=1 go test ./internal/parser -run '^$' -bench '^BenchmarkReferenceContainerTitles$' -benchtime=1x -count=3
```

The closed missing-prefix fixture remains ordinary visible text; only its fully
prefixed counterpart defines a reference. Compare unterminated and fully prefixed
controls separately when assessing performance; no general resource-budget or
machine-specific numeric gate is implied.

The check validates pinned source bytes and regenerated Go bytes; Go tests compare
all C/F mappings directly with the retained source, including default versus
Turkic behavior. The [parser accounting specification](engineering/parser-limits.md) defines
shared finite parser budgets and interruption semantics.

## Initialize a new clone

Install **Task v3.54.0**, the supported and verified repository task runner.
With Go installed, use the upstream [source installation](https://taskfile.dev/docs/installation)
with an exact version:

```sh
go install github.com/go-task/task/v3/cmd/task@v3.54.0
```

Put Go's installation directory (`go env GOBIN`, or `$(go env GOPATH)/bin`
when GOBIN is empty) on your `PATH`. Alternatively, install the matching binary
for your OS/architecture from the official [v3.54.0 release](https://github.com/go-task/task/releases/tag/v3.54.0)
and verify it against that release's checksums. Confirm `task --version`
reports `3.54.0`; other versions have not been validated here.

Clone the repository, then initialize it from the checkout:

```sh
git clone https://github.com/frithuz/mdchk.git
cd mdchk
task --list
task repo:init
```

`repo:init` is the setup entry point. It currently activates only the executable
[commit-msg hook](../.githooks/commit-msg), by setting repository-local
`core.hooksPath` to `.githooks`. It does not change global Git configuration,
install tools, download anything, or require building mdchk. Rerunning it is
safe and leaves the same hook configuration in place. Missing prerequisites,
a missing or non-executable hook, or a failed configuration write fail setup.
The task runs at the Taskfile root even when invoked from a subdirectory;
future setup steps can be composed in its ordered command list.

This setting replaces the hook directory for this local repository, including
an inherited global hook directory. Initialization warns when replacing a
different effective path. If you already use hooks, integrate them into
`.githooks` before initialization; existing hooks elsewhere are no longer run.

Task supplies discoverable tasks and ordered, extensible setup orchestration
without a separate hook manager. This is development tooling only, with no
mdchk runtime dependency or root module-file changes. Its full transitive
graph is pinned by the upstream [go.mod](https://github.com/go-task/task/blob/v3.54.0/go.mod)
and [go.sum](https://github.com/go-task/task/blob/v3.54.0/go.sum).
Task is [MIT licensed](https://github.com/go-task/task/blob/v3.54.0/LICENSE);
its dependencies retain their own licenses. When upgrading the supported pin,
review upstream maintenance/security changes, the dependency graph and licenses,
Taskfile compatibility, and fresh-clone, repeat-setup and hook integration checks.
Do not use `latest` or independently override its transitive dependencies.

Initialization and the delivered hook target macOS and Linux with POSIX
`/bin/sh` and standard Unix utilities. This setup was verified on Darwin arm64;
Linux uses the same shell/utility interface but was not run locally. Native
Windows setup is not currently validated or supported by this Taskfile.

The hook uses POSIX `/bin/sh`, Git 2.22 or newer, and standard Unix utilities,
including `awk`, `cmp`, `cp`, `mv`, `tr`, `wc`, and `mktemp -d` (available on
macOS and Linux). The commit message must be a readable, writable regular text
file, and its directory must allow temporary files and atomic replacement.

On normal and amended commits, the hook silently removes all case variants
of `Co-Authored-By:` trailers and their continuation lines. It uses
[`git interpret-trailers`](https://git-scm.com/docs/git-interpret-trailers)
to distinguish trailers from subject and body text. Colon is always the
trailer separator for this policy, regardless of `trailer.separators` settings.
Other message bytes, including unrelated trailers, are preserved. Configured
trailer commands are not executed. Git still applies its usual commit message
cleanup after the hook.

If parsing, reading, writing, or replacement fails, the hook prints an error
and rejects the commit. It also rejects ambiguous messages where removing the
footer would expose a previous co-author-shaped paragraph as another trailer;
rewrite that paragraph as ordinary prose before retrying.

## Verify the hook

Shell script tests use [Bats](https://bats-core.readthedocs.io/) and live beside
the script they test. Install Bats 1.7 or newer, then run the hook's integration
suite from the repository root:

```sh
bats .githooks/commit-msg.bats
```

Each test uses its own temporary repository. The suite checks message preservation,
repeated sanitization, failure handling, and real normal and amended commits.
It does not change this repository's Git configuration or history.

To exercise direct hook invocations with another POSIX shell, set `HOOK_SHELL`:

```sh
HOOK_SHELL=dash bats .githooks/commit-msg.bats
```

Bats runs tests using Bash; the hook itself still runs under POSIX `/bin/sh`
when invoked by Git and does not require Bats.

## Local enforcement limits

Hook activation is local configuration and is not enabled automatically by a
clone. `git commit --no-verify`, changing `core.hooksPath`, deleting or disabling
the hook, and commands that do not run `commit-msg` (such as `git commit-tree`)
can bypass it. Existing commits are not rewritten. The prohibition still
applies when the hook is bypassed; this local hook alone cannot enforce it
across all contributors or commits received by a remote repository.


## Parser resource, race and fuzz verification

`Parse` uses the accepted finite defaults. Use one validated `OperationBudget`
with `ParseWithBudget` across an operation's documents/reparses and `ChargeWork`
for adapter work; only document counters reset. See [parser accounting](engineering/parser-limits.md)
for exact scope, depth, implementation bounds, typed outcomes and admission
precedence. Source/model constructors and accessors outside Parse are not an
operation-metered loading API.

The race job uses Ubuntu 24.04 amd64, GCC and `CGO_ENABLED=1`. Local race checks
require a Go-supported race OS/architecture, cgo and a working C compiler; the
verified local host is Darwin arm64 with its installed developer toolchain.
Report an unsupported race host as unavailable; an ordinary test pass does not
substitute for race verification. Independent concurrent parses and serialized
shared-operation parses are exercised by the race suite.

```sh
go build ./...
go test ./...
go vet ./...
CGO_ENABLED=1 go test -race ./...
(
  for target in FuzzParse FuzzInlineTextProvenance FuzzReferenceRegistry FuzzListMarkerSourceFacts FuzzListContinuationOwnership FuzzListGroupingBoundaries; do
    go test ./internal/parser -run '^$' -fuzz "^${target}$" -fuzztime=30s -parallel=2 -timeout=2m || exit $?
  done
)
GOMAXPROCS=1 go test -bench=. -run '^$' -benchmem ./internal/parser
```

`FuzzParse` directly adds all 652 retained upstream Markdown examples and focused
adversarial inputs. It bounds input to 64 KiB, checks deterministic semantic
outcomes, immutable snapshot/input bytes and every AST/Value/reference source
range, and asserts explicit resource interruption rather than accepting internal
failure. Existing specialized targets retain their independent source oracles;
the arbitrary-input ones distinguish explicit budget interruption before checking
unavailable syntax. The full corpus gates still run all unchanged examples under
default limits, independently of fuzz size bounds.

CI runs six independent 10-second smoke targets for pushes/PRs and six explicit
5-minute campaigns each Monday at 04:23 UTC (two workers per target, 12-minute
Go timeout, 15-minute job timeout). Native minimized failures are retained as
30-day CI artifacts from `internal/parser/testdata/fuzz/`. Download and preserve
`testdata/fuzz/<Target>/<hash>` under that directory, then replay:

```sh
go test ./internal/parser -run '^<Target>/<hash>$'
```

Fix the regression and commit the minimized input as a permanent native Go fuzz
corpus entry. Do not delete a failing corpus entry to make CI pass. Longer local
campaigns use the same command with an explicit `-fuzztime=5m` or longer chosen
budget. See [recorded timing/allocation baselines](engineering/parser-performance.md)
for fixture sizes, host, measurement limits and profiling investigation.
