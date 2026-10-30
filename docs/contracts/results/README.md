# Result schema and fixtures

The accepted schema-1 producer contract uses JSON Schema 2020-12. The
[semantic contract](../../engineering/results.md) supplies cross-record,
byte-identity, completeness, and operation-specific requirements beyond JSON Schema.
`fixtures.json` identifies the result/projection fixtures and expected CLI exits.

Validate in an isolated Python environment:

```sh
python3 -m venv /tmp/mdchk-contract-validation
/tmp/mdchk-contract-validation/bin/pip install -r docs/contracts/results/requirements.txt
/tmp/mdchk-contract-validation/bin/python docs/contracts/results/validate.py
```

Validation includes twelve deliberately invalid mutations. It checks serialization
and representative semantic invariants; it does not execute mdchk or prove
parser, fixer, transport, authentication, or platform conformance.

Lint, config validate, inspect CLI and inspection package producer tests also validate the actual emitted CLI JSON with this same
schema and semantic validator. Enable the isolated interpreter explicitly:

```sh
MDCHK_RESULT_VALIDATOR_PYTHON=/tmp/mdchk-contract-validation/bin/python go test ./...
```

This checks implemented lint/config-validation/inspection envelopes and the inspection
package projection, including source coverage, bounded report omissions and exact
suppression ownership. Other operations retain their separate acceptance gates.
The optional additions and current CommonMark property catalog are documented in
[inspection projection version 1](../../engineering/inspection-projection-v1.md).

Lint directive integration populates `notes` and `suppressed_diagnostics`. The
semantic validator includes nested suppressed diagnostic IDs, source ranges and
original file snapshots. Lint records omit `directive_id` because the operation
has no serialized directive collection. Exact ownership and permission origins
remain in immutable shared analysis observations; lint notes identify permission
fields without exposing native configuration paths. Whole-record omissions in
either collection make the result incomplete, with exact retained/omitted counts.

The opaque frontmatter projection fixture retains original YAML delimiter and
payload ranges separately from body nodes. Actual inspection producer tests also
reject mutated header regions outside the snapshot, inconsistent body offsets,
and contradictory termination. This is source metadata, not structured decoding.


The delivered document-context projection retains the existing `slides`,
`matched_slide_patterns` and `frontmatter` fields. Matching records at most the first
configured pattern. Optional `slide_match` adds its exact index and origin without
changing schema version or granting metadata/resource authority. Unresolved context
uses section availability rather than a guessed false or absent value.

The suppression-audit directive fixture shows independent per-target reasons
while retaining all duplicate named tokens and ordinary permission/effect
state. Actual MD087 producer tests validate both the closed schema and semantic
owner/token/state joins for full and selected inspection. Negative mutations
reject invented reasons, blocked-effect conclusions, missing first tokens,
wrong self exclusion and missing disable ownership. Existing non-audit reason
strings retain their open explanation grammar.
