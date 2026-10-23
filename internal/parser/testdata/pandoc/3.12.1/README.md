# Original Pandoc profile targets

These original mdchk fixtures freeze the approved
[Pandoc contract](../../../../../docs/engineering/pandoc-compatibility.md) and
[profile](../../../../../docs/engineering/pandoc-profile.json), revision
`pandoc-3.12.1-mdchk-1`. They specify the complete target. Integrity success
means the authored data is consistent: **actual full-profile mdchk Pandoc parsing
is NOT-RUN/unimplemented, zero passes, conformance_complete=false**. A private
[block stage](../../../model.md#pandoc-block-stage) executes 126 retained cases
(74 list/container cases plus 52 fence/attribute/Div/raw boundary cases) separately;
its partial block results do not change this full-profile ledger. Production
capability admission still rejects unavailable Pandoc with exit 12.

From the repository root, using Python 3.9+ and its standard library:

```sh
python3 -B tools/pandoc_profile.py --check
python3 -B -m unittest discover -s tools -p 'test_pandoc_profile.py' -v
python3 -B tools/pandoc_profile.py
python3 -B tools/pandoc_profile.py --check --json
```

The third command regenerates only `ledger.json`. It never writes inputs,
expectations, coverage, provenance, references or tools. All paths resolve from
the tool location, so the same absolute tool path works from unrelated working
directories. Ordinary commands need no network, Pandoc, developer `Working/`
directory, Go compiler or application parser. Python must already be installed.
CI runs integrity and the semantic mutation/offline unit suite.

`--require-complete` fails until complete selected-profile parser execution and
verification exist. There is no actual-output import or adapter that can echo
expectations into passes. The original target tooling does not run the Go block
stage; run `go test ./internal/parser -run '^TestPandoc' -count=1` for that
separate stage, which retains pending inline semantics and unavailable complete
consumer authority. Full-profile recognition, rule callbacks, fix/reparse safety,
MCP, frontmatter, Quarto, automatic IDs, slug policy and configuration switches
remain subject to their later gates.

## Authored data and source schema

`inputs/` contains 543 original UTF-8 byte fixtures, including CRLF, tabs,
composed/decomposed Unicode, and an unterminated last line. `expectations.json`
is a separately authored literal target collection. Its targets were chosen
from the accepted contract and pinned references, without running an oracle or
mdchk parser. Review these meanings and source records independently during code
review; never regenerate them from observed output. `coverage.json` owns every
one of the 74 switches, both enabled and disabled. The frozen profile has 37/37
states, 47 upstream defaults, exactly ten adopted default disables and 27
retained upstream disables. The checker also derives these sets directly from
the retained pinned extension declarations.

Each case has its profile through the collection revision, input path/hash,
extension/state, dimension, literal target, rationale, original source contract,
source assertions and typed facts. `target.structure` names required structural
families, rather than a serialized full AST; `target.meaning` specifies the
selected interpretation. Ordinary fallback targets describe actual body syntax,
including YAML-looking thematic breaks/setext headings. Excluded switches can
share an ordinary witness across positive/fallback/precedence accounting; it
positively specifies their selected absence. For example, disabling
`angle_brackets_escapable` does not defeat enabled `all_symbols_escapable`, and
`gutenberg` is a writer convention without reader behavior.

Enabled switches own positive, fallback, code protection, quote nesting, CRLF,
Unicode, precedence and explicit escape inputs. Every exclusion also owns
protection, nesting, CRLF, Unicode and an individually reviewed escape interaction. Every
dimension resolves to owned case IDs or an explicit narrowly scoped N/A rationale.
No case filtering precedes full state/case accounting. The adjacent-prose fence
exception is specifically the tested backtick form; the ordinary tilde witness
starts a document. Container-global notes retain quote, list and nested-note
physical ownership rather than adopting the manual restriction.

A span is `[start, end, original_text]`, with zero-based half-open original UTF-8
byte offsets. Empty present spans use equal offsets and `""`; null is absence.
The checker rejects out-of-bounds and scalar-interior ranges. Physical positions
are one-based scalar columns (tabs count one); CRLF interior positions fail.
Document `content` and `gaps` partition original bytes. Quote variants retain
prefix gaps separately. Each source assertion has a role, original span and
literal meaning. No semantic string length is used to infer an original range.

Typed facts retain details an oracle's JSON loses:

- Notes preserve all definitions, eligible and ineligible references, unused,
  unresolved, duplicate and empty records, exact labels, source owners and
  last-duplicate binding. Named-note references remain literal; inline-note
  references can bind. Anonymous empty and whitespace notes remain present.
- Tables retain every header/body cell independently of delimiter-derived width,
  original separator positions, protected inline pipes, plus/single-dash forms,
  spanless null padding and attached caption extent. The checker verifies the
  authored partition; it does not recognize Markdown inline constructs.
- Attributes retain ordered raw ID occurrences and last effective selection on
  headings, code, inline code, links, images, spans, divisions and tables.
- Tasks preserve list ownership, exact X/x/space marker, separator and state.
- HTML retains physical tags/origin, ordinary children and opaque
  script/style/pre/textarea payloads. Protection records separate markers,
  original payload and adjacent ordinary prose for code, math, raw and citations.

Independent literal inventories anchor whole sensitive collections and their
source occurrences. Tests remove records and mutate state/binding/ownership,
including mutations that maintain incidental inventories, to exercise semantic
relations beyond checksums. These checks prove expected-data validity, never
that a parser recognized the asserted construct. This is not a second Markdown
parser: the tool checks declarations, source slices and supplied relationships.

`ledger.json` deterministically projects all ownership and literal targets with
explicit NOT-RUN statuses. `provenance.json` verifies every original input,
authored expectation/coverage file, checker/test tool, generated ledger and six
selected upstream references/notices. To change an authored target, review its
input, source ranges, semantics, coverage and provenance together. Updating only
a digest is insufficient for inconsistent source facts. Regeneration validates
the pinned generated digest and cannot silently approve changed targets.

## Provenance and optional observations

`upstream/` retains the actual six upstream files fetched on 2026-10-23 from
revision `e51c9c6054c8f4ec5c3209d5abe10939dfe2963e`. Exact URLs, observed byte
counts, SHA-256, retrieval date, roles and licenses are in `provenance.json`.
`MANUAL.txt` and the Markdown reader match the approved digest pins. The original
`COPYING.md`, `COPYRIGHT`, and per-file Haskell notices are unmodified. The GPL
references remain isolated testdata; no Haskell implementation or tests are
compiled, executed, translated into Go or linked into the product. Original
mdchk inputs and targets are original project material; this delivery selects
no overall project software license.

The approved source archive and official macOS ARM64 oracle ZIP digests are
**expected pins only** here: their artifact bytes were not fetched or verified.
The historical 99 research inputs and five upstream comparisons are unavailable;
no recovered bytes, execution or outcomes are claimed. This corpus is new
original work, not reconstructed historical research.

An explicit optional development invocation can create a NEW observation:

```sh
python3 -B tools/pandoc_profile.py --oracle-executable /absolute/path/to/pandoc --oracle-archive /absolute/path/to/pandoc-3.12.1-arm64-macOS.zip --case footnotes-positive --oracle-output /absolute/path/to/new-observation
```

The oracle archive must match the approved ZIP digest, and the executable must
match the binary inside that ZIP and report exactly Pandoc 3.12.1. The tool uses
the full explicit reader, JSON writer, sandbox, fresh empty data directory,
columns 80 and tab stop 4, with no filters or citeproc. The new output directory
retains exact input, raw stdout/stderr and command/profile/version/executable
identity in `observation.json`; stdout/stderr are also losslessly base64 encoded
there. A nonzero exit or partial/malformed JSON fails after retaining the raw
observation. Internal document validation requires a compatible 1.23 API version
with integer components, typed metadata, and known block/inline constructors
with recursively checked payloads (including notes, lists, tables, citations and
attributes), finite numeric widths and scalar Unicode strings. The bounded
observation validator allows 128 shape-nesting levels; this is JSON interface
validation, not Markdown recognition or proof of selected-reader semantics.
Constructor shapes follow the public [pandoc-types 1.23.1.2 declarations](https://github.com/jgm/pandoc-types/blob/1.23.1.2/src/Text/Pandoc/Definition.hs),
within the pinned reader’s `>=1.23.1.2 && <1.24` dependency range.

Observation `outcome` is `success`, `nonzero`, `malformed` or `timeout`. A reader
timeout retains all available raw bytes plus input/profile/archive/binary/verified
version/actual command identity, has `valid_observation=false`, `exit=null`
(no completed process exit was observed), and `timeout_seconds=30`; the CLI
returns nonzero. Completed processes retain their actual numeric exit, including
zero with `outcome=malformed` when the JSON shape is invalid. Artifact/binary or
version-preflight failures refuse before a reader observation/output directory
is created; their diagnostics identify that pre-invocation refusal. Ordinary check cannot invoke this path. Wrong archive, binary or
version cannot become valid evidence. Optional boundary tests mock subprocess
responses and a synthetic ZIP pin solely to test these controls; they are not
real oracle execution or mdchk passes. No real oracle has been run for this
corpus. Observations never overwrite reviewed expectations.
