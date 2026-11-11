# MD095 fixtures

`md095.json` is the hand-authored MD095 oracle (SHA-256
`20677564569fb578707dfca8c2d4b09a5ad5654acf42ef516892f5180b8d540f`), read by
`fixtures_test.go` through production lint and inspect.
Its 90 records give the original source with its size and SHA-256, the
selected flavors and `options` with `enabled`, optional `allowed_hosts` and an
optional frontmatter format. Expected `findings` and `problems` carry original
byte ranges, one-based line/Unicode-scalar start and exclusive end positions
and the message; `incomplete` gives the coverage message when the destination
inventory is withheld. They cover the rule-page examples, every destination
class, reference-definition ownership, `xn--`, percent-encoded, escaped,
entity, uppercase and NFC-changed hosts, several and multi-letter flagged code
points, unflagged scripts, compatibility, digit and punctuation mappings, user
information, ports, root dots, special-scheme slash runs and backslashes, IP
literals, opaque and local destinations, the
allowlist forms, each unreliable-input reason, protected content and
frontmatter, containers and GFM table cells, and LF/CRLF twins. Its
`invalid_configurations` rows, each with the expected setting path, are read by
`internal/config/md095_test.go`. Expected results were written from the rule
contract; never regenerate them from production output.
