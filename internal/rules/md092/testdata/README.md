# MD092 fixtures

MD092 has three separate immutable root-authored literal fixture sets:

| Fixture | Cases | SHA256 |
| --- | ---: | --- |
| `md092.json` | 81 | `1a60725172f8b8e5038604145766f2c9cd9c9ce00b3acb16d8b3ca078477bb25` |
| `md092_raw_lifetime.json` | 6 | `1d5ab8daa2ce6d3315e7439f36311d22a24881aac59b9d3f3831f81a2131df32` |
| `md092_unknown.json` | 3 | `486afc71751f0ea8c3ffd8a94f7bdfb09dced1731cd36a9ded5e0ce801827aed` |

These 90 sources specify 180 CommonMark/GFM flavor rows, each exercised through
both full production lint and inspection. Fixture hashes, full ordered visible,
suppressed and MD087 findings, audit decisions, coverage/completeness, exact
original geometry/snapshot identity and unchanged bytes are asserted. The raw
supplement preserves incoming versus owned raw transitions and actual form-1
ANY4 boundaries. The unknown supplement distinguishes atomic withholding from
known opaque remainder protection. These are fixed expectations; fixture presence
and planned row counts are not evidence of execution. Never fit them to output.
