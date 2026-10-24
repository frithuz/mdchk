# Frontmatter boundary fixtures

`boundaries.json` is the reusable lexical recognition matrix for #166 and later
opaque-block/consumer integration. Every case has a unique `id`, original
`input`, the independent `enabled`/`formats` settings, and a `want` result.

`want: null` means no recognized header. Otherwise `format` is `yaml` or `toml`
and ranges are zero-based, half-open original UTF-8 byte offsets:

- `region` includes a preserved BOM and all header line endings.
- `opener` and `closer` retain marker-line trailing spaces/tabs, without BOM or
  line endings.
- `content` excludes delimiter lines and retains payload endings.
- `closer: null` means a recognized unterminated header through EOF, including
  an opener alone at EOF. An empty content range is still present.

Fixtures distinguish missing/disabled recognition from unterminated recognition,
exercise YAML/TOML subsets, and pin BOM/start restrictions, exact marker spelling,
matching and mismatched closers, LF/CRLF/CR/mixed/EOF endings, malformed metadata,
later header-looking body text and deferred JSON. Recognition is lexical and
does not decode or validate metadata values.

`TestFrontmatterBoundaryFixtures` uses the same matrix alongside CommonMark and
GFM. The private helper deliberately takes no flavor parameter. The production
parser assertions currently prove ordinary interpretation remains unchanged;
opaque AST recognition, protection and inspection are separate deliveries.
