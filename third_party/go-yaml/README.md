# go-yaml notice

The configuration syntax adapter uses `github.com/goccy/go-yaml` v1.19.2,
source revision `92bc79cb5f685e999ad131473168fc45215d12d9`, under the MIT
license retained in [LICENSE](LICENSE). The pinned module's `go.mod` declares
Go 1.21.0 and no external module requirements. `go.mod` and `go.sum` pin the
module and its checksums; only `internal/config` imports the dependency.

This dependency supplies YAML tokenization, parsing and source positions. The
adapter validates syntax nodes rather than relying on the library's coercing
typed decoder. It never enables external file or directory references. Review
upstream maintenance, security advisories, license and adapter tests on upgrades.
