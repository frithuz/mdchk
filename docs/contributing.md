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

The module is `github.com/frithuz/mdchk`. It currently uses only the Go standard
library, with no third-party dependencies or `go.sum` entries. With Go already
installed, the commands above need no network access. To check without automatic
toolchain downloads, workspace overrides, module downloads or module-file edits:

```sh
GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly go test ./...
GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly go vet ./...
```

The [Go workflow](../.github/workflows/go.yml) runs test, vet, and the CommonMark
corpus checks below on pushes and pull requests. Its actions are pinned to commit
IDs; module caching is disabled while there are no external dependencies. Go
package builds provide no production command-line executable; the conformance
adapter below is a separate test command.

### Package boundaries and current capability

`internal/parser` provides immutable source snapshots, physical-line/source
mapping, structural indentation helpers and a staged CommonMark 0.31.2 parser.
`NewSource` copies input bytes; `Bytes` returns a copy. Half-open Source spans
retain snapshot ownership and original Unicode, NUL, tabs and LF/CRLF/CR bytes.
Strict UTF-8 validation precedes Markdown work; a typed InvalidUTF8 failure points
to the first malformed scalar's original leading byte. Cancellation and deadline
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

Delimiter/bracket pairs remain honestly pending for later handlers. Inline HTML
is now recognized atomically; malformed HTML resumes ordinary interpretation.
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
strict invalid UTF-8 retains precedence. `FuzzInlineTextProvenance` reconstructs
Text, CodeSpan, break, InlineHTML and autolink semantics independently from
original-byte
contributions.

All 652 unchanged cases remain in the ledger: **383 pass, 269 pending**, retaining
all 355 previous passes and adding 28 from inline HTML and ordinary fallback.
These are staged results, not full CommonMark or extension conformance. Coverage owners are downstream
completion providers, never reverse blockers on this increment. Full work/depth-
budget hardening remains #31.

`TestBreakUpstreamAcceptance` checks unchanged 633–649 and preserves actual
pending emphasis providers. `TestBreakOriginsMappingAndCoordinates` and
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
python3 -B tools/commonmark_conformance.py
python3 -B tools/commonmark_conformance.py --strict
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

Every unfiltered run currently reports **383 pass, 0 fail, 0 error, 269
pending**. The separately maintained `foundation.json` lists every supported ID
and remaining provider exactly once. Angle autolinks add 24 passes while retaining
all 331 established IDs: 20, 346, 480–481, 526, 538, 594–610 and literal HTML
fallback 618. All 19 Autolinks examples 594–612 now complete; bare 611–612 remain
ordinary Text. Examples 638–639 retain emphasis; 344/642/643 now pass with opaque
inline HTML,
and 640–641 retain code opacity. Remaining providers are #24/#25 emphasis,
#26/#27 links and #28 images. Conservative pending checks may
also defer malformed candidates until their handler proves literal fallback.
No pending case is counted as a conformance pass. Final completion still requires
all 652 cases without exemptions. These coverage owners are not reverse native
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
accounts for all 652 IDs exactly once. Its supported list records the 383
passes above. A supported case becoming fail/error/pending is a regression;
missing/duplicate IDs invalidate the ledger. A newly passing case also fails
the gate until its ID moves from pending to `supported_html_examples` in a
reviewed syntax delivery with independent AST/source-span tests. Keep existing
supported IDs and inspect any proposed ledger change for weakening. The runner
never updates it automatically or edits generated expectations. CI runs the
full gate and tests synthetic pass baselines to detect lost passes and
unrecorded new passes independently of the real corpus counts.

To reuse a compiled executable, build outside the checkout and pass its path:

```sh
adapter_dir=$(mktemp -d)
go build -o "$adapter_dir/specadapter" ./internal/conformance/cmd/specadapter
python3 -B tools/commonmark_conformance.py --adapter "$adapter_dir/specadapter"
python3 -B internal/parser/testdata/commonmark/0.31.2/upstream/test/spec_tests.py \
  --spec internal/parser/testdata/commonmark/0.31.2/upstream/spec.txt \
  --program "$adapter_dir/specadapter" --number 207
rm -r "$adapter_dir"
```

The last comparison now exits 0: example 207 is a recognized definition-only
document with legitimately empty HTML. The unchanged upstream helper still has no
pending category; use `--number 638` to observe unsupported emphasis as an error.
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
Remaining definition cases retain their actual link/HTML/break providers in
`foundation.json`, without #17/#18/#19 or a blanket exemption.

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
Turkic behavior. #31 retains comprehensive parser-budget hardening.

## Activate the commit message hook

After cloning, run this from the repository root:

```sh
git config core.hooksPath .githooks
```

The executable [commit-msg hook](../.githooks/commit-msg) works immediately;
building mdchk and installing a third-party hook manager are unnecessary.
This setting replaces the hook directory for this local repository. If you
already use hooks, integrate them into that directory before switching it.

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
