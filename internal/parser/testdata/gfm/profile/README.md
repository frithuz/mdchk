# Approved GFM profile fixtures

These fixtures encode CommonMark 0.31.2 plus all five published GFM extensions.
They validate expected data and source contracts. The GFM parser and consumer
integration remain unavailable; configuration continues to reject unavailable GFM.
The separate actual CommonMark conformance gate remains 652 unchanged cases.

`ledger.json` resolves every one of the 677 retained published cases under three
explicit scopes: original section scope, modern core, and integrated GFM. Stable
identity retains snapshot digest, upstream ID, original section/scope and exact
input digest. An `original` reference resolves only to that case's immutable HTML;
a `fixture` reference resolves to the declared scope in `expectations.json`.
All 28 extension cases have ordinary core expectations as well as integrated
expectations. Section scopes are internal metadata, not public extension toggles.
Empty original HTML in 176 and 188 is valid.

`expectations.json` contains independently authored literal output, reason,
governing section and exact source projections for every changed interpretation,
plus non-adoption and supplemental regressions. The selected projections are
required ordered node facts, not exhaustive whole production trees. A document
projection asserts the source envelope only. Core projections do not pretend that
extension nodes were recognized. Future adapters must publish the required
projection from actual syntax while independently rejecting extra forbidden
extension recognition in core mode. No fake production AST is constructed here.

Each node has a typed kind, `[start,end,raw]` UTF-8 byte envelope, ordered original
content segments, ordered child projections and kind-specific `facts`. Byte
boundaries, exact slices, containment, source order and discontinuous content are
checked separately from HTML. Paragraph content excludes its terminal block
ending; definitions remain separate from paragraph/use ranges. Container payload
belongs to child syntax, while fenced-code content excludes its fences. Empty
present ranges differ from `null` absence.
One-based public positions count Unicode scalars and tabs as one; CRLF is one
line ending. Containers retain their original prefixes separately.

- Table rows retain header/delimiter/body roles, alignment, outer and actual
  separator ranges, escaped pipes and complete original cell fields (including
  whitespace and excess cells). Semantic cell values are distinct from source
  fields. Padding is spanless. Mismatch 203 is ordinary prose; unescaped code
  pipes split, while 200 retains the backslash in original escaped cells.
- Task facts identify the owning item, first paragraph, original bracket/check
  spelling, separator, list marker, tightness and checked state. The uppercase
  CRLF fixture includes literal UTF-8 offsets and public positions.
- Strike facts preserve one/two-tilde delimiters and arities, content and nested
  children. Unmatched/mixed/triple runs, paragraph gaps and escapes retain
  ordinary meanings.
- Links distinguish bare GFM, angle, inline and reference origins, visible
  bytes and destination provenance. Added `http://`/`mailto:` prefixes and
  projected reference values are spanless. Reference links retain a separate
  definition/label/destination binding, outside the use envelope. The fixture for published 615 records
  the explicit raw-source maximal email match priority against a competing core
  escape. The pinned extension grammar does not expressly order those rules;
  this is an explicit reviewed-fixture interpretation, not a renderer oracle.
  Core 615 retains its escaped-plus text meaning independently.
- Filter facts identify all nine case-insensitive names, exact opening/closing
  affected `<` ranges, boundaries and protected HTML envelopes. Each protected
  range equals a separately asserted raw HTML owner; inline protection covers
  the tag construct and literal block protection stops at the closing-tag line.
  Payload remains
  raw, including directive-looking text. Comments and code have no filter facts.

Modern-core witnesses cover textarea across blank lines, search interruption,
source exclusion, lowercase declarations, VT/FF/control/space/tab distinctions,
Unicode-symbol emphasis, reference folding, balanced link destinations and entity
clarifications. Numeric references preserve their Unicode scalar (U+0080 stays
U+0080), replacing NUL/invalid scalars without HTML-browser remapping. They refer directly to pinned governing text, not same-number
examples from another edition. Paired fixtures include combined features,
containers, LF/CRLF, tabs and UTF-8. Frontmatter is explicitly independently
**disabled** in every fixture; the frontmatter-looking document receives ordinary
selected-dialect interpretation.

The nested-strong, invalid-entity and xmpp comparison-program differences are
explicitly not adopted. Independent literal tests check their output and source
boundaries. No comparison renderer supplies any expectation.

Run from the repository root (Python 3.9+, standard library only):

```sh
python3 -B tools/gfm_profile.py
python3 -B tools/gfm_profile.py --check
python3 -B tools/gfm_profile.py --check --json
python3 -B -m unittest discover -s tools -p 'test_gfm_profile.py' -v
go test ./internal/parser -run TestGFMProfile -count=1
python3 -B tools/gfm_profile.py --require-complete
```

Generation writes only `ledger.json`; authored expectations are never inferred
from parser output or regenerated by the tool. Validation uses retained inputs
without network, cmark or developer-local paths, and works from unrelated CWDs.
Check mode is read-only on success and failure. Rejection diagnostics identify
the fixture, interpretation scope and nested node path while retaining the
semantic assertion reason. Provenance verifies per-artifact
hashes as well as semantic checks; tests exercise semantics beyond the hash gate.

Machine-readable `expected_validity` is separate from `actual_consumers`. There
are 2,031 published scope units (677 × 3) plus two units per supplemental fixture.
Published source fixtures are not double-counted. Every current consumer unit is
explicitly `not-run`: fixture integrity is not a GFM conformance pass. The future
consumer comparison API separately reports pass/fail/error/not-run/unimplemented;
wrong HTML, wrong source structure, missing scopes and subsets cannot establish
complete conformance. Fake consumers in tests exercise these refusals only.
`--require-complete` currently exits 1 because no GFM consumer integration ran.
It never skips or counts pending data as a pass. Phase 09 owns real consumers and
extension recognizers; this delivery activates no production behavior.

## Attribution

`expectations.json` and `ledger.json` derive from the published GFM specification
(based on John MacFarlane's CommonMark Spec) and CommonMark 0.31.2, under
CC-BY-SA 4.0. Modifications are the separately authored modern-core/integrated
HTML, source/structure expectations, adaptations, supplemental examples and
identity/reference metadata described above. `provenance.json` records each
artifact's role, license, origin/pins, modifications and digest. The original
GFM page, examples and provenance remain byte-for-byte unchanged. Original
notices remain in `../0.29-gfm/upstream/spec.html`, its distinct cmark-gfm COPYING,
and `../../commonmark/0.31.2/upstream/LICENSE`; their separate software terms
remain intact. No comparison software or new runtime dependency was imported.
