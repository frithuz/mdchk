# Result schema and fixtures

The accepted schema-1 producer contract uses JSON Schema 2020-12. The
[semantic contract](../../engineering/results.md) supplies cross-record,
byte-identity, completeness, and operation-specific requirements beyond JSON Schema.
`fixtures.json` identifies the 17 result/projection fixtures and expected CLI exits.

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
