# mdchk mcp

The mdchk MCP server gives AI agents `lint` and `fix` tools. Use project mode to work on files in a project directory, or standalone mode to send configuration and Markdown content directly to the server.

```text
mdchk mcp (--root PATH | --standalone) [options]
```

## Options

Choose exactly one mode: `--root PATH` or `--standalone`.

| Option | Effect |
| --- | --- |
| `--root PATH` | Work on files and infer configuration within this project root |
| `--standalone` | Accept configuration and Markdown content with each tool call |
| `--transport stdio\|http` | Choose the connection transport; defaults to `stdio` |
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

Each standalone call includes `config` and a nonempty `files` list. Configuration is a structured object using the settings in the [configuration guide](../configuration.md). Send `{"version": 1}` to use default rules. Each file has a relative `path` for reporting and exclusion matching, plus its Markdown `content`:

```json
{
  "config": {
    "version": 1,
    "rules": {
      "final-newline": {"enabled": true}
    }
  },
  "files": [
    {"path": "README.md", "content": "# Example"}
  ]
}
```

Call `lint` with these arguments to receive findings. Call `fix` to receive corrected contents and remaining findings; in this example, the returned content is `"# Example\n"`. The server does not load or save files using the supplied names. Your client decides where to save returned content.

For `fix`, `"dry_run": true` returns a preview with proposed content and diffs. Neither standalone operation writes project files. Each request uses only its own configuration and contents, so you can use one service for multiple projects without sharing their settings.

## Understand the results

Results identify checked and skipped files, findings with locations and rule names, changes, and any execution errors. Fix findings describe the corrected content; preview findings describe the proposed content. Standalone fix results include the full content for each successfully processed file, including unchanged files. Excluded files appear as skipped and have no corrected content.

Rule violations are a completed check, even when some cannot be fixed automatically. Invalid configuration, inaccessible project files, and server failures are errors. MCP tool results carry these distinctions; individual calls do not terminate the server or return shell exit codes. See the [exit code guide](../exit-codes.md) for terminal commands.

For a project fix, an error can occur after other files have changed. Review the changes and failures before retrying. For a standalone fix, review returned contents and errors before saving files. If a response is marked incomplete, do not treat missing results as clean checks.

Keep the example HTTP service local. For access from other machines, configure your deployment's authenticated HTTPS endpoint. Submitted configuration and Markdown are sent to that service for processing.
