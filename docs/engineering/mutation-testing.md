# Critical behavior mutation baseline

Run `sh tools/mutation.sh --output /tmp/mdchk-mutations-unique` from a POSIX (Linux/macOS) Git
checkout with Go from go.mod and Python 3. The output directory must be new and
outside the checkout. The script installs Gremlins **v0.6.0** into a private
outside-module directory; it never trusts a PATH executable. `go version -m`
verifies the installed module version (the upstream executable's `--version`
can say `dev` for a version-qualified Go install). No product module dependency
or Go directive is changed.

The explicit [.gremlins.yaml](../../.gremlins.yaml) selects conditional boundary
and negation operators, one worker, two test CPUs and no global score threshold.
Arithmetic, assignment, logical, loop and other operators are deliberately later
expansion. Ambient `GREMLINS_*`, GOFLAGS, workspace and schema-interpreter settings
cannot change the campaign. GOMAXPROCS and Go build parallelism are two.

The runner copies all tracked and nonignored candidate files, including dirty
bytes and new tests, into a private clean snapshot. It excludes Git metadata and
ignored artifacts such as Working. Symlinks are refused. Gremlins copies this
small snapshot per worker, never the live checkout. Reports remain outside every
source copy. The source manifest includes hashes and modes; live and snapshot
bytes are checked after execution. Do not edit the candidate during a run.

| Area | Mutated files | Executed tests |
| --- | --- | --- |
| Heading rule | internal/rules/md001.go | internal/rules package |
| Configuration permissions | internal/config/validate.go, policy.go | internal/config package |
| Safe fix structure and semantic validation | internal/fix/plan.go, validate.go | internal/fix package |

These are package-local campaigns. `coverpkg` is not used to imply consumer test
execution. Production MD047 tests run in the rules package, but fix mutants are
not tested against rules consumers in this initial baseline. Engine's separate
`internal/engine/edit_permissions.go` is deliberately deferred: fix tests execute
its ordinary behavior, but do not mutate its interval comparisons. Broader
consumer integration, engine boundaries and additional rules/operators remain
future expansion. This baseline does not claim exhaustive critical-code coverage.

Each area has an owned process-group deadline of 900 seconds. Installation has
300 seconds and each baseline/mutant replay 120 seconds. Tool errors, baseline
failures, deadlines, interruption, missing/malformed/empty/incomplete reports,
unexpected files/operators and missing executed areas fail the command. An exit
zero without completed reports is failure. Every invocation uses a fresh output
directory, so an earlier report cannot satisfy it. All final statuses are counted
from file-level mutations, including NOT COVERED, NOT VIABLE, TIMED OUT and
SKIPPED; these are not semantic assertion kills. LIVED remains observational,
with an explicit survivor ledger, before review establishes a score gate.

Four representative reported kills must also pass a paired replay: an uncached
baseline test passes, then the exact comparison token is changed in the isolated
snapshot and the named semantic test must fail an assertion. Build failures,
panics, no-tests and deadlines cannot satisfy these controls. The controls cover
a heading increment, a permission decision, CRLF-interior edit rejection, and
trusted semantic preservation. Full baseline and mutant logs are retained.

The separate [scheduled workflow](../../.github/workflows/mutation.yml) runs on
weekly schedule or workflow_dispatch with a 65-minute job bound. It verifies the
failure harness and retains complete or partial logs/reports with `always()`;
there is no continue-on-error. Existing ordinary Go workflow jobs are unchanged.
Local measurements below are not hosted Linux acceptance evidence.

## Measured baseline and review

Measured locally on macOS-27.2-arm64-arm-64bit-Mach-O using go version go1.27.3 darwin/arm64.
One worker, GOMAXPROCS=2, Go build parallelism=2, test CPU=2; existing module/build
caches reused. The run includes pinned installation, coverage, all three campaigns
and all four paired replays. Whole wall time: **362.766 seconds**.

| Area | Listed | Killed | Lived | Not covered | Whole area (s) | Engine (s) | Coverage startup |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| rules | 7 | 7 | 0 | 0 | 8.245 | 5.531 | 2.6480295s |
| config | 89 | 84 | 0 | 5 | 65.315 | 63.883 | 1.36824325s |
| fix | 334 | 310 | 0 | 24 | 282.072 | 266.276 | 15.731501042s |

**430 listed = 401 killed + 29 not covered**. All areas have zero LIVED,
TIMED OUT, NOT VIABLE and SKIPPED. The raw killed/listed fraction is **93.26%**;
tool efficacy uses KILLED/(KILLED+LIVED), so it is **100%**. These denominators
answer different questions. Neither establishes exhaustive coverage.

Source commit: `bc6fbb11a0b63f921879edcbbac234f22f709a0f` plus the candidate file manifest in
`run.json` (source digest `5a15c3a4e5fcdec9d9ca81434b6299d4f0372bcd42ede55331ff4daa8a6f29c3`).
Official v0.6.0 module origin: `ca8fbcc452c99bc33200ddfec30a8d46bf85c42a`;
module checksum: `h1:3G2ROO0I3q4bb5bxElQIUITTuEbl1iOfVYFqunGwrJI=`.
`tool-build.log` retains actual build provenance. This is the final entrypoint
repeat; the first exploratory run also completed 401 killed/29 not covered
in 360.727 seconds before report identity/delta and coverage-parser refinements.

Raw JSON, full console logs and paired replay logs are preserved in
[mutation-baseline](mutation-baseline/). Stable mutant identities combine source
SHA-256, exact source line, file/position, operator and token delta in
`mutants.json`; status is excluded from identity. `run.json` retains the complete source manifest, exact commands,
tool/config identity, host and timing. Positions alone are not stable across
source edits. The engine's reported elapsed time excludes coverage and startup;
whole invocation times include these overheads. Existing Go caches were reused;
this is not a cold-cache throughput benchmark.

See the [unexecuted mutation ledger](mutation-baseline/survivors.md) for all 29
source-identified uncovered mutations and concrete dispositions. No executed
survivor required an equivalence claim or new assertion in this campaign.

No production behavior was changed to improve a score. Review the recorded
survivors and scope with the project reviewers before imposing any global score
threshold. Future runs retain their raw results even when observational survivors
change; a completed run is evidence to review, not automatic approval of a new
baseline.

## Final replay and evidence applicability

After the measured campaign, the final runner tightened only replay diagnostics:
the plan control now changes `text[at-1] == '\r'` to `!=`, requiring the CRLF
subtest to fail with an accepted unsafe decision; the semantic control requires
the uncertain-proof subtest to fail with the unsafe candidate retained as complete.
All four paired controls passed again through the final replay implementation;
[strict-replays/run.json](mutation-baseline/strict-replays/run.json) records the
final runner hash, source manifest, assertion markers and separate replay time.
The original campaign `run.json` and its original replay logs remain unchanged.

All five mutated Go files, their tests and the explicit configuration stayed
byte-identical between campaign and stricter replay. The package invocation,
installation, limits, discovery and report-validation code also stayed unchanged.
The final harness adds real entrypoint SIGTERM cleanup and stale-output rejection
controls. The final documentation and committed reports were filled afterward;
these evidence-only additions do not change campaign applicability. The whole
campaign was not rerun simply to include its own generated evidence in a snapshot.

The seven infrastructure tests run the actual signal entrypoint, assert owned
descendant cleanup on SIGTERM and deadline, retain partial failure evidence,
refuse stale/malformed/incomplete/empty/wrong-scope reports, and prove dirty/new
candidate bytes are copied while ignored artifacts and Git metadata are omitted.

Four original console logs contain intentional whitespace from printed source
bytes. They are stored losslessly as `.log.gz` so repository whitespace checks
remain useful. `log-archives.json` maps original names to archives and records
both compressed and uncompressed SHA-256 values. Use `gzip -dc FILE.log.gz` to
read them; decompression reproduces the original console bytes exactly.
