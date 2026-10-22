# Original published GFM corpus

This directory retains the exact published GFM `0.29-gfm (2019-04-06)` page,
508,310 bytes, SHA-256
`b153d814fdfc8624bb6da7449162c1cd707a637f7d1c1b636eb44b9cf63fa220`.
The [approved compatibility contract](../../../../../docs/engineering/gfm-compatibility.md)
uses CommonMark 0.31.2 plus all five published extensions. This delivery supplies
corpus infrastructure only. It establishes no GFM parser capability, conformance,
profile adaptations, or GitHub website rendering parity. Configuration still
rejects unavailable GFM, including the default. The existing CommonMark 652-case
pin and tooling remain separate and unchanged.

`upstream/spec.html` is the unmodified normative page from
<https://github.github.io/gfm/>. The approved pin was retrieved on 2026-10-14;
these identical bytes were independently retrieved again on 2026-10-22. No matching
source commit is established: the content digest is the revision. The label alone
is not a pin. `provenance.json` records both dates and each artifact's origin,
size, digest, role, license and modifications. A null page repository/commit is
intentional; the comparison implementation's commit does not identify this page.

`examples.json` retains all 677 consecutive original IDs, including both disabled
task examples. There are 649 base cases and 28 extension cases: tables 198–205,
task list items 279–280, strikethrough 491–493, extended autolinks 622–635, and
disallowed raw HTML 657. There are no skips, automatic-success expectations,
footnotes or comparison-renderer substitutions. Original HTML expectations are
immutable; newer-core and integrated-profile adaptations belong to separate
future metadata, as specified by issue 228.

Each record contains the original `example`, `section`, `markdown`, `html`,
`start_line`, `end_line`, plus `snapshot_sha256`, `input_sha256`, and `scope`.
Stable identity is the pair `(snapshot_sha256, example)`, never the number alone.
The input hash covers the exact extracted Markdown encoded as UTF-8, including
its terminal LF. Line ranges are inclusive outer example-container lines in the
retained HTML (example 1:526–539, 279:6686–6702, 677:13650–13662), not source spans
in a parsed Markdown document. Sections follow actual numbered headings rather
than the table of contents or escaped example content.

The stdlib HTMLParser extractor tracks nested containers and code fields. It
removes only real `span class="space"` presentation wrappers while preserving
their text. It decodes presentation entities exactly once and converts the
published right-arrow tab notation to tabs in both fields. It does not trim,
normalize HTML, render Markdown, or modify terminal newlines. Empty expected HTML
in 176 and 188 is valid; missing fields are errors. Escaped tags produced by entity
decoding remain literal data and are never parsed again. Malformed/truncated
containers, unsupported presentation tags and duplicate fields are rejected.

Use Python 3.9+ with only its standard library from the repository root:

```sh
python3 -B tools/gfm_corpus.py
python3 -B tools/gfm_corpus.py --check
python3 -B -m unittest discover -s tools -p 'test_gfm_corpus.py' -v
```

Paths resolve relative to the tool, so an absolute tool path also works from an
unrelated working directory. Neither command downloads anything, invokes an
external renderer or reads a developer-local snapshot. Regeneration writes only
`examples.json`, even if absent. Check mode writes nothing on success or failure.
It verifies the fixed approved source size/hash before extraction, verifies every
retained input and extractor against provenance, runs extraction twice, checks
the pinned generated size/hash, then compares exact output bytes. Restore damaged
retained inputs from their approved source; no command updates pins or repairs
inputs. Errors go to stderr with exit 1; status is separate from generated JSON.
Git attributes preserve the pinned input/output bytes and the extractor LF
on checkout. The unmodified page contains two intentional trailing-whitespace
lines, so only that exact retained artifact is exempt from whitespace checks.
The generator version, source digest and exact JSON serialization are recorded
separately in provenance. Tests exercise synthetic structural failures behind
the production digest gate, complete independent extraction, network-denied
regeneration, drift rejection and complete file inventory/bytes/mtime stability.
CI explicitly runs both GFM drift and ingestion tests alongside CommonMark.

## Attribution and retained notices

The published specification is based on the CommonMark Spec by John MacFarlane,
licensed CC-BY-SA 4.0; the original notice and license link remain in
`upstream/spec.html` lines 120–128. Derived example data remains under
[CC-BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/), attributed to
John MacFarlane and the published GitHub Flavored Markdown specification.
Extraction and the added metadata are the modifications described above and in
provenance; the original inputs and expected HTML have not been adapted.

`upstream/cmark-gfm-COPYING` retains the original notices from comparison release
`0.29.0.gfm.13`, commit `587a12bb54d95ac37241377e6ddc93ea0e45439b`.
It is a notice artifact, not the source page or an extractor. Its distinct terms
cover CC-BY-SA specification data, BSD-style software (John MacFarlane), and
MIT-derived normalization material (Karl Dubost). No cmark-gfm Python software
was imported. The existing CommonMark `upstream/LICENSE` and Python helper
provenance remain intact at `../../commonmark/0.31.2/`; that separate pipeline
retains its BSD and MIT terms. No blanket software license is assigned to this
specification or derived corpus.
