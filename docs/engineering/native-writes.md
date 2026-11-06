# Native per-file replacement

Issues #146 and #147 implement acquisition-bound stale protection and production
per-file replacement in `internal/files`, with a retained semantic-result adapter
in `internal/fix`. Issue #261 replaced the original Darwin-only, cgo-based adapter
with one portable implementation built on Go's standard library (`os.Root`) and,
only where unavoidable, pure-Go `golang.org/x/sys`. CLI apply uses this retained
authority and reports observed saved snapshots, partial publication outcomes and
remaining saved-content findings. Dry-run does not call this path or create stages.

## Platform matrix

Apply is implemented once for Linux, macOS and Windows and builds with
`CGO_ENABLED=0`. No build needs a C toolchain.

| Host | Apply behavior | Evidence |
| --- | --- | --- |
| Linux, macOS, Windows | Native replacement and exact saved snapshots through one implementation | `cli-platform` CI job on macOS and Windows, with `CGO_ENABLED=0`; the `test` and full race jobs on Linux |
| Any other `GOOS` | Explicit refusal, exit 13, nothing written: `native replacement is unsupported on <GOOS>; apply supports linux, darwin and windows`. Dry-run remains usable | Compile check for `freebsd` in the `test` job and a unit test of the refusal |

The refusal on other hosts is a deliberate, testable decision, not a missing
adapter. Network, cloud and virtual filesystems are not verified and no support is
claimed for them; there is no filesystem-type check.

## Supported targets

A supported target is a regular file, reached without any symlink, junction or
reparse-point component, inside the selected root, with an owner-writable mode and
no set-ID or sticky bits. Anything else is refused as an unsupported target with
exit 13 and nothing written. This includes read-only files, directories, FIFOs,
devices and files whose parent chain contains a link.

## Authority and confinement

`AcquireWrite` uses the existing bounded read and immutable Source construction.
It retains the same read's pinned parent directory, the full parent-chain
identities, and the file's identity, mode, owner and extended attributes.

`internal/fsroot` provides the pin-and-verify traversal on `os.Root`. `os.Root`
guarantees that no lookup escapes the root it was opened on, but it follows
symlinks that stay inside the root. The contract that every component is a plain
directory or regular file is therefore enforced on top of it:

- Each component is examined with `Root.Lstat`; anything that is not a plain
  directory (or, for the final entry, a regular file) is refused. `ModeSymlink`
  and `ModeIrregular` cover Windows junctions, mount points and other reparse
  points.
- The component is opened through the held parent, the opened handle is compared
  with the `Lstat` result using `os.SameFile`, and the entry is examined again
  after the open.
- Each directory is pinned with `Root.OpenRoot` after that verification, so every
  later create, rename and remove is relative to a verified held handle and never
  re-resolves a path.

A component swapped to a contained symlink between check and open is detected by
the post-open identity comparison and can never escape the root; a swap to an
escaping symlink is refused by `os.Root` itself. Absolute canonical paths must
already name the intended non-symlink scope; inspection's explicit symlink
exception grants no write access.

`WriteSet` owns the observations and residual budget. Fresh equal-byte Sources,
mutable acquisition descriptions and detached `fix.FileView` values cannot
manufacture approval. `fix.Validated.Publish` uses its privately retained
original/candidate sources and dependency graph, requiring the same original
budget. Cancellation of the acquisition context remains binding even if a caller
passes a fresh context. Both contexts remain binding throughout dependency
traversal and the last pre-rename checkpoint. `WriteSet.Publish` is the
lower-level native primitive and does not itself claim semantic correction
approval.

Resource-only acquisitions cannot become publication targets. Content and
absence observations remain distinct. Required configuration/permission resources
are declared at acquisition; selected-target and resource dependencies retained
by the validator are also checked. Exact bytes, size, modification time, entry
identity, type, mode, owner, extended attributes and parent identity, mode and
owner are revalidated immediately before publication. Changes with restored mtime
and identical-byte inode replacement are stale. No rebasing occurs. After this
operation observes one of its own replacements, later dependency checks use that
saved identity/content while retaining the original acquisition binding. CLI
saved-view reconciliation uses these observations.

## Replacement

For each file, apply:

1. Verifies every path component and pins the parent directory.
2. Writes the full candidate to an exclusive 0600 sibling stage with an
   unpredictable name, then copies mode and, on Unix, owner/group and all extended
   attributes. Any copy failure refuses.
3. Revalidates identity, size, mtime, mode and content hash of the target, its
   dependencies and its parent. Any change is stale and nothing is published.
4. Replaces the target with one rename through the pinned parent, then re-reads
   the saved entry and, on Unix, syncs the parent directory.

The target is never truncated or deleted first, and nothing is written outside the
selected targets. Cleanup removes only the owned stage and refuses detected stage
substitution. Equal retained candidates return `NoChange=true` with no Saved
snapshot, no stage and no write. This explicitly means no native confirmation was
performed; callers must not use it as an observed saved view. It does not claim
writable acquisition authority.

On Windows, `os.Rename` is `MoveFileEx` with `MOVEFILE_REPLACE_EXISTING`: a
same-volume replace, not a stronger claim. A target held open without
share-delete fails and is reported as a normal I/O refusal with the stage removed
and the original untouched.

Hard-link aliases remain separate logical targets: other aliases retain the
original inode/content, and `HardLinks` records the original link count observed
at acquisition: from the stat result on Unix and from
`GetFileInformationByHandle` on Windows.

## Metadata

| Item | Behavior |
| --- | --- |
| Mode, set-ID, sticky | Portable via `os.FileMode`. Unsupported bits and owner-unwritable modes refuse. Mode is copied with `Chmod`. |
| Read-only | Refused through the mode bits; the Windows read-only attribute is reflected in the mode. |
| Owner/group | Unix: the stage is `Chown`ed to the target's uid/gid when it differs; failure refuses. Windows: not applicable, the stage inherits the directory's access control. |
| Hard-link count | Not part of the stale comparison, because identity and content cover it. Reported where the platform supplies it. |
| Extended attributes (Linux, macOS) | All attributes are listed, read and copied exactly through `Flistxattr`/`Fgetxattr`/`Fsetxattr`; attributes the new file acquired that the target lacks are removed. Failure refuses. This preserves Linux POSIX ACLs and SELinux labels and `com.apple.provenance`, quarantine and resource forks on macOS. Files carrying them are fixable rather than refused. Bounded to 256 attributes and 1 MiB. |
| macOS ACLs | Not inspected or preserved (see accepted limits). |
| BSD file flags, Linux immutable | No pre-check. An immutable target fails at rename or open with `EPERM`, which refuses with no mutation. |
| Windows alternate data streams | Dropped on replace (see accepted limits); a test pins the behavior. |
| Directory sync | Parent `Sync` on Unix; skipped on Windows. |

## Outcomes

Failures before the rename leave the original untouched and remove the stage. A
stage that cannot be proven owned is left in place and reported by name. A file
that cannot be replaced (immutable, held open without share-delete on Windows,
permission denied) is an I/O refusal with no mutation. Independent files continue
after another file fails. Prepublication failures leave the original untouched.
Native rename errors are reconciled against the staged and target identities:
observed publication is retained, an intact original/stage pair establishes
refusal, and ambiguous entry state is explicitly uncertain. `Publication.Published`
retains successful rename evidence even if later sync or cancellation fails;
`Uncertain` and an absent Saved snapshot describe failed saved-state observation.
An uncertain state cannot authorize later dependent writes without a new
operation. Published content is never blindly rolled back over newer content.

## Accepted limits

- **No cross-process lock.** Stale protection detects concurrent changes but does
  not serialize writers, and a noncooperating writer can still change a file
  between the last check and the rename. A deterministic native test changes the
  target after the final check and demonstrates the newer bytes being
  overwritten. Reviewed when MD019, MD021 and MD009 corrections joined MD047: no
  lock is added, intentionally, because mdchk trusts one execution at a time, in line
  with #261. Stale protection is unchanged. Two concurrent runs with the same
  configuration compute identical output. Runs with different configurations, such
  as different MD009 settings, can compute different output, and a run can overwrite
  another run's result after its last check. Full
  no-lost-update protection requires exclusive or coordinated access. No
  protection against privileged mount manipulation is claimed.
- **macOS ACL entries** are not inspected or preserved. Detecting them requires
  libc and therefore cgo.
- **Windows alternate data streams**, including Mark-of-the-Web, are dropped on
  replace. This is best effort with no workaround.
- **No filesystem-type check.** Network, cloud and virtual filesystems are not
  verified and no support is claimed.
- **No power-loss durability, multi-file transactions or rollback.** The stage and,
  on Unix, the parent directory are synced; nothing stronger is promised.
- **Stage inode identity.** `os.SameFile` cannot distinguish an inode number a
  filesystem has reused, so stage-substitution detection is best effort within the
  same noncooperating-writer interval.
- Post-write snapshots describe observed bytes, which external writers can change
  again.

## Platform-specific code

mdchk contains no platform-specific code unless it is required. Each file and the
Go API tried first:

| File | Why it is required |
| --- | --- |
| `internal/fsroot/flags_unix.go` | `os.Root.OpenFile` accepts raw flags but the standard library exports no portable `O_NONBLOCK`/`O_NOFOLLOW`; the `syscall` constants are used (no cgo, no `x/sys`). A raced FIFO would otherwise hang a read. Also holds the Unix directory `fsync`, which `os.File.Sync` provides on a directory opened with `Root.Open(".")`. Config discovery and config file reads (`internal/config`, `internal/configscan`) use `ReadFlags` too. |
| `internal/fsroot/flags_other.go` | The Windows counterpart: no extra open flags are needed, and a directory handle cannot be synced. |
| `internal/files/owner_unix.go` | `os.FileInfo` exposes no owner or link count; `Sys()` yields `*syscall.Stat_t` (stdlib). `os.File.Chown` applies it. |
| `internal/files/owner_windows.go` | `os.FileInfo` carries no link count on Windows; the stdlib `syscall.GetFileInformationByHandle` reads it from the open handle. |
| `internal/files/owner_other.go` | Windows: ownership is governed by inherited access control that `os.FileInfo` does not expose. |
| `internal/files/xattr_unix.go` | The standard library has no extended attribute API. Pure-Go `golang.org/x/sys/unix` (`Flistxattr`, `Fgetxattr`, `Fsetxattr`, `Fremovexattr`) is the only dependency, enforced by `depguard`. |
| `internal/files/xattr_other.go` | Windows has no extended attributes to copy. |
| `internal/files/write_native.go` | The native write path, built only for Linux, macOS and Windows. It pairs with `write_unsupported.go`. |
| `internal/files/write_unsupported.go` | The explicit refusal compiled only for hosts outside Linux, macOS and Windows. |
| `cmd/mdchk/output_signals_unix.go` | Ignores `SIGPIPE` so that writes to a closed stdout or stderr return `EPIPE` and the CLI's exit precedence holds. Go otherwise ends the process on `SIGPIPE` for descriptors 1 and 2. This applies only to the executable. |
| `cmd/mdchk/output_signals_other.go` | The non-Unix counterpart, which does nothing. |

### `internal/resource`

The read-only resource reader keeps its platform files. The consolidation
onto `internal/fsroot` was evaluated against the existing read tests and
rejected, because each primitive the reader needs is missing from `os.Root`
or from the standard library's `syscall` package. The checks below are
against the Go 1.27 sources.

| File | Go API tried | Why it is insufficient |
| --- | --- | --- |
| `native_unix.go` | `os.Root.OpenFile` and `os.Root.OpenRoot` with `O_NOFOLLOW`; stdlib `syscall.Openat` | `os.Root` resolves contained symlinks, including after an `ELOOP` from `O_NOFOLLOW`, so a contained link opens instead of being refused. The reader keeps `*os.File` parent handles, and `os.Root` cannot be built from one. `syscall.Openat` exists on Linux only, not on macOS. |
| `native_metadata_linux.go`, `native_metadata_darwin.go` | `os.Root.Lstat` followed by `os.Root.OpenFile` | An open between `Lstat` and the identity check can reach a FIFO or device swapped in meanwhile, and opening a device can have side effects. `O_PATH` on Linux and `O_EVTONLY` on macOS keep a handle without opening contents. The standard library exposes no metadata-only open through `os.Root`. |
| `native_stat_unix.go` | `os.Root.Lstat`; stdlib `syscall.Fstatat` | `os.Root.Lstat` needs an `os.Root`, not the reader's `*os.File` parent. `syscall.Fstatat` is not defined on macOS, where `O_EVTONLY` can still need read permission, so the no-follow `fstatat` fallback comes from `x/sys/unix`. |
| `native_windows.go` | `os.Root` on Windows (`internal/syscall/windows.Openat`); `os.SameFile`; stdlib `syscall` | `os.Root` always requests `FILE_GENERIC_READ`, so it cannot observe an entry whose ACL denies read data, which the reader reports as existing. It opens with `OBJ_CASE_INSENSITIVE` and exposes no flag to clear it, so a wrong-case name would open the real entry instead of being treated as a different name. `os.SameFile` compares the 64-bit file index, while ReFS needs the 128-bit `FileIdInfo` identity. The stdlib `syscall` package has no `GetFileInformationByHandleEx` (for full identity and directory case sensitivity), `GetDriveType` (to refuse mapped network drives) or `NtCreateFile` (for `OBJ_DONT_REPARSE` on every component). |
| `native_stat_windows.go` | — | A three-line wrapper over `native_windows.go`. |
| `native_other.go` | — | The refusal stub for hosts outside Linux, macOS and Windows. |

The write path does not need these guarantees. It opens only selected regular
targets that it must read and write anyway, and it compares every opened
entry with the identity recorded at selection, so a case-insensitive or
followed lookup either reaches the recorded file or refuses. The two adapters
therefore stay separate.

## Checked lifecycle

Creation, writes, metadata, flush, close, rename, cleanup and directory sync have
checked outcomes. Primary failures and later close or cleanup failures are joined
with inspectable error identities. Cleanup validates ownership before unlinking;
failed validation or removal reports the owned staging name without removing a
foreign entry. If the initial stage identity cannot be inspected, its name is
reported and retained rather than guessed safe to remove.

Source and final-stage verification close failures refuse before publication.
Once the saved bytes and identity have been observed, a later observation-close
or directory-sync failure retains Published and Saved and reports the error; that
failure alone does not make observed content unknown. Close consumes each owned
handle once, even when it returns an error. Cleanup is bounded by the already
admitted stages and held directories and still runs after cancellation or
residual exhaustion. No rollback is attempted.

The validator builds an immutable target index and dependency/source manifest
once, admitting their maps and copied edges before construction. Publication
refuses a resource/target key collision when their retained observations differ,
rather than overwriting one observation. Publication meters only reachable stack/seen/dependency records and every visited key/edge;
it does not repeatedly reserve the entire project's unrelated source inventory.
Native 128/256/512 independent-publication controls consume approximately 218
nodes per file, alongside cyclic dependency and sticky-exhaustion controls.

## CLI apply and observed results

The command validates arguments, selected configuration and all candidate content
before the first write. Governing definitions retain their same-read native
identity and byte hash; governing missing names are authoritative absence
dependencies. A changed definition or appeared ancestor refuses affected writes.
Logical report paths are explicitly mapped to native acquisition authority.

Per-file publication continues after independent I/O failures. Successful writes
remain written after a later sync/close/unlock error. Unknown outcomes do not
become confirmed writes. The command confirms otherwise unobserved targets with
bounded stable acquisition, then analyzes observed saved snapshots. After a complete
validation this work uses the fresh allowance described under
[saved fix runs](limits.md#saved-fix-runs); otherwise it uses the original residual
budget. Unavailable saved content has explicit coverage.
Candidate identities remain separate from saved result snapshots.

Targets whose retained proof depends on another changed candidate are
conservatively deferred, including reverse order and cycles. This avoids treating
a preview overlay as a saved multi-file transaction. Independent changes can
still be saved. No rollback of earlier publication is attempted.

Before acquisition, response admission reserves every target outcome, original,
candidate and saved identities, confirmed changes, error categories and one
owned staging sibling name of up to 255 bytes per target (the current native
name is 45 bytes). The reporting fallback retains these essential outcomes and
`files[].leftovers`, omitting only whole optional details with exact counts.
A too-small mandatory manifest prevents all writes.
