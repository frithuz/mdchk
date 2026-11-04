# Agent instructions

## Commit message style

Follow the [Go project's commit message conventions](https://go.dev/doc/contribute#commit_messages):

- Start with a short subject in the form `area: summary`, using the primary affected package or project area as the prefix.
- Write the summary in the imperative, starting with a lowercase word and without a trailing period. For example: `docs: clarify lint configuration`.
- Separate the subject from the body with a blank line. Explain what changed and why in complete sentences, using plain text wrapped at about 72 columns.
- Include relevant issue references, such as `Fixes: #123` or `Updates: #123`, when applicable.

## No co-author trailers

Under absolutely no condition may an agent add a `Co-Authored-By` trailer to a commit message. This rule has no exceptions and applies to all commits an agent creates or edits, including amended, rebased, and squashed commits. Do not add such trailers through templates, tooling, or automated attribution.

## Setup and validation

A fresh checkout needs Go (the version in `go.mod`), Node and Python 3. Run
`task repo:init` once to activate the repository Git hooks, then
`go mod download`. mdchk is a CLI with no dev server: build it with
`go build -o mdchk ./cmd/mdchk`. See `docs/contributing.md` for more.

`factory/checks.sh` runs the validation commands from the Linux `test` job in
`.github/workflows/go.yml`: `go test ./...`, the no-cgo build, cross-target
`go vet`, the conformance-tagged tests, `sh tools/lint.sh` and its self-test,
and the CommonMark and GFM corpus, profile and table checks in `tools/`. CI
also runs race, fuzz, native cross-OS and vulnerability jobs.

## Software factory

`/factory run` builds every open GitHub issue labeled `factory` through
intake → spec → build → gate → review → approve, one PR per issue. The skill
is `.agents/skills/factory/SKILL.md` and its agents are in
`.agents/agents/<name>/agent.md`; Claude Code finds them through symlinks
in `.claude/skills/` and `.claude/agents/`, so edit the `.agents/` files. State is in `factory/jobs/` (gitignored);
`factory/bin/job` is its only writer. Checks: `factory/checks.sh`. Dashboard:
`cd factory && python3 -m http.server`, then open
http://localhost:8000/dashboard.html.

Each job works in its own git worktree, a full checkout of the repo at
`.worktrees/<job-id>` (gitignored). A tool run from the repo root that doesn't
read `.gitignore` sees those copies too; exclude `.worktrees/` in its config.
