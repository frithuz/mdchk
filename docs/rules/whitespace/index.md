# Whitespace rules

These rules are part of the [implementation ruleset](../index.md).

| Rule | Focus | Status |
| --- | --- | --- |
| [MD009](md009.md) | Whitespace at line ends | Analysis implemented; manual |
| [MD010](md010.md) | Tab characters | Analysis/configuration implemented |
| [MD012](md012.md) | Runs of empty lines | Analysis/configuration implemented |
| [MD013](md013.md) | Maximum line width | Analysis/configuration implemented |
| [MD027](md027.md) | Whitespace after blockquote markers | Analysis/configuration implemented; manual |
| [MD028](md028.md) | Empty lines separating blockquotes | Analysis/configuration implemented; manual |
| [MD047](md047.md) | Exactly one terminating newline | CommonMark analysis, configuration, previews and Darwin arm64+cgo/local APFS apply implemented; protected EOF remains manual |
| [MD064](md064.md) | Repeated spaces in prose | Analysis/configuration implemented; manual |
| [MD085](md085.md) | Indented paragraph continuation lines | Analysis/configuration implemented; manual |
| [MD089](md089.md) | Spacing between CJK and Latin text | Analysis/configuration implemented; manual |

MD013 maximum-line-width analysis and configuration are implemented for CommonMark and GFM, available but disabled by default. Findings are manual; [its source-width policy](md013.md) defines contribution protection and the explicit category exceptions.
