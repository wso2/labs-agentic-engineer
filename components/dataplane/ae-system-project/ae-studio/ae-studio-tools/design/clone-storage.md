# ae-studio-tools: clone storage

Where the container keeps git: one disk emptyDir, `studio-data`, treated as a
**cache of GitHub**. The code is `internal/repo` (engine, snapshots,
references) and `internal/repo/reaper` (lifecycle); the volume itself is
rendered by the ResourceType ([`../../design/README.md`](../../design/README.md)).

## Volume

- **One disk emptyDir per pod.** `ae-studio-tools` mounts it whole,
  read-write, at `AE_STUDIO_DATA_DIR` (`/studio-data`). `ae-design-agent`
  mounts only `snapshots/`, read-only, so it never sees a mirror or a
  reference store.
- **Wiped on every roll.** A roll (every release, secret save or
  model-connection edit) empties it, and each repository re-clones on its
  first use, about 1 to 3 s for an AE project repository. Nothing on it is
  the only copy of anything durable.
- **No PVC.** A per-org volume (RWO with `Recreate`) would need a block
  volume and a storage class per install, leak volumes under a `Retain`
  class, and pay an attach delay when a roll lands on another node. All of
  that buys durability for data GitHub rebuilds.

Layout under the root (`internal/repo/paths.go`):

| Path | Holds |
|---|---|
| `repos/<owner>/<repo>/git/` | the bare clone, lower-cased, shared by every caller of that repository |
| `repos/<owner>/<repo>/repo.lock` | flock: shared for reads, exclusive for fetch, push and ref moves |
| `snapshots/projects/<project>/<sha>/` | a plain-file tree of a project commit, for the agent |
| `snapshots/skills/<sha>/` | a plain-file tree of an Org skills commit |
| `references/<owner>/<repo>/` | the repository's reference documents (never committed) |
| `trash/<id>/` | two-phase delete staging |
| `tmp/` | atomic clone and snapshot staging, the askpass shim |

## Budget and admission

One pod serves one org, so there is **one budget**:
`AE_STORAGE_BUDGET_BYTES`, 2 GiB by default. It sits a full GiB under the
volume's 3Gi `sizeLimit`, because exceeding `sizeLimit` makes kubelet evict
the only pod rather than return ENOSPC.

- **Usage** is the reaper's block-usage `du` of the root at each sweep, plus
  the bytes of every clone, snapshot and reference upload since, so
  admission is not blind between sweeps.
- **Pressure** (`UsagePct`) is the higher of the budget share and the node
  filesystem's byte use from `statfs`. On an emptyDir, `statfs` reports the
  node's disk, which can fill for reasons of its own. Inodes are not watched.
- **Admission:** at 90 % pressure (`DiskAdmissionRefusePct`) a new snapshot or
  a reference upload is refused (`ErrDiskAdmission`, a `disk_full`). A
  snapshot that already exists is reused. Commits, tags and reads are never
  gated.
- **Eviction:** from 85 % of the budget, purge `trash/` first and measure
  again; if still over, evict down to 70 %: snapshots least recently used
  (never one at a mirror's HEAD or used in the last 30 min, since a turn
  reads its snapshot lazily for up to 30 min), then mirrors least recently
  fetched, skipping any whose `repo.lock` is held. Reference stores are never
  evicted.

Trash first, because a rename into `trash/` frees nothing on the root's `du`,
which the 85 % mark reads (`trash/` is under the root): without
the purge, the volume would stay over the mark and eviction would drain
`repos/` sweep after sweep.

## Reaper

One sweep every 5 min, plus a forced sweep queued on ENOSPC (which only the
node disk filling produces). Each pass is isolated: one failing never stops
the next. One pod runs one reaper, so there is no leader lock.

1. **tmp:** purge `tmp/` entries older than 1 h, keeping the askpass shim.
2. **trash:** purge `trash/<id>` entries older than 1 h.
3. **snapshot age:** trash a snapshot `<sha>` leaf unused for 1 h whose sha
   is no mirror's current HEAD.
4. **git maintenance:** on a mirror with over 1000 loose objects or over 20
   packs, at most 10 per sweep, under its exclusive flock: `repack -ad`,
   `prune --expire=2.hours.ago`, `pack-refs --all --prune`. Never `git gc`.
   It runs before the budget so eviction sees the reclaimed space. A pod can
   live for days between releases, which is why this pass stays.
5. **budget:** the eviction above. Logs `reaper.sweep {usedBytes,
   budgetBytes, pct, evicted}`.

Deletes are two-phase: rename into `trash/<id>` (the canonical path frees at
once), then purge. No pass removes or renames `snapshots/`, `projects/` or
`skills/` themselves: the agent's `subPath` mount pins that inode, so only
`<sha>` leaves go.

Passes that do not exist, and why:

- **Orphans.** Nothing lists the live repositories. An orphan (a project
  delete whose trash failed) is unreachable, because every request resolves
  its project through `aep-api`
  ([route-groups.md](route-groups.md#how-a-request-names-its-repository)).
  Eviction takes it under pressure, and the next roll wipes it.
- **Recordings.** Run feeds are read from the observer
  ([ADR-0049](../../../../../../docs/decisions/ADR-0049-a-finished-runs-feed-is-read-from-the-observer.md)).
- **Leader flock.** `Recreate` means one pod, so one reaper.

## Clone

A cold clone is `git clone --bare` into `tmp/`, then a fetch with the refresh
refspecs `+refs/heads/*:refs/heads/*` and `+refs/tags/*:refs/tags/*`, then
one rename into place. `refs/pull/*` never arrive: nothing reads them, they
grow by one per coding task, and the pod repeats the clone after every roll.
The clone runs detached from the request that started it, under its own
5 min deadline, and concurrent first readers share it; a caller that gives up
only stops waiting. Each clone logs `repo.clone {repo, mode, ms}`.

Snapshots are `git archive` of a commit into `tmp/`, then one rename:
immutable, regular files and directories only. A reused snapshot's mtime is
set to now, which is what "least recently used" reads.

## When the disk is full

- **Room saves** (the Files socket's `apply`) are commits, so admission never
  refuses them: a save wins over a turn's snapshot. An actual ENOSPC queues a
  forced sweep and answers `disk_full` (503). `ae-collab` keeps the document
  live and its baseline unchanged, so the next debounced flush retries the
  same changes; the Room reads it as restarting.
- **`aep-api` callers** get `ErrAEStudioUnavailable` for `disk_full`
  ([route-groups.md](route-groups.md#problem-codes-as-aep-api-reads-them)):
  Temporal retries, sweeps skip. A roll empties the volume, so waiting
  recovers.
- **A single repository larger than the budget** has no code of its own: it
  gets the caller's bounded retries and a log line.

## Reference documents

The files a user attaches when creating a project live in
`references/<owner>/<repo>/`, uploaded by `aep-api` through
`PUT /internal/v1/repos/{owner}/{repo}/references` (at most 10 files of
5 MiB, readable types only; the set is replaced). They are a **temporary
cache**: a roll loses them, and a project delete trashes them with the
mirror. They are never committed.

The MCP socket's project lookup copies them into each project snapshot under
`specs/requirements/references/`, with a `.aep-references.json` manifest, and
answers their names in `references[]`. The agent reads them for `/start` and
for flow turns; chat turns get none. The overlay is best-effort: a failure
logs `references.overlay_failed` and the snapshot still serves, so a turn
whose references were lost runs without them.
