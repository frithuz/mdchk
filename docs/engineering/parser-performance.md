# Parser performance baseline

The growth benchmarks parse an existing immutable source snapshot. Input
construction and the initial outcome/tree inspection are outside the timer;
source copying, file I/O, concurrency and transport work are excluded. Every
iteration must finish with `Complete`, without a failure or interruption.
`complete/op` and `nodes/op` expose that outcome and retained AST size.
`BenchmarkParseDefaultLimit` separately requires explicit document-node budget
exhaustion; its `limited/op` time is never a successful-parse throughput result.

## Reproduction

Baseline revision: `4e43c02419371ebdc4f7a36d14a30290c91c0a08` (before #31),
using the new benchmark fixtures. Host: Apple M1 Max, 64 GiB RAM,
Darwin 27.2.0 arm64; Go `go1.27.2 darwin/arm64`. Local, serial, closed-loop
microbenchmarks use `GOMAXPROCS=1`, default GC settings, no external services,
three independent benchmark repetitions, and 50 ms calibration per case.
These are bounded initial measurements, not stable machine-independent latency
thresholds. The largest cases execute few iterations; repeat with a longer
`-benchtime` and more samples before drawing performance-regression conclusions.

```sh
GOMAXPROCS=1 go test -run '^$' -bench '^BenchmarkParseGrowth$' \
  -benchmem -benchtime=50ms -count=3 ./internal/parser
GOMAXPROCS=1 go test -run '^$' -bench '^BenchmarkParseDefaultLimit$' \
  -benchmem -benchtime=50ms -count=3 ./internal/parser
```

To reproduce the pre-limit baseline in a detached worktree at the revision
above, copy `internal/parser/performance_test.go` into that worktree, omitting
`BenchmarkParseDefaultLimit` (the old parser has no resource-limit result API),
and run the first command. Existing failed-link and reference-title benchmarks
remain separate; this suite does not duplicate those scanners' fixtures.

Each ordinary path uses 256, 1,024 and 4,096 repeated units; dense inline syntax
uses 64, 256 and 1,024 units so successful parsing fits the finite default
construction budget. Nested container and image/emphasis cases independently
vary depth (4, 16, 64 at 256 payload bytes), then payload (256, 1,024, 4,096
bytes at depth 4). Container fixtures contain eight prefixed lines. Backtick
fixtures mix run widths 1–3 with a final unmatched width-4 opener. All baseline
cases below completed. Values are medians of three runs; allocations are rounded
to whole counts/bytes.

| Path | ns/op | B/op | allocs/op | Retained nodes |
| --- | ---: | ---: | ---: | ---: |
| `plain/256` | 41,226 | 10,008 | 30 | 3 |
| `plain/1024` | 161,030 | 28,440 | 30 | 3 |
| `plain/4096` | 624,370 | 102,168 | 30 | 3 |
| `short-lines/256` | 514,896 | 520,377 | 3,378 | 513 |
| `short-lines/1024` | 1,473,354 | 2,207,167 | 13,374 | 2,049 |
| `short-lines/4096` | 6,327,176 | 9,091,514 | 53,326 | 8,193 |
| `dense-inlines/64` | 989,796 | 1,530,090 | 12,816 | 898 |
| `dense-inlines/256` | 4,314,657 | 6,242,289 | 51,040 | 3,586 |
| `dense-inlines/1024` | 22,860,250 | 25,370,749 | 203,896 | 14,338 |
| `unfinished-html/256` | 45,741 | 11,424 | 41 | 3 |
| `unfinished-html/1024` | 175,352 | 41,760 | 44 | 3 |
| `unfinished-html/4096` | 700,592 | 181,664 | 48 | 3 |
| `backticks-malformed-tail/256` | 236,375 | 396,840 | 3,586 | 386 |
| `backticks-malformed-tail/1024` | 1,074,647 | 1,615,531 | 14,158 | 1,538 |
| `backticks-malformed-tail/4096` | 4,465,853 | 6,490,545 | 56,413 | 6,146 |
| `undefined-references/256` | 60,425 | 18,728 | 34 | 3 |
| `undefined-references/1024` | 229,139 | 59,432 | 34 | 3 |
| `undefined-references/4096` | 902,544 | 225,320 | 34 | 3 |
| `containers/depth/4` | 46,953 | 44,464 | 231 | 21 |
| `containers/depth/16` | 74,397 | 105,072 | 509 | 33 |
| `containers/depth/64` | 180,271 | 304,240 | 1,614 | 81 |
| `containers/payload/256` | 47,040 | 44,464 | 231 | 21 |
| `containers/payload/1024` | 123,030 | 74,192 | 232 | 21 |
| `containers/payload/4096` | 431,233 | 223,056 | 233 | 21 |
| `images-emphasis/depth/4` | 27,904 | 39,512 | 336 | 11 |
| `images-emphasis/depth/16` | 86,691 | 141,072 | 1,264 | 35 |
| `images-emphasis/depth/64` | 349,157 | 570,225 | 4,964 | 131 |
| `images-emphasis/payload/256` | 28,222 | 39,512 | 336 | 11 |
| `images-emphasis/payload/1024` | 45,689 | 53,048 | 336 | 11 |
| `images-emphasis/payload/4096` | 114,473 | 113,592 | 336 | 11 |

## Delivered resource-metered candidate

The same commands, fixtures, host and toolchain were rerun after resource
metering and its profile-guided checkpoint/locking changes, without overlapping
fuzzing or other validation jobs. All 30 growth cases still completed with the
same retained-node counts as the baseline. The production source digest before
and after measurement was identical:
`8112f1a13412c0f80c29cbfcff2c7142a26bd630a34963897078252aa15c3b22`.
This identifies the measured working tree independently of the eventual commit.
The SHA-256 is calculated over sorted `internal/parser/*.go` production files,
excluding `_test.go`, with each relative path and file content followed by NUL:

```python
import hashlib
from pathlib import Path

h = hashlib.sha256()
for p in sorted(Path("internal/parser").glob("*.go")):
    if not p.name.endswith("_test.go"):
        h.update(str(p).encode() + b"\0" + p.read_bytes() + b"\0")
print(h.hexdigest())
```

Median candidate results (three repetitions, 50 ms calibration):

| Path | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| `plain/256` | 131,329 | 10,280 | 35 |
| `plain/1024` | 513,417 | 28,712 | 35 |
| `plain/4096` | 2,022,750 | 102,440 | 35 |
| `short-lines/256` | 570,702 | 520,633 | 3,382 |
| `short-lines/1024` | 2,658,623 | 2,207,422 | 13,378 |
| `short-lines/4096` | 10,812,958 | 9,091,776 | 53,330 |
| `dense-inlines/64` | 1,949,626 | 1,530,474 | 12,827 |
| `dense-inlines/256` | 8,330,274 | 6,242,673 | 51,051 |
| `dense-inlines/1024` | 36,507,480 | 25,371,128 | 203,907 |
| `unfinished-html/256` | 145,287 | 11,696 | 46 |
| `unfinished-html/1024` | 570,214 | 42,032 | 49 |
| `unfinished-html/4096` | 2,255,965 | 181,936 | 53 |
| `backticks-malformed-tail/256` | 500,387 | 397,112 | 3,591 |
| `backticks-malformed-tail/1024` | 2,110,818 | 1,615,803 | 14,163 |
| `backticks-malformed-tail/4096` | 8,663,076 | 6,490,816 | 56,418 |
| `undefined-references/256` | 183,561 | 19,032 | 39 |
| `undefined-references/1024` | 722,955 | 59,736 | 39 |
| `undefined-references/4096` | 2,872,246 | 225,624 | 39 |
| `containers/depth/4` | 118,165 | 44,976 | 239 |
| `containers/depth/16` | 164,945 | 105,968 | 529 |
| `containers/depth/64` | 374,370 | 306,672 | 1,682 |
| `containers/payload/256` | 113,987 | 44,976 | 239 |
| `containers/payload/1024` | 338,954 | 74,704 | 240 |
| `containers/payload/4096` | 1,260,906 | 223,568 | 241 |
| `images-emphasis/depth/4` | 72,161 | 39,888 | 350 |
| `images-emphasis/depth/16` | 203,420 | 141,736 | 1,302 |
| `images-emphasis/depth/64` | 732,474 | 572,040 | 5,098 |
| `images-emphasis/payload/256` | 72,424 | 39,888 | 350 |
| `images-emphasis/payload/1024` | 149,301 | 53,424 | 350 |
| `images-emphasis/payload/4096` | 454,008 | 113,968 | 350 |

The explicit-limit fixture separately returned `Incomplete`/`Partial` with
`LimitExceededReason` and a typed `document-nodes` exhaustion on every run:
57,912,666 ns/op (range 56,794,000–59,086,542), 37,271,144 B/op, and 281,325
allocs/op. The input was not completely parsed, so no input-throughput metric is
reported for this case. Each run used one measured iteration; repeat longer for
confidence in timing changes.

The finite accounting has a measurable constant cost. Plain 49,152-byte input
now takes about 2.023 ms versus 0.624 ms before metering (about 3.2 times); dense
1,024-unit input takes 36.507 ms versus 22.860 ms (about 1.6 times). These are
descriptive median comparisons, not statistical significance claims. Plain
fourfold payload increases take about 3.9 times longer, unfinished HTML about
4.0 times, and malformed-tail backticks about 4.1–4.2 times. Dense syntax grows
about 4.3–4.4 times while allocations and retained output grow about fourfold.
The measured flat-input ranges expose no additional quadratic growth in input
width; deeper or larger accepted profiles require their own measurements.

Quote depth admission scans the current ancestor stack before pushing another
container. Those cumulative checks can grow quadratically with depth, although
each scan is charged and the finite syntax-depth limit bounds the accepted
range. At fixed 256-byte payload, the measured depths 4 / 16 / 64 took medians
of 118,165 / 164,945 / 374,370 ns, respectively, retaining 21 / 33 / 81 nodes.
These samples show no large depth-related jump in the measured range; they do
not establish linear scaling for arbitrary operator-raised depth limits.

Profiles of earlier candidates identified per-byte monotonic-clock checks,
then per-byte mutex/context lookup costs. The implementation moved elapsed-time
checks to bounded checkpoints, holds the accepted single-worker operation lock
across a parse, and avoids repeated context lookup in hot scanning paths while
retaining exact budget counters. The final figures include the remaining cost
of that accounting. Timing improvement from those repairs is not a reason to
weaken exhaustion or cancellation semantics.

## Growth investigation

Plain text, unfinished HTML, and unresolved references scale close to the
fourfold input increase, with nearly constant allocation counts. Short lines
and dense inlines produce many source-preserving nodes and segments: their
allocation bytes/counts grow approximately with output size, while elapsed time
has additional GC and allocator costs. For example, the pre-limit exploratory
4,096-unit dense fixture retained 57,346 nodes and allocated about 102 MB in
815,251 allocations per parse, despite only about 143 KB of source. Its
roughly 4–5-fold elapsed growth per fourfold input increase is not evidence of
quadratic suffix rescanning. That fixture is now the separate explicit-limit
benchmark rather than part of successful-parse throughput.

An allocation profile of that exploratory pre-limit dense fixture attributed
about 22% of allocated bytes directly to `NewNode`, 14% to segment-slice cloning,
13% to node-handle cloning, and 12% to `inlineNodes.take`. These are allocation
costs, not retained-heap measurements or claims that they may be removed without
preserving immutable source ownership. The short CPU sample was dominated by
runtime memory management and scanning rather than one Markdown scanner; it is
insufficient to assign a stable percentage to a parser function.

Nested image/emphasis labels retain original label values at each enclosing
level. Consequently source size, retained node count, and provenance/label bytes
are different workload measures. A separate pre-limit exploratory depth sweep
at fixed 256-byte payload measured median times of 356,018 / 733,190 / 1,477,029 /
3,416,037 ns at depths 64 / 128 / 256 / 512; allocated bytes were 570,224 /
1,199,010 / 2,649,156 / 6,429,256, and allocations were 4,964 / 9,894 / 19,752 /
39,467. Output amplification explains some superlinear byte growth even while
node/allocation counts remain approximately linear. Depth 512 intentionally
exceeds the new default supported depth, so this is an investigation of the
previous parser, not a promise of completion under current limits. Independent
depth/payload benchmarks keep those effects distinguishable.

For a longer focused investigation, run a single benchmark with profiles rather
than profiling concurrent benchmark jobs:

```sh
GOMAXPROCS=1 go test -run '^$' \
  -bench '^BenchmarkParseGrowth/dense-inlines/1024$' -benchtime=3s \
  -cpuprofile=/tmp/parser.cpu -memprofile=/tmp/parser.mem ./internal/parser
go tool pprof -top /tmp/parser.cpu
go tool pprof -top -alloc_space /tmp/parser.mem
```

For the exploratory pre-limit cases, extend the dense growth size slice to
include 4,096 and the image depth slice to include 128, 256, and 512 in the
detached baseline checkout only. Select the dense case for CPU/allocation
profiling or select `images-emphasis/depth` for the depth sweep. These larger
workloads must not replace the current completed-parse fixtures.

Finite byte, depth, construction, and work limits bound accepted work; they do
not establish a universal linear-time complexity guarantee. Recheck both the
explicit outcome and output growth whenever changing limits or workloads.

## Integrated GFM initial baseline

Measured on the #235 candidate based on `db897be92028f33d8bb8b1bac8eb3e0a4048b3c9`,
after the R2 independent mutation-oracle repair, on Apple M1 Max, 64 GiB,
Darwin 27.2.0 arm64, Go 1.27.3 darwin/arm64.
Local serial closed-loop microbenchmarks used GOMAXPROCS=1 and default GC settings;
no concurrent fuzz/test campaign or external service ran. Snapshot creation,
fixture generation, initial outcome/tree inspection, export and I/O are excluded.
Operation creation and actual integrated parsing are timed. Source identity below
is SHA-256 of the sorted JSON mapping from every `internal/parser/*.go` path to
its SHA-256, including test fixtures.

`536e221376ebd520c257b4504a77d2e2fbb8894eaf2a3c169807d58309048973`

```sh
GOMAXPROCS=1 go test -run '^$' -bench '^BenchmarkGFMProfile$' \
  -benchmem -benchtime=50ms -count=3 ./internal/parser
```

Values are medians of three short calibration samples, an initial observation,
not a throughput threshold, regression claim or comparison with the historical
core baseline above. All complete rows use default finite limits. The last row
uses 10,000 work units, asserts work-units exhaustion (observed > allowed
10,000), and reports no completed throughput. Work below is admitted accounting;
construction differs from retained AST nodes.

| Fixture | Input bytes | Outcome | ns/op | B/op | allocs/op | Nodes | Work | Construction |
| --- | ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| mixed | 193 | Complete | 126912 | 139088 | 981 | 47 | 16164 | 668 |
| url-suffix | 1570 | Complete | 525722 | 594064 | 2663 | 240 | 177998 | 3243 |
| html-tail | 1288 | Complete | 202289 | 171352 | 1196 | 48 | 183364 | 874 |
| explicit-work-limit | 3137 | work-units limit | 9039 | 7136 | 38 | 1 | 9937 | 59 |

The mixed 193-byte literal combines all five extensions, quote/list/task, a
two-column table with short/excess cells, nested emphasis, code/comment protection
and reference links. URL and HTML families append 32 adversarial units; the
limited fixture appends 128 inline units. Exact source literals/generators remain
in `gfm_verification_test.go` and `gfm_performance_test.go`. Finite adversarial
resource tests separately record doubled sizes 16/32/64. Neither elapsed growth
nor these bounded counters prove arbitrary-input complexity or OS RSS limits.

The prior R1 short sample preceded the test-oracle repair. Its parser-Go digest
was `815355048fc27ef3b102b1df0c0ca10410c999fb87bd7fe45867fac36bcf3bc4`;
median ns/op were 153235 / 600562 / 233038 / 10497 for mixed, URL suffix,
HTML tail and explicit work limit respectively. The repair changes verification,
not production parsing or benchmark inputs. Both observations are retained as
initial samples; their difference is not a claimed optimization or regression.
