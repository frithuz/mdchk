# go-toml notice

The metadata source adapter uses `github.com/pelletier/go-toml/v2` v2.4.3,
licensed under the MIT License. The upstream license is included in this directory.

The project-owned adapter guards native entry, copies source expressions before
advancing the parser, and uses the public decoder for semantic validation. It
keeps exact native values and original source spans independently of generic Go
value conversion. See [the metadata contract](../../internal/metadata/README.md).
