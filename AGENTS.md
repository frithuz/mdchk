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
and the CommonMark and GFM corpus, profile and table checks in `tools/`. It also
runs the result contract validator and the producer tests that use it, which CI
doesn't. CI also runs a vulnerability scan at the end of that job, plus race
and fuzz jobs.

The native macOS and Windows `cli-platform` job is paused with `if: false` and
will be revived later. Until then CI runs nothing on those hosts, so a passing
run or a test matching that job's filters is not macOS or Windows evidence.
Other docs and agent files still describe the job as running.

CI runs Go tests through `go run ./tools/testrun`, which prints one summary
line on a pass and only failing output otherwise; set `DEBUG=1` for the full
`go test -v` stream.

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

Jobs run at the same time and share one disk, one Go cache and the session
scratchpad, so a factory agent must not disturb what it didn't create:

- Keep temp files in `<Scratch>/<your agent name>/`, using the `Scratch` path from
  your prompt, instead of the session scratchpad. It's removed when the job ends.
- Delete and kill only what you created. Never use a name-wide `pkill`.
- `factory/bin/gate` refuses to run when a disk has less than `min_free_gb` free
  (`factory/config.json`). If you hit a full disk, report it. Don't delete the
  shared Go cache, and don't point `GOCACHE` or `TMPDIR` into the repo: tests fail
  with temp directories there. Only the orchestrator empties the cache, with
  `factory/bin/job trim-cache` once a run has no job left running.
- Acceptance binaries are shared in the user cache directory, are private to
  their key, and expire by themselves; `factory/bin/job trim-cache` doesn't touch
  them.
