<!--
Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).

WSO2 LLC. licenses this file to you under the Apache License,
Version 2.0 (the "License"); you may not use this file except
in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing,
software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
KIND, either express or implied.  See the License for the
specific language governing permissions and limitations
under the License.
-->

# Shared workspace volume — shipped end state

How aep-api and agents share `/workspaces`: one RWO PVC, co-located pods,
bounded retention, and the accepted single-node limitation.

## 0. The mount is a cache, EXCEPT `runs/`

Every tree on this volume but one is derived: mirrors, snapshots, staging and
trash can all be produced again from git or from Postgres, which is why they can
be evicted under disk pressure and why losing the PVC is an availability trap
rather than data loss.

`runs/<orgId>/<cycleId>/` is the exception. It holds the coding agent's RUN
RECORDINGS — a cycle's feed as v2 `RunEvent` NDJSON — and a run's feed existed
while its pod did and nowhere else, so a deleted recording cannot be
regenerated. It is **observability, not ledger**
([ADR-0027](../../../docs/decisions/ADR-0027-run-recordings-are-observability-not-ledger.md)):
the `run_cycles` row in Postgres stays the system of record for what happened to
a version, and `RunCycleView.recording` tells a console what the platform can
actually serve (`none | recording | complete | gaps | lost`). Losing a recording
costs a feed, never a fact.

The practical consequences are in §3: `runs/` has its own 30-day retention pass,
and quota/LRU eviction under disk pressure never touches it.

## 1. Shared RWO PVC `/workspaces` (D2, 2026-07-31)

One PersistentVolumeClaim mounts at `/workspaces` on aep-api (read-write) and
agents (read-only). aep-api is the sole writer: bare mirrors under
`repos/<org>/<project>/<repoSlug>/`, immutable per-SHA snapshots, the project's
reference-document store, the coding-agent run recordings under
`runs/<org>/<cycleId>/`, `trash/`, and `tmp/`. Agents derive snapshot paths
from turn `WorkspaceRef` IDs + SHAs and never write the mount.

**Reference documents** (`<repoDir>/references/`, console ADR-0017) are the one
thing here that is not derived from git. They are the files a user attaches on
the create view — transient turn inputs, never committed — and a snapshot is
`git archive` of a tree, so they cannot arrive through it. `Ensure` copies them
into the extracted snapshot at `specs/requirements/references/` before it is
published, which is what makes them readable by a turn. Two properties follow
from the store living *inside* the repo dir rather than in a sibling tree: the
per-org quota's whole-subtree `duDir` already counts them, and `TrashRepo`
already deletes them with the project. The overlay is best-effort — a failure
must not fail a snapshot every turn depends on — so a lost overlay is silent to
the user and logged at WARN.

Access mode is **ReadWriteOnce**. Size is **10Gi**. Ticket-11 D2 kept the
shared volume (reversed 2026-07-31); content does not move over HTTP.

## 2. Placement: shared-label podAffinity (hostname)

Both Deployments carry the same pod label
(`aep.io/workspace-colocation=<value>`) and a
`requiredDuringSchedulingIgnoredDuringExecution` podAffinity on that label with
`topologyKey: kubernetes.io/hostname`. The first pod schedules freely; later pods
must land on the same node so the RWO volume is attachable.

Not used:

- **hostname `nodeSelector`** — pins to a specific node name and fights
  reschedule after node loss.
- **agents → api podAffinity** — one-way affinity leaves aep-api free to move
  alone; mutual shared-label affinity keeps both sides together.

No `strategy: Recreate`. Identical `fsGroup` on every mounting pod (leaf
securityContext patch so `runAsNonRoot` is preserved).

## 3. Retention, maintenance, admission

| Knob | Value |
|---|---|
| `AEP_WORKSPACE_SNAPSHOT_MAX_AGE` | 1h |
| `AEP_WORKSPACE_TRASH_MAX_AGE` | 1h |
| `AEP_WORKSPACE_RECORDING_MAX_AGE` | 30d (720h) |
| `AEP_WORKSPACE_RECORDING_MAX_BYTES` | 0 (per-cycle cap off) |
| `AEP_WORKSPACE_ORG_QUOTA_BYTES` | 2 GiB (2147483648) |
| Disk watermarks | high 85% / low 70% |
| Admission | refuse new snapshots at ≥ 90% used (bytes or inodes) |

Reaper sweep order (seven passes per tick; authority is the `reaper` package
doc). Every replica runs 1–4; leader-only (`<root>/.reaper.lock`) for 5–7:

1. **tmp reclamation** — purge `tmp/` entries older than `TrashMaxAge` (skip
   `askpass.sh`)
2. **trash age-purge** — purge `trash/<id>` older than `TrashMaxAge`
3. **snapshot age-reap** — trash aged non-HEAD `snapshots/<sha>` dirs
4. **recording retention** — purge `runs/<org>/<cycleId>` older than
   `RecordingMaxAge`, then hold each org under `OrgQuotaBytes` oldest-first.
   Days rather than hours because this tree is not rebuildable (§0), and aged
   from the NEWEST file in the directory — a recording is appended to for the
   whole run, so the directory's own mtime would date it from the moment the run
   started
5. **orphan reconciliation** — on-disk `repos/…` vs DB rows (mtime grace)
6. **git maintenance** — before quota so eviction sees reclaimed space
7. **quota / watermark eviction** — org quota then global high/low watermarks.
   It walks `repos/` ONLY: a cache may be thrown away to make room, the only
   copy of something may not, so recordings are bounded by pass 4 instead

Two-phase delete: rename into `trash/<ulid>` (canonical path frees instantly;
bytes stay allocated), then purge by age — or under pressure (below).

### Trash-eviction cascade

Rename-into-trash frees **zero** bytes on `statfs`. Age-gated trash purge alone
would leave the volume over the high watermark across ticks and cascade-drain
`repos/`. Shipped fix: when over the high watermark, **purge `trash/`
unconditionally first**, re-read `statfs`, and only then evict snapshots /
mirrors if still over. Open readers survive (POSIX keeps deleted-but-open
inodes alive). ENOSPC takes the same emergency trash purge via `ForceSweep`.

Git maintenance (never `git gc`, never `git maintenance --task=loose-objects`):

1. `repack -ad`
2. `prune --expire=2.hours.ago`
3. `pack-refs --all --prune`

Gate: >1000 loose objects or >20 packs; ~10 repos per tick; exclusive flock
with ~2s timeout then skip. `repack.writeBitmaps=false` on mirrors.

Admission at 90%: `Ensure` refuses a new snapshot when recorded usage ≥ 90%
(max of byte used% and inode used%) and the dest does not already exist;
in-flight reads and existing snapshots keep working. ENOSPC triggers an
emergency trash sweep and surfaces `ErrDiskFull`.

## 4. Accepted limitation: single-node RWO

Node loss takes the whole workspace plane offline until the volume reattaches
(~6–12 minutes typical). The design does **not** scale writers or readers past
one node: RWO + hostname co-location is the capacity ceiling.

## 5. PVC lifetime / availability trap

OpenChoreo prunes Flux-style: remove the provisioning trait, or delete the
Component / ReleaseBinding that owns the claim, and the PVC is Deleted. Every
PVC carries `kubernetes.io/pvc-protection`, so a mounted claim goes to
`Terminating` and **waits** — it does not vanish under a running pod. The
failure mode is an **availability trap, not data loss**: the PVC stays
`Terminating` until every consumer stops, and re-adding the trait cannot create
a same-named claim while the old one is still terminating. Recovery: scale both
aep-api and agents Deployments to zero, let the claim clear, then re-add.

Blast radius is a cold start for everything under `repos/`, `trash/` and `tmp/`
— all rebuildable. It is **permanent** for `runs/`: the run recordings there
have no other copy (§0). Nothing the platform decides depends on them, so a lost
`runs/` tree costs history and no correctness, and the affected cycles report
`recording: lost` rather than pretending their agents said nothing.

## 6. Collab

Collab-server runs **replicas: 1**, `/healthz` probes, 512Mi memory limit,
`terminationGracePeriodSeconds: 30`, concurrent shutdown flush. D6 keeps access
tokens fresh over the stateless channel for long sessions; residual: a
last-leave forced flush often has no client for `token-please`, so exposure
stays the ≤60s commit debounce window.

## 7. Rejected alternatives

| Approach | Outcome |
|---|---|
| HTTP content bundles / `/internal/v1` bundle endpoint + agents HTTP client | Cancelled — D2 kept the shared mount; content does not move over HTTP |
| RWX in dev (multi-node flock) | Declined — stay RWO + co-location |
| Register #1 (persist HEAD/tags in Postgres / `PersistingWorkspace`) | Cancelled |
| Snapshot-layer removal | Cancelled |

## 8. Related concerns

- **Collab upstream:** console nginx can override the collab upstream via
  `COLLAB_SERVER_URL`; that is a deployment concern, not a workspace-volume
  change.
- **Anthropic credentials:** aep-api still applies/deletes per-org
  `anthropic-credentials` Secrets via the in-cluster Kubernetes client on org
  disconnect. **Decision:** retire that path with phase 08's k8s-client
  deletion wave (`learn/get-cloud-working-v2/plan/phase-08-coding-agent-oc-job.md`
  — ticket 11 D7 loose end): expected outcome is delete the workflow-plane
  Secret push once runners read Anthropic keys through ExternalSecret; aep-api
  must not keep a Kubernetes client solely for this call.
