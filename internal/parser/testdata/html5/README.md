# Named character references

`entities.json` is the unmodified authoritative WHATWG HTML entity dataset
referenced by CommonMark 0.31.2 section 2.5. It was retrieved from
<https://html.spec.whatwg.org/entities.json> on 2026-10-20. Its SHA-256 is
`d741d877ac77c4194c4ad526b5b4a19aef8dfe411ab840a466891cdbb9f362e6`.

The retained file has 2231 entries. Only the 2125 names ending in a semicolon
are accepted by CommonMark; the other 106 browser aliases are deliberately
excluded. Names and values are case-sensitive. Some values contain two Unicode
scalars. Numeric references use CommonMark's own grammar and scalar/NUL rules,
without HTML browser recovery or Windows-1252 remapping.

The copyright holder is WHATWG (Apple, Google, Mozilla, Microsoft). The unmodified
upstream [LICENSE](LICENSE), retrieved from
<https://github.com/whatwg/html/blob/main/LICENSE>, retains the CC BY 4.0 and
source-code BSD 3-Clause terms. The generated mapping derives from this dataset;
its generated header points to this attribution and license.

Regenerate or verify the generated production map offline with Python 3.9+ and
gofmt:

```sh
python3 -B tools/html_entities.py
python3 -B tools/html_entities.py --check
go test ./internal/parser -run 'NamedEntities|Decoding'
```

The generator checks the exact retained input hash and cross-checks `characters`
against `codepoints`. Go tests independently read the retained JSON, check its
hash, compare every semicolon name/value and rejected alias, and exercise actual
Parse output with exact original spans. Numeric, syntax-boundary, source-mapping
and interruption checks are separate from table generation. Production parsing
uses only the checked-in map and the Go standard library, with no file reads or
network access. No CommonMark corpus bytes or expected HTML were changed.
