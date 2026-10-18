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
completed phases. Documents accounted for by indented code, thematic breaks,
empty ATX headings and blanks also complete with exact source-marker spans, empty
references and vacuous inline work. Other valid input yields a partial root
with staged paragraph/break/heading children where recognized, and explicit
whole-document `syntax-unimplemented` uncertainty. A private stack handles
continuation, thematic/heading interruption, blank closure and EOF. Paragraph
and heading segments retain original-byte provenance for later inline parsing.
ATX opening/closing markers stay separate from ASCII-trimmed raw content;
invalid openers take the paragraph path when no other provider applies.
Valid Setext underlines promote the entire eligible paragraph before thematic
dispatch, with separate content/underline spans and complete physical envelopes.
Failed equals/dash underlines fall back to paragraphs where no other provider
applies; potential list and other unsupported starts still defer their suffix.
Promotion acquires final content once and resumes following blocks. Closed
paragraphs and nonempty headings remain partial: references and inline
interpretation are unavailable. See the model contract for exact envelopes, content trimming,
interior hard-break preservation and conservative start guards.

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
assertions. Eighteen Setext cases establish structural/fallback progress; 98
remains a supported pair of breaks. Code cases 85/100 now pass. Six cases remain
source-only: quotes 92/93/101, lists 94/99 and 91's conservative fence/HTML guards.
Those deferrals are explicitly recorded, not claimed as recognized headings.
Indented code retains original-byte envelopes, indentation markers and content
separately from normalized Literal; code contents bypass inline parsing. Blank
runs belong to code only with a following code line, and a dedent resumes normal
block dispatch. Actual retained 107–118 and cross-feature code targets have
independent AST/source assertions, supplemented by tab/endings/EOF, NUL/Unicode,
blank/whitespace preservation, immutable ownership and outcome fixtures.
Thirty upstream cases pass actual HTML comparison; 622 remain pending.
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

Every unfiltered run currently reports **30 pass, 0 fail, 0 error, 622
pending**. Supported IDs are 1–3, 8, 11, 18, 36, 43, 47–48, 50–54, 69, 79, 85,
98, 100, 107, 110–112, 116–118, 134, 231 and 289: actual inline-free documents
with indented code, breaks, empty ATX headings and blanks. The twenty new code
passes do not establish general inline, fenced-code, quote or list support.
Indented-code 108/109 retain #14–#18 list/container/reference/inline owners;
113 retains #17/#18/#21, and 114/115 retain #17/#18. Paragraph 225 now retains
its code with remaining #17/#18; other paragraph cases retain actual #17/#18/#21
owners. Named thematic targets retain list/reference/inline/break/emphasis owners.
ATX targets retain #17/#18 for text/reference handoff, #19 for escapes (65/66/76),
#24/#25 for emphasis (66) and #21 for multiline content (70). Setext targets
retain #13 for 92/93/101, #14–#16 for 94/99 and #11/#12 classification for 91.
Recognized Setext/fallback cases and 59 retain #17/#18, plus actual multiline
(#21), escape (102/106, #19) and emphasis (80–82, #24/#25) owners. Setext cannot
be empty; its nonempty headings remain pending. Other cases use broad remaining
inline/core-completion scopes #18/#29. These coverage owners are not reverse
prerequisites on this staged delivery. Pending cases are not conformance passes. Independent constructed AST
tests verify renderer output for the core model catalog, escaping/URL
encoding, raw segments, lists and image labels without claiming that Parse
produced those trees. Malformed UTF-8, adapter execution/protocol failures and
interruption cannot become syntax-pending. Raw HTML output preserves its
spelling except for CommonMark's mandatory NUL to U+FFFD substitution;
original source bytes/spans remain unchanged. Unsupported or unknown
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
accounts for all 652 IDs exactly once. Its supported list records the thirty
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

The last comparison intentionally exits 1 and reports one **error**, because the
unchanged upstream helper has no pending category. The adapter's default protocol
emits HTML alone on stdout only for complete rendered input. Pending input exits 2
with an outcome on stderr, errors exit 1, and interruptions exit 3. Upstream maps
all nonzero exits to error, including pending; use mdchk's runner for staged
accounting. A complete empty input succeeds with empty stdout, while example 207
must not falsely pass against its legitimately empty expected HTML. The retained
helper splits `--program` on whitespace, so use an executable path without spaces
for that helper. mdchk's runner uses an argument vector and accepts spaced paths.

The adapter's `--json` protocol is for the test runner: transport success exits 0
with a versioned `rendered`, `pending`, `error` or `interrupted` outcome. That exit
alone says nothing about conformance. HTML is meaningful only for `rendered`.
The focused Go/Python tests exercise actual empty/pending/encoding outcomes,
retained-helper success and execution-error paths, both comparison modes, case
207, filter errors, and simulated lost/new passes. Synthetic transport fixtures
are separate from the immutable upstream corpus.

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
