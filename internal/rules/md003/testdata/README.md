# MD003 fixtures

`md003.json` records each heading that breaks the configured `style` (empty
means the default, `consistent`) as its complete envelope in source order. Each
expected item gives six coordinates in `Range` (original byte start/end and
one-based start and exclusive end line/column), an independent literal `Source`
slice and the heading's actual `Style`. `Recognize` selects frontmatter
recognition, and `Incomplete` marks expected incomplete coverage. Tests run
both flavors.
