# Native per-file replacement

Issues #146 and #147 implement acquisition-bound stale protection and production
per-file replacement in `internal/files`, with a retained semantic-result adapter
in `internal/fix`. CLI apply uses this retained authority and reports observed
saved snapshots, partial publication outcomes and remaining saved-content findings. Dry-run does not call this path, create stages or acquire
filesystem locks.

## Verified matrix

The native adapter is exercised natively on Darwin arm64 with cgo and local APFS.
Other operating systems and architectures, Darwin without cgo, and other
filesystem types refuse;
cross-builds do not establish native guarantees. Linux and Windows remain target
platforms with no enabled native publication adapter. Network, cloud and virtual
filesystems have no inferred support.

A supported target is a regular file with writable owner permissions, ordinary
owner/group/mode, no ACL entries, no file flags, no set-ID/sticky bits, and no
extended attributes except the bounded `com.apple.provenance` attribute. That
attribute is inspected, compared and copied exactly. This includes refusing
resource forks, other named streams, security labels and unknown xattrs, rather
than discarding them. Read-only and immutable files are refused. Chown/chmod or
metadata inspection/copy failures refuse publication; restrictions are never
cleared. Newly inherited staging ACLs are refused before content is written.

## Authority and confinement

`AcquireWrite` uses the existing bounded read and immutable Source construction.
It retains the same read's native entry and full parent-chain identities and
security metadata. Bounded native ACL snapshots also bind every parent directory's
permissions. Every path component opens with `openat`, `O_NOFOLLOW` and
nonblocking directory/regular-file checks. Final publication uses `renameat` on
the pinned parent. Absolute canonical paths must already name the intended
non-symlink scope; inspection's explicit symlink exception grants no write access.

`WriteSet` owns the observations and residual budget. Fresh equal-byte Sources,
mutable acquisition descriptions and detached `fix.FileView` values cannot
manufacture approval. `fix.Validated.Publish` uses its privately retained
original/candidate sources and dependency graph, requiring the same original
budget. Cancellation of the acquisition context remains binding even if a caller
passes a fresh context. Both contexts remain binding throughout dependency
traversal, lock waiting and the last pre-rename checkpoint. `WriteSet.Publish` is the lower-level native primitive and does
not itself claim semantic correction approval.

Resource-only acquisitions cannot become publication targets. Content and
absence observations remain distinct. Required configuration/permission resources
are declared at acquisition; selected-target and resource dependencies retained
by the validator are also checked. Exact bytes, entry identity, type, owner,
group, mode, link count, flags, provenance and parent identity/permissions are
revalidated under coordination immediately before publication. Changes with
restored mtime and identical-byte inode replacement are stale. No rebasing occurs.
After this operation observes one of its own replacements, later dependency
checks use that saved identity/content while retaining the original acquisition
binding. Saved-view cross-file reconciliation belongs to #148.

## Coordination and publication

Darwin `flock` locks the pinned parent directory, ordered by native device/inode.
It serializes even different logical targets within one parent, a deliberate
concurrency cost. Parent replacement is stale, and replacing a target inode does
not replace the lock domain. No lock files are created or removed. Waits are
nonblocking polls bounded by two seconds, context cancellation and the same
residual budget. Locks span final validation and publication; process exit
releases them. There is no unlocked fallback.

The production boundary creates an unpredictable exclusive sibling with mode 0600,
writes complete candidate bytes, preserves supported metadata, checks staged
content/identity, syncs and closes it, revalidates original inputs, and publishes
with parent-relative native rename. It then observes the saved entry and syncs
the directory. It never truncates or deletes the target first. Cleanup removes
only the owned stage and refuses detected stage substitution. Failure reports
owned leftover names. Hard-link aliases remain separate logical targets: other
aliases retain the original inode/content, and `HardLinks` records the observed
original link count.

Equal retained candidates return `NoChange=true` with no Saved snapshot. This
explicitly means no native confirmation was performed; callers must not use it
as an observed saved view. It creates no stage or replacement, including empty
files, and does not claim writable acquisition authority.

Prepublication failures leave the original untouched. Native rename errors are
reconciled against the staged and target identities: observed publication is
retained, an intact original/stage pair establishes refusal, and ambiguous entry
state is explicitly uncertain. `Publication.Published`
retains successful rename evidence even if later sync/cancellation fails;
`Uncertain` and an absent Saved snapshot describe failed saved-state observation.
An uncertain state cannot authorize later dependent writes without a new
operation. Published content is never blindly rolled back over newer content.
This boundary does not promise whole-operation transactions or power-loss
survival. #148 owns final CLI outcomes,
independent-file continuation and saved-view reanalysis.

## Accepted concurrent-writer limitation

Directory advisory locks coordinate participating writers only. A noncooperating
writer can modify an input, dependency, parent or private staging entry after the
last check and before the native rename/unlink. Detected changes refuse; that
remaining interval is not compare-and-swap. A deterministic native test actually
changes the target after the final check and demonstrates its newer bytes being
overwritten. Full no-lost-update protection requires exclusive/coordinated access.
No protection against privileged mount manipulation is claimed. Post-write
snapshots describe observed bytes, which external writers can change again.


The validator builds an immutable target index and dependency/source manifest
once, admitting their maps and copied edges before construction. Publication
refuses a resource/target key collision when their retained observations differ,
rather than overwriting one observation. Publication meters only reachable stack/seen/dependency records and every visited key/edge;
it does not repeatedly reserve the entire project's unrelated source inventory.
Native 128/256/512 independent-publication controls consume approximately 218
nodes per file, alongside cyclic dependency and sticky-exhaustion controls.
Final stage and observed saved metadata both recheck ACL/xattr support and exact
provenance. This host silently retains protected provenance after a set request;
exact mismatch rejection is therefore also independently tested against a native
observation with a different expected value, without claiming a successful
native provenance mutation.

## Checked lifecycle

Creation, writes, metadata, flush, close, rename, cleanup and lock release have
checked outcomes. Primary failures and later close/cleanup failures are joined
with inspectable error identities. Cleanup validates ownership before unlinking;
failed validation or removal reports the owned staging name without removing a
foreign entry. If the initial stage identity cannot be inspected, its name is
reported and retained rather than guessed safe to remove.

Source and final-stage verification close failures refuse before publication.
Once the saved bytes and identity have been observed, a later observation-close,
directory-sync or lock-release failure retains Published and Saved and reports
the error; that failure alone does not make observed content unknown. A failed
unlock disposes all retained parent descriptors and invalidates the acquisition,
preventing reuse of a lock whose release was not confirmed. Close consumes each
owned descriptor once, even when it returns an error. Cleanup and lock disposal
are bounded by the already admitted stages and held descriptors and still run
after cancellation or residual exhaustion. No rollback is attempted.

## CLI apply and observed results

The command validates arguments, selected configuration and all candidate content
before the first write. Governing definitions retain their same-read native
identity and byte hash; governing missing names are authoritative absence
dependencies. A changed definition or appeared ancestor refuses affected writes.
Logical report paths are explicitly mapped to native acquisition authority.

Per-file publication continues after independent I/O failures. Successful writes
remain written after a later sync/close/unlock error. Unknown outcomes do not
become confirmed writes. The command confirms otherwise unobserved targets with
bounded stable acquisition, then analyzes observed saved snapshots using the
original residual budget. Unavailable saved content has explicit coverage.
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
