# Approved GFM profile fixtures

These fixtures encode CommonMark 0.31.2 plus all five published GFM extensions.
They validate expected data and source contracts. The test-only tagged adapter
executes the real parser lifecycle. Production GFM selection now uses that same
whole profile through lint, inspection and fix; named missing rules remain
unavailable independently of syntax support.
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
extension nodes were recognized. The adapter publishes the required
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

Machine-readable `expected_validity` is separate from actual conformance.
`--check` validates fixtures and reports its 2,117 consumer units as `not-run`;
this integrity-only command never claims execution. The legacy exact
`consumer_result`/`results` APIs and `--require-complete` refusal retain their
meaning. Fake consumers in tests exercise rejection only.

## Execute the actual adapter offline

Go 1.27.2+ and Python 3.9+ are required, with the declared Go module dependencies
already available for an offline build. No cmark executable, network, external
spec checkout or Working-directory artifact is needed:

```sh
go build -tags mdchk_conformance -o /tmp/mdchk-specadapter ./internal/conformance/cmd/specadapter
python3 -B tools/gfm_profile.py --run --adapter /tmp/mdchk-specadapter --json
python3 -B tools/gfm_profile.py --run --require-approved-profile
python3 -B tools/gfm_profile.py --run --examples 198,649 --scope original
python3 -B tools/gfm_profile.py --run --section 'Tables (extension)'
python3 -B tools/gfm_profile.py --run --fixture combined-all-five
go test -tags mdchk_conformance ./internal/conformance/...
python3 -B -m unittest discover -s tools -p 'test_gfm_conformance.py' -v
```

`tools/gfm_conformance.py` is the same actual-run entry point. Without `--adapter`
it builds a temporary tagged executable from this checkout. Relative prebuilt
adapter paths resolve from the invocation directory; corpus paths resolve from
the retained checkout, independently of CWD. Filters combine by intersection and
unknown or empty selections fail. Inventory validation always precedes filtering.

The closed test-build scopes are core, tables-only, tasks-only, strike-only,
bare-only, filter-only and integrated. They reuse the same parser lifecycle and
finite operation; ordinary untagged Parse and product builds remain core-only.
Original base units execute modern core; original extension units execute only
their named section extension. Core units execute core, and integrated units
execute all five. No public configuration extension switches are added.

Every run reports selected/executed/filtered counts against the complete 2,117
units: 677 published inputs including 28 extensions, each under three scopes,
plus 43 supplemental fixtures under two scopes. Published source fixtures attach
to existing units. Each unit carries source pin/hash/section/lines, execution
scope, immutable raw reference, approved target reference and adaptation reason.
The actual catalog comes only from parser syntax/metadata and completed
same-operation protection consumers. The matcher selects ordered exact witnesses
with real ancestry; empty fixture content/children are unasserted. Actual content
coalesces adjacent source spans for this assertion view; transformations remain
explicit in every actual fact's mappings, text/link parts and typed table/filter
facts. Original marker roles are retained alongside that mapping inventory. Disabled extensions are
checked over the complete actual catalog, including document-only expectations.

Strict deterministic HTML equality applies, with raw byte spellings and unified
HTML/fact differences retained. There is no GFM HTML normalization. Per-axis
pass/fail/error/pending counts are explicit. `raw_declared` compares immutable
historical expectations; `approved_target` selects an original base unit's
reviewed core reference before execution, and otherwise retains its declared
scope. Therefore original 649/650 honestly fail raw historical equality while
passing their approved modern comment targets. No fixture is rewritten, skipped
or automatically accepted. The legacy `conformance_complete` and explicit
`raw_historical_conformance_complete` require all 2,117 raw passes.
`approved_profile_conformance_complete` and `--require-approved-profile` require
all 2,117 approved target passes with no errors/pending/missing units. A subset
can establish neither completion. `--run --require-complete` gates historical raw
equality and therefore fails on the deliberate modern-comment differences.

The tagged executable admits at most 1 MiB before copying into Source, and the
actual-export protocol additionally bounds source coordinates to 16,384 bytes,
20,000 facts and 16 MiB output. The runner also bounds retained actual responses
to 32 MiB cumulatively, reporting further oversized retention as explicit errors
while preserving unit accounting. JSON rejects duplicate keys, floating/nonfinite
numbers, excessive nesting/records and invalid Unicode; typed assertions do not
equate booleans and integers. Shared sticky work/time/node accounting covers
parse, protection, rendering, coordinates, catalog copies and serialization.
These are finite application bounds, not an OS RSS or hard preemption guarantee.
Each runner subprocess has a finite timeout (default 65 seconds, maximum 120),
bounded stdout/stderr acquisition and owned process-tree cleanup. On POSIX it
owns a new process group; on Windows cleanup uses the system taskkill tree command.
Unknown protocol/kinds/outcomes, malformed responses, timeout and missing output
are explicit errors; unfinished syntax remains pending. No partial output passes.

The separately pinned optional diagnostic programs remain cmark-gfm
0.29.0.gfm.13 at 587a12bb54d95ac37241377e6ddc93ea0e45439b and cmark 0.31.2
at eec0eeba6d31189fd828314576494566d539b1e3. They are not required or invoked by
this runner and cannot supply targets. The independent parser verification suite now covers mixed AST/source/protection,
excluded syntax, scope isolation, concurrency and finite resource behavior.
`TestGFMVerified`, `FuzzGFMProfile` and `BenchmarkGFMProfile` exercise the actual
whole-profile parser independently of this adapter. See the [verification record](../../../../../docs/engineering/gfm-verification.md).
The supported default/explicit production GFM selection now feeds real lint,
fix and inspect consumers (#161). Named production rules (#236), frontmatter,
Pandoc and MCP remain separate; this profile does not implement them.

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
