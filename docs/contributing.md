# Contributing

Follow the commit message conventions in [AGENTS.md](../AGENTS.md). Commit
messages must not contain `Co-Authored-By` trailers.

## Build and test the Go foundation

Install [Go 1.27.2 or newer](https://go.dev/dl/). Go 1.27.2 is the supported
minimum, recorded in `go.mod`; CI reads that file and tests the exact minimum.
The bootstrap uses a current supported Go release with its security fixes; see
the [Go release history and support policy](https://go.dev/doc/devel/release).

From a clean checkout, run:

```sh
go build ./...
go test ./...
go vet ./...
```

On platforms that support Go's race detector, also run:

```sh
go test -race ./...
```

The module is `github.com/frithuz/mdchk`. It currently uses only the Go standard
library, with no third-party dependencies or `go.sum` entries. With Go already
installed, the commands above need no network access. To check without automatic
toolchain downloads, workspace overrides, module downloads or module-file edits:

```sh
GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly go test ./...
GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly go vet ./...
```

The [Go workflow](../.github/workflows/go.yml) runs test and vet on pushes and
pull requests. Its actions are pinned to commit IDs; module caching is disabled
while there are no external dependencies. Go package builds currently produce
no command-line executable.

### Package boundaries and current capability

`internal/parser` provides only an in-memory `Source` foundation. `NewSource`
copies the supplied bytes, and `Bytes` returns an independent copy. The zero value
is empty; nil and empty inputs have the same empty content. Source retention
does not normalize Unicode, line endings, tabs, NUL, or invalid encoding. This
holder does not perform parsing or encoding validation.

Keep file loading, configuration, rules, CLI/MCP adaptation, reporting and writes
outside the parser. Initial Go packages remain internal. The
[engineering design](engineering.md#shared-engine-and-package-boundaries)
describes the responsibilities planned for the other packages.

Bootstrap tests verify byte retention and ownership, not Markdown syntax.
[Corpus ingestion (#2)](https://github.com/frithuz/mdchk/issues/2),
[the AST/parse contract (#3)](https://github.com/frithuz/mdchk/issues/3),
[line scanning/source mapping (#4)](https://github.com/frithuz/mdchk/issues/4),
and [the conformance runner (#5)](https://github.com/frithuz/mdchk/issues/5)
are separate deliveries. Syntax work must use the pinned upstream examples plus
independent AST/source-span checks, explicitly account for pending cases and
retain the passing baseline. No CommonMark, GFM or Pandoc conformance is claimed
by this foundation; the full corpus completion gate remains unchanged.

## Activate the commit message hook

After cloning, run this from the repository root:

```sh
git config core.hooksPath .githooks
```

The executable [commit-msg hook](../.githooks/commit-msg) works immediately;
building mdchk and installing a third-party hook manager are unnecessary.
This setting replaces the hook directory for this local repository. If you
already use hooks, integrate them into that directory before switching it.

The hook uses POSIX `/bin/sh`, Git 2.22 or newer, and standard Unix utilities,
including `awk`, `cmp`, `cp`, `mv`, `tr`, `wc`, and `mktemp -d` (available on
macOS and Linux). The commit message must be a readable, writable regular text
file, and its directory must allow temporary files and atomic replacement.

On normal and amended commits, the hook silently removes all case variants
of `Co-Authored-By:` trailers and their continuation lines. It uses
[`git interpret-trailers`](https://git-scm.com/docs/git-interpret-trailers)
to distinguish trailers from subject and body text. Colon is always the
trailer separator for this policy, regardless of `trailer.separators` settings.
Other message bytes, including unrelated trailers, are preserved. Configured
trailer commands are not executed. Git still applies its usual commit message
cleanup after the hook.

If parsing, reading, writing, or replacement fails, the hook prints an error
and rejects the commit. It also rejects ambiguous messages where removing the
footer would expose a previous co-author-shaped paragraph as another trailer;
rewrite that paragraph as ordinary prose before retrying.

## Verify the hook

Shell script tests use [Bats](https://bats-core.readthedocs.io/) and live beside
the script they test. Install Bats 1.7 or newer, then run the hook's integration
suite from the repository root:

```sh
bats .githooks/commit-msg.bats
```

Each test uses its own temporary repository. The suite checks message preservation,
repeated sanitization, failure handling, and real normal and amended commits.
It does not change this repository's Git configuration or history.

To exercise direct hook invocations with another POSIX shell, set `HOOK_SHELL`:

```sh
HOOK_SHELL=dash bats .githooks/commit-msg.bats
```

Bats runs tests using Bash; the hook itself still runs under POSIX `/bin/sh`
when invoked by Git and does not require Bats.

## Local enforcement limits

Hook activation is local configuration and is not enabled automatically by a
clone. `git commit --no-verify`, changing `core.hooksPath`, deleting or disabling
the hook, and commands that do not run `commit-msg` (such as `git commit-tree`)
can bypass it. Existing commits are not rewritten. The prohibition still
applies when the hook is bypassed; this local hook alone cannot enforce it
across all contributors or commits received by a remote repository.
