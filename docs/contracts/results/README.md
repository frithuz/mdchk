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

Validation includes ten deliberately invalid mutations. It checks serialization
and representative semantic invariants; it does not execute mdchk or prove
parser, fixer, transport, authentication, or platform conformance.
