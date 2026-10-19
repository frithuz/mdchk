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

`internal/parser` provides immutable source snapshots, physical-line and text
mapping, structural indentation helpers, a source-bound core CommonMark model,
and `Parse(ctx, source)`. `NewSource` copies incoming bytes and
`Bytes` returns a copy; the zero Source remains empty. Acquisition preserves
Unicode bytes, line endings, tabs, NUL and invalid encoding. `Source.Span` checks
half-open byte bounds/ownership; `Source.Text` exposes immutable original-byte
views without copying the complete snapshot for each range.

Parse enforces strict UTF-8 before Markdown work. Empty and ASCII-blank input
produce an available empty document, established empty references and
completed phases. Documents accounted for by indented/fenced code with known
info semantics, HTML blocks, thematic breaks, empty ATX headings and blanks also
complete, including empty quotes, lists of empty items, and quotes/lists containing
those established inline-free blocks, nested in either kind of container.
They retain exact source-marker spans, empty references and vacuous inline work.
Other valid input, including lists requiring reference or inline interpretation,
yields a partial root with staged source-bound block children where recognized,
and explicit whole-document `syntax-unimplemented` uncertainty. A private stack handles
continuation, thematic/heading interruption, blank closure and EOF. Paragraph
and heading segments retain original-byte provenance for later inline parsing.
ATX opening/closing markers stay separate from ASCII-trimmed raw content;
invalid openers take the paragraph path when no other provider applies.
Valid Setext underlines promote the entire eligible paragraph before thematic
dispatch, with separate content/underline spans and complete physical envelopes.
Failed equals/dash underlines fall back to paragraphs where no other provider
applies; valid leading definitions are removed before Setext promotion.
Promotion acquires final content once and resumes following blocks. Closed
paragraphs and nonempty headings have complete block/reference work but pending
inline interpretation. Reference-only input with known semantics completes;
undecoded destination/title or fence-info semantics keep Blocks pending and
Inlines not started, with available records/lookup. See the model contract for
exact envelopes, trimming, hard-break preservation and phase availability.

Invalid UTF-8 retains the snapshot with typed failure, an authoritative
snapshot-bound byte location and unavailable AST. Cancellation/deadline remain
separate incomplete outcomes. Empty children or nil failure alone never establish
complete parsing. No paragraph HTML or full dialect conformance is claimed.
See the [parser model contract](../internal/parser/model.md) for the outcome
example, core kind/property catalog, segmented raw/semantic provenance, absent
versus zero-width ranges, deterministic traversal and reference-record ownership.
Constructed model fixtures verify the full catalog independently of actual parser
recognition. Direct examples 219–227 and independent Parse AST/source assertions
verify staged paragraph progress, with later-dependent structures still pending.
Original thematic examples 43–61 and independently asserted cases 11/98 verify
recognition, precedence and exact marker/envelope ranges. All ATX examples 62–79
and existing paragraph case 227 have independent structural/source assertions.
Empty-heading 79 now passes actual Parse/Evaluate; nonempty headings stay pending.
All Setext examples 80–106 and thematic 59 have independent Parse/source
assertions. Nineteen Setext cases establish structural/fallback progress; 98
remains a supported pair of breaks. Code cases 85/100 pass. Quotes 92/101 now
precede outside thematic breaks; 93 remains lazy paragraph text. List cases 94/99 retain item paragraphs and outside breaks;
inline work remains pending. Example 91 retains its
initial heading/paragraph plus Setext/paragraph structure in the invalid multiline
tag suffix; inline text/code-span/inline-HTML work remains pending.
Those deferrals are explicitly recorded, not claimed as recognized headings.
Indented code retains original-byte envelopes, indentation markers and content
separately from normalized Literal; code contents bypass inline parsing. Blank
runs belong to code only with a following code line, and a dedent resumes normal
block dispatch. Actual retained 107–118 and cross-feature code targets have
independent AST/source assertions, supplemented by tab/endings/EOF, NUL/Unicode,
blank/whitespace preservation, immutable ownership and outcome fixtures.
Fenced code recognizes backticks/tildes, paragraph interruption, matching closers
and valid unclosed EOF, preserving original tokens, raw info and literal payload.
All 119–147 plus cross-feature 19 have independent actual Parse/source assertions;
23 fenced-section cases pass HTML, including indented-code 134. Empty and blank
payloads, source line endings, partial tabs/NUL/Unicode, ownership and interruption
have separate fixtures. Ordinary info semantics are available; punctuation escapes
and plausible character references remain explicitly unavailable pending #19.
Unknown names are not guessed decoded text. Mixed unsupported documents stay
partial; code payload never defines references or inline syntax. See the model
contract for the exact source/semantic and EOF conventions.
All seven HTML-block forms now preserve opaque source content and form identity.
Forms 1–6 interrupt paragraphs; form 7 does not. Fixed-form continuation, complete
terminating lines, blank-line/EOF closure and conservative mixed/interrupted
outcomes have actual Parse/source tests for all 148–191 plus cross-feature 21/31.
Twenty-five HTML-section cases pass; 19 retain genuine later providers. Original
HTML line endings stay raw; mandatory NUL substitution remains semantic only.
Nested block quotes now match containers before leaf continuation, attach leaves
to their actual parent, preserve segmented original-source content and support
paragraph-only lazy continuation. All unchanged 228–252 have independent actual
Parse/source assertions, including omitted nested markers, blank boundaries and
code/HTML integration. Earlier tab 6 and 236/239/240 add four passes, preserving
all 80 prior passes. Additional fixtures cover seven-form HTML quote-end closure,
residual tabs, NUL/Unicode/endings/EOF, immutable accessors and deterministic nested
interruption without normal-EOF publication. These four quote-related passes
remain part of the current supported baseline.
List items now retain ordered nested block children and established multiline
ownership/closure in mixed quote/item containers. Effective indentation keeps the
original tab phase; paragraph-only laziness can omit prefixes. Blank-start/empty
items, dedented siblings/outside blocks and recursive same-line starts have
independent actual-Parse assertions. Existing marker/ordinal/padding/ContentIndent
and Prefix facts survive closure. `item-indent` and `item-blank` markers preserve
original continuation/boundary evidence for grouping; leaf content remains segmented
on the original Source. Code/HTML stays opaque. Interruption discards unfinished
items instead of assigning normal EOF closure.

Compatible siblings now acquire immutable List parents within document, quote and
item owners. Marker/delimiter changes and intervening blocks split groups; initial
ordered starts do not erase item spellings. Tightness follows direct item/block
blank boundaries, with each nested list independent. Code/quote-internal blanks
and loose sublists do not alone loosen parents. Paragraph nodes remain available;
the test renderer suppresses only the appropriate direct paragraph tags.
See the [item/list contract and per-case
providers](../internal/parser/model.md#list-markers-continuation-and-staged-grouping).

Every unchanged 253–300 input retains independent item/leaf source assertions
through the new List wrappers. Every Lists input 301–326 additionally has explicit
available List ancestry, tightness, start/type, exact envelopes/segments and marker
range expectations. Non-rendered definition boundaries preserve item separation:
317 retains all three items and outer Tight=false. Empty/definition-only items
and established inline-free blocks complete; text-dependent cases retain pending
inline interpretation with available references. Tabs 7 and empty item 284 now pass actual Parse/Evaluate,
reaching the previous 86-pass baseline. Definition-only 207 now raises it to
87 pass and 565 pending, preserving all 86 earlier passes.

Focused fixtures cover original source facts, mixed inner/outer tightness, quotes,
opaque internal/trailing blanks, empty-item gaps, all endings/EOF, tab provenance,
Unicode/NUL, immutable accessors and deterministic interruption. Existing marker
and continuation fuzz targets retain their independent ownership/source oracles;
FuzzListGroupingBoundaries generates sibling marker changes, blank separation and
quote/ending variants with independently expected group/start/tightness facts.
These bounded targets do not establish full list conformance or comprehensive
#31 work/depth-budget protection.
Go tests check complete unique inventory accounting as well as ownership,
range/provenance, outcome and phase-handoff behavior.

Keep file loading, configuration, rules, CLI/MCP adaptation, reporting and writes
outside the parser. Initial Go packages remain internal. The
[engineering design](engineering.md#shared-engine-and-package-boundaries)
describes the responsibilities planned for the other packages.

The [test-only conformance runner](#run-commonmark-regression-checks) is available;
other block recognizers, inline text and reference definitions remain later
deliveries. Context is shared across phases; full budget metering/enforcement and cancellation hardening remain
[#31](https://github.com/frithuz/mdchk/issues/31). Public positions mean one-based
lines/Unicode code-point columns, tabs count as one column, with exclusive ends.
Syntax deliveries must use the pinned upstream cases plus independent AST/source-span
checks and ratchet actual passing
cases. No CommonMark, GFM or Pandoc conformance is claimed here; full corpus
completion still requires every case without exemptions.

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
targets retain their pending block/inline owners.

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

Every unfiltered run currently reports **87 pass, 0 fail, 0 error, 565
pending**. Supported IDs are 1–3, 6–8, 11, 18–19, 21, 31, 36, 43, 47–48, 50–54,
69, 79, 85, 98, 100, 107, 110–112, 116–120, 122–127, 129–137, 139, 142–144,
146–147, 150–151, 153–154, 156–166, 171, 173, 178, 181, 183–184, 186,
189–191, 207, 231, 236, 239–240, 284 and 289: actual inline-free documents with
indented/fenced code, HTML, breaks, empty ATX headings, blanks, quotes and lists.
Grouping previously added 7/284; definition-only 207 now adds a pass while
retaining all 86 earlier IDs.
It does not establish general inlines, extensions or full CommonMark conformance.
Indented-code 108/109 retain #18 reference/inline owners;
113 retains #18/#21, and 114/115 retain #18. Paragraph 225 now retains
its code with remaining #18; other paragraph cases retain actual #18/#21
owners. Named thematic targets retain reference/inline/break/emphasis owners.
ATX targets retain #18 for text/reference handoff, #19 for escapes (65/66/76),
#24/#25 for emphasis (66) and #21 for multiline content (70). Setext targets
retain #18 for 92/101, additionally #21 for 93, #18 for 94/99 and
#18/#20/#23 for 91.
Recognized Setext/fallback cases and 59 retain #18, plus actual multiline
(#21), escape (102/106, #19) and emphasis (80–82, #24/#25) owners. Setext cannot
be empty; its nonempty headings remain pending. Fenced 128 retains #18;
121 retains #18/#20; 138/145 additionally retain #21; 140/141 retain #18
for surrounding paragraphs/headings. Info decoding remains #19. HTML 174 retains
#18, 175 retains #18, and mixed HTML cases retain their actual reference/text,
break, inline-HTML or emphasis providers (#18/#21/#23–#25). Other cases use
broad remaining inline/core-completion scopes #18/#29. Quote 235 retains
#18; other pending quote cases retain #18, plus #21 for multiline text.
Lists 301–326 retain #18, with #21 additionally for multiline 304/312.
Reference cases 214/218 retain #18/#27. These coverage owners are not reverse
prerequisites on this staged delivery. Pending cases are not conformance passes. Independent constructed AST
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
accounts for all 652 IDs exactly once. Its supported list records the 87
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
pending category; use `--number 208` to observe an unsupported case as an error. The adapter's default protocol
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


### Reference definitions and Unicode folding

Leading definitions are collected document-wide before inline resolution, outside
AST child traversal. All duplicate/unused records retain original components and
markers; normalized lookup selects the earliest. Invalid candidates and ordinary
remainders retain their paragraph/Setext/container structure. Actual Parse tests
exercise every unchanged definition example 192–218, plus list 317 and independent
boundary/source, full-fold, ownership and deterministic interruption fixtures.
Only 207 becomes a new HTML pass; the remaining definition examples retain exact
inline/link/decoding providers in `foundation.json`, not #17 or a blanket exemption.

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
