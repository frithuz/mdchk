# MD062 fixtures

`md062.json` is the independent issue-104 MD062 oracle (SHA-256
`fb015ce6170104d19665a9e5ddfe96738fd8b67f4982dd7b3eebf36c6f796f6b`): 78
records with original source size/SHA, selected flavors and settings, and
authored candidate byte/scalar ranges with the constant
`Whitespace in link destination.` message. They cover the page examples, image
and tab candidates, every grammar exclusion, protected code, raw HTML and
frontmatter, containers, emphasis, GFM table cells, multibyte columns and
LF/CRLF endings. Its `invalid_configurations` rows are read by the config test.
Expected results must never be regenerated from production output.
