# mdchk mcp

Status: planned and unavailable in this build. The executable rejects `mcp`,
including with help/version flags. The syntax and examples below describe the
accepted future command contract.

The mdchk MCP server gives AI agents `lint` and `fix` tools. Use project mode to work on files in a project directory, or standalone mode to send configuration and Markdown content directly to the server.

```text
mdchk mcp (--root PATH | --standalone) [options]
```

## Options

Choose exactly one mode: `--root PATH` or `--standalone`.

| Option | Effect |
| --- | --- |
| `--limit NAME=VALUE` | Set a finite operator [resource limit](../engineering/limits.md); repeat for multiple limits |
| `--root PATH` | Work on files and infer configuration within this project root |
| `--standalone` | Accept configuration and Markdown content with each tool call |
| `--transport stdio\|http` | Choose the connection transport; defaults to `stdio` |
| `--http-policy PATH` | Load strict operator JSON for local, direct TLS or trusted-proxy HTTP deployment |
| `--listen ADDRESS` | HTTP bind address; defaults to `127.0.0.1:8080` and applies only to HTTP |

Both modes support either transport. The [global options](../index.md#global-options) also apply. `--config`, `--format`, and `--dry-run` are not MCP startup options; configuration and previews follow the selected mode's tool interface.

## Choose a mode

| Mode | Input | Configuration | Fix result |
| --- | --- | --- | --- |
| Project | Paths within a configured project root | Inferred from the project | Writes safe corrections to project files, or previews them |
| Standalone | File names and contents sent with each request | Sent with each request | Returns corrected content for the caller to save |

## Work with a project

Start the server with a project root:

```sh
mdchk mcp --root /path/to/project
```

Connect your MCP client using that command. The server communicates over standard input and output. It loads the project's `.mdchk.yaml` configuration, or uses defaults if none is present. Configuration discovery stays within the configured root.

Supply paths relative to the root. Omitting `paths` checks all selected Markdown files beneath it. For example, call `lint` with these tool arguments:

```json
{
  "paths": ["README.md", "docs/"]
}
```

Call `fix` with the same paths to apply safe corrections. Add `"dry_run": true` to preview changes without writing files. Results identify changes and any remaining findings. The server cannot access paths outside its root or write through symlinks.

## Send configuration and content

Start a standalone server for a local MCP client:

```sh
mdchk mcp --standalone
```

Or run it as a web service:

```sh
mdchk mcp --standalone --transport http --listen 127.0.0.1:8080
```

Connect an MCP client to `http://127.0.0.1:8080/mcp`. The HTTP endpoint accepts MCP requests; the examples below show tool arguments, rather than complete HTTP messages.

Each standalone call includes `config` and a nonempty `files` list. Configuration is a structured object using the settings in the [configuration guide](../configuration.md). Send `{"version": 1}` to select catalog defaults when the default dialect and directive processing are available. Known unavailable default-enabled checks remain visible as incomplete coverage. Explicitly requested unavailable capabilities are configuration errors. Each file has a relative `path` for reporting and exclusion matching, plus its Markdown `content`:

```json
{
  "config": {
    "version": 1,
    "rules": {
      "MD047": {"enabled": true}
    }
  },
  "files": [
    {"path": "README.md", "content": "# Example"}
  ]
}
```

Call `lint` with these arguments to receive findings. Call `fix` to receive corrected contents and remaining findings; in this example, the returned content is `"# Example\n"`. The server does not load or save files using the supplied names. Your client decides where to save returned content.

For `fix`, `"dry_run": true` returns a preview with proposed content and diffs. Neither standalone operation writes project files. Each request uses only its own configuration and contents, so you can use one service for multiple projects without sharing their settings.

## Supply referenced resources

Standalone requests can include an optional `resources` inventory alongside `files` and `config`:

```json
{
  "config": {
    "version": 1,
    "resources": {"index_file": "index.md", "anchor_style": "github"},
    "rules": {"MD051": {"cross_file": true}}
  },
  "files": [{"path": "README.md", "content": "[Guide](docs/guide.md#install)\n"}],
  "resources": {
    "complete": false,
    "entries": [
      {"path": "docs/guide.md", "kind": "file", "content": "# Install\n"},
      {"path": "images/logo.png", "kind": "file"},
      {"path": "empty", "kind": "directory"},
      {"path": "removed.md", "kind": "missing"},
      {"path": "private", "kind": "unavailable"}
    ]
  }
}
```

`config.resources` sets policy; request-level `resources` supplies read-only context. Entries never become lint/fix targets or returned corrected content. Every `files` entry, including excluded ones, already supplies a resource; repeating its path in `resources.entries` is an error, even with identical content.

Omitting the inventory gives a partial map seeded by `files`. `complete` defaults to false: an omitted path is unknown, not missing. Set it to true only for a full inventory of names and types under the virtual root. Explicit `missing` proves absence even in a partial map; `unavailable` marks an inaccessible subtree even in a complete map. A file entry without `content` proves existence but cannot supply anchors. Only checks needing unavailable context receive incomplete coverage.

Names must be canonical relative UTF-8 paths with `/` separators, without empty, dot, parent, backslash, or drive-prefix segments. They are case-sensitive literal names, not URLs. Reject duplicate names, contradictory ancestors, malformed Unicode, and unknown fields before analysis. The server never looks up omitted resources on its filesystem or the network. See the [resource contract](../engineering/resources.md#standalone-resource-map) for exact validation and lookup rules.

## Understand the results

Results identify checked and skipped files, findings with locations and rule names, changes, and any execution errors. Fix findings describe the corrected content; preview findings describe the proposed content. Standalone fix results include full contents for successfully returned files, including unchanged files. A response-size limit may withhold whole contents with explicit per-file outcomes and omissions. Excluded, failed or withheld files have no content entry; content strings are never truncated.

Rule violations are a completed check, even when some cannot be fixed automatically. Invalid configuration, inaccessible project files, and server failures are errors. MCP tool results carry these distinctions; individual calls do not terminate the server or return shell exit codes. See the [exit code guide](../exit-codes.md) for terminal commands.

For a project fix, an error can occur after other files have changed. Review the changes and failures before retrying. For a standalone fix, review returned contents and errors before saving files. Status `incomplete` and `complete: false` identify reduced analysis coverage; read the structured `coverage` reasons and do not treat missing results as clean checks. Operational errors take precedence and retain available coverage information.

The default HTTP service trusts local native users and binds literal loopback.
Remote access requires `--http-policy` with `direct_tls` or `proxy_tls`, an HTTPS
public URL, one OAuth issuer and operator-provisioned public signing keys. Project
lint/preview requires `mdchk:read`; apply also requires `mdchk:write`. Standalone
requires `mdchk:run`. See the [HTTP deployment contract](../engineering/http.md)
for policy examples, key provisioning, Host/Origin rules and proxy requirements.

The supported MCP revisions are `2026-07-28` (preferred) and `2025-11-25`.
Both use stateless HTTP JSON or stdio. Legacy stateless HTTP cancellation can be
ignored without a persistent session; disconnect does not establish that work or
writes stopped. Never automatically retry a project fix after a lost response.

Defaults are 1 MiB per text, 8 MiB aggregate content, 16 MiB raw request and 8 MiB
encoded response, including both structured output and JSON text fallback. See
[limits](../engineering/limits.md) for concurrency, timeouts and finite overrides.
Domain `clean`/`violations` results use `isError: false`; `incomplete`/`error` use
`isError: true` under the [shared result schema](../engineering/results.md).
