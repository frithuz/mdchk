# MD038 fixtures

`md038.json` is the root source-authored issue-73 MD038 oracle: 60 cases with
61 distinct ranges, written from literal original bytes before the rule was
implemented. Each case has a `name`, the original `source` with its `sha256`,
the excess code-span edge whitespace as original byte `ranges`, a `why`
explaining them, and an optional `context`. The test pins the whole-file digest
and runs every case under the listed `flavors`. Expected values must never be
regenerated from production output.
