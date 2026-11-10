# MD049 fixtures

`md049.json` is the frozen root source-authored 64-case MD049 oracle, adopted
before implementation or parser probes (SHA-256
`5faab73b3e65c2dbd6c967058e7aa8666a8f12ed5d787007c666514cf9fef1b6`).
Its 123 selected flavor executions reuse 64 independent literals and 62 authored
opening ranges. Batch/reverse/concurrent cases are additional isolation checks,
not independent oracle counts. Expected output must not be regenerated from
production results.
