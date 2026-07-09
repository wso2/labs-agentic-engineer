# Skills: one flat library, kind in frontmatter (migration plan)

Status: **implemented + live-verified** (impl-skills branch, uncommitted) · Owner: Anjana · 2026-07-08
Supersedes the layout sections (§8) of `skills-repo-storage.md` once shipped.
Related: PR #90 (closed-without-merge candidate; disposition in §2).

## 1. Problem

Skills live in **three places with two disjoint vocabularies**:

- `services/aep-api/skills/builtin/` — 4 stack skills (`go`, `react-webapp`,
  `api-management`, `thunder-authentication`), kind `builtin`, authored ONLY
  inside aep-api.
- repo-root `skills/` — 5 generation-flow skills, kind `flow`, vendored into
  `services/aep-api/skills/flow/` by `go generate` (commit `2cd4edc`).
- The per-org `org-skills` GitHub repo mirrors that split as **path-encoded
  kinds**: `skills/<kind>/<name>/SKILL.md`, kinds `builtin | flow | custom |
  imported`.

Consequences:

- The playground/evals read repo-root `skills/` (`evals/skills.ts
  loadRepoSkills`) and therefore never see the 4 stack skills; the live design
  agent (snapshot-fed) sees everything. Same class of split-brain PR #90
  attacks.
- Kind is duplicated into the *path*, so every consumer (aep-api store,
  reconcile, agents `scanCatalog`, runner s2s) hard-codes the layout.
- Two authoring locations means two drift-guard mechanisms and a standing
  "which folder does this skill go in" question.

## 2. Relationship to PR #90

PR #90 ("single skill library for both agent paths") solves the same
split-brain against the *upstream* architect/playground architecture. We adopt
its ideas, not its diff (our branch has since replaced the architect agent
with the snapshot-fed main agent, and our skill bodies are ahead — v2
`dependencies[]`, committed-truth flow):

| PR #90 piece | Disposition |
|---|---|
| Repo-root `skills/` as the single authored source (incl. stack skills) | **Adopt** (this doc) |
| Skills available to the agents service / playground | **Adopt** — falls out of the single library + existing snapshot path |
| Drift-guarded byte-identical embedded copies | **Adopt mechanism we already have** — `go generate` vendor + porcelain check (`2cd4edc`), extended to the whole library; PR #90's Go-test guard not taken |
| `ArchitectFS` + wire-pushed HLA + `FALLBACK_HLA_GUIDANCE` | **Not taken** — no architect agent here; the `_skills` snapshot supplies the catalog (ADR-0004 progressive disclosure) |
| PR #90's HLA/stack skill content edits | **Not taken** — our copies are ahead (dependencies[] v2); content stays ours |
| ADR-0005 single-skill-library | **Superseded by this doc** |

PR itself: left open for now; close manually later.

## 3. Target design

### 3.1 One authored library

```
skills/                        # repo root — THE library, flat
  api-management/SKILL.md      #   (moved from aep-api builtin/)
  go/SKILL.md
  react-webapp/SKILL.md
  thunder-authentication/SKILL.md
  excalidraw-wireframes/SKILL.md        # metadata.aep.kind: platform
  high-level-architecture/SKILL.md      # metadata.aep.kind: platform
  openapi-conventions/SKILL.md          # metadata.aep.kind: platform
  task-breakdown/SKILL.md               # metadata.aep.kind: platform
  task-planning/SKILL.md                # metadata.aep.kind: platform
```

`services/aep-api/skills/` keeps a single vendored copy (`go:embed` cannot
cross the module boundary):

```
services/aep-api/skills/
  embed.go        # //go:generate rm -rf embedded && cp -R ../../../skills embedded
  embedded/       # vendored, byte-identical to repo-root skills/
```

`builtin/` and `flow/` are deleted. `BuiltinFS`/`FlowFS` collapse to one
`LibraryFS`. Make targets `vendor-flow`/`vendor-flow-check` become
`vendor-skills`/`vendor-skills-check` (same porcelain mechanism — catches
added-but-not-vendored, the drift class that hid `task-breakdown`).

### 3.2 Kind moves into frontmatter

New vocabulary (user decision, 2026-07-08): **`platform`** (was `flow`) and
**`org`** (was `builtin`); `custom`/`imported` unchanged.

```yaml
---
name: high-level-architecture
description: ...
metadata:
  aep:
    kind: platform   # platform | org | custom | imported
---
```

Rules:

- **Absent/invalid `metadata.aep.kind` ⇒ `org`** (user decision). Authored
  org skills therefore carry NO marker; only platform skills are marked in
  the source library.
- The service **stamps** `custom` (create/update) and `imported` (import)
  into the SKILL.md it writes — required, otherwise they'd read back as
  read-only `org` and reconcile would purge them as "retired".
- Semantics per kind (REVISED 2026-07-08, user decision: platform skills are
  no longer hidden — one library, all of it inspectable):
  - `platform` — generation-flow guidance; **lists READ-ONLY on the skills
    page** (own "Platform" section) and resolves by name; participates in
    the updates badge; mutations → 403; reconciled from the embed.
  - `org` — visible on the skills page; `editable=false`; reconciled from
    the embed; feeds coding-runner `skillsApplied`.
  - `custom`/`imported` — user-owned, editable, never touched by reconcile.
  - Read-only-ness is enforced by the mutation guards (`!isUserKind` →
    `ErrSkillNotEditable`), not by visibility filtering — `Resolve`/
    `ListSummaries` no longer filter kinds.

### 3.3 Per-org repo goes flat

`org-skills` repo layout becomes `skills/<name>/SKILL.md`
(+ `skills/<name>/references/*.md`) — **no kind directories** (user decision:
the source has no kind folders, so the provisioned repo shouldn't either).

- Name collisions become structural: one dir per name. The create/import
  collision check (`resolveFresh`, all kinds) already enforces this; the old
  "custom shadows builtin" *union* semantics survive only as a migration
  rule (§4, custom wins the dir).
- **Reserved skill names**: `platform`, `org`, `builtin`, `flow`, `custom`,
  `imported` are rejected by name validation — keeps the parser unambiguous
  while legacy nested layouts still exist in the wild.
- Reconcile (content-SHA, from `9f4001e`) becomes one pass over the whole
  embedded library:
  - embedded name owned by `custom`/`imported` in the repo → **skip**
    (org wins, preserved from the shadow-migration rule);
  - absent or SHA-differs → seed/overwrite (whole-dir replace);
  - repo skill of kind `platform`/`org` not in the embed → purge.
- `UpdatesAvailable` compares `org`-kind skills only (page badge; platform
  stays invisible, as `flow` was).

### 3.4 Wire / API / console

- `SkillDetailBody.kind` etc. are plain strings in the hand-managed
  `openapi.yaml` (no enum) — **no spec edit needed** for the rename; goldens
  and console types do change.
- Runner s2s: `MaterializedName("org", "go") = "org-go"`,
  `PrefixedID = "org/go"`. The runner consumes these strings verbatim
  (verify no `builtin-` hardcode during Phase 4).
- console-legacy: `SkillKind = 'org' | 'custom' | 'imported'`; chip label
  `Org`. (`apps/console` has no skills page yet — no change.)
- The spec is hand-AHEAD of code (see `openapi-spec-first-regen-hazard`):
  do **not** run `make openapi`; hand-edit if anything is needed.

### 3.5 Agents service

`scanCatalog` (`load-workspace.ts`) reads the new flat snapshot layout
`skills/<name>/SKILL.md` and stays **tolerant of the legacy nested layout**
(a first-level dir without a SKILL.md whose name is a legacy kind → recurse
one level). Old per-SHA snapshots and mixed-deploy windows then keep working
in either direction. `evals/skills.ts` `materializeSkills` writes flat
fixture snapshots. The playground already reads repo-root `skills/` flat —
after the move its catalog gains the 4 stack skills (the PR #90 outcome).

## 4. Legacy repo migration (existing orgs)

Existing `org-skills` repos hold `skills/{builtin,flow,custom,imported}/…`.
`Reconcile` (already invoked by provisioning, project creation, and
`POST /org/skills/sync`) performs the migration — no new endpoint:

1. `loadCatalog` parses **both** layouts: depth-4 `skills/<legacyKind>/<name>/SKILL.md`
   (kind from path, mapped `builtin→org`, `flow→platform`) and depth-3
   `skills/<name>/SKILL.md` (kind from frontmatter, absent→org).
2. Reconcile stages ONE commit: delete every legacy kind-dir; write the
   embedded library flat; rewrite user skills (`custom`/`imported`) at their
   flat path with the kind stamped into frontmatter.
3. Collision on flatten (legacy custom `go` shadowing builtin `go`): the
   user kind wins the dir; the embedded skill is skipped (§3.3).
4. Steady state: no legacy dirs; subsequent reconciles are pure content-SHA
   comparisons.

Rollback: the repo is git — the migration is one revertable commit per org.

## 5. Implementation phases

Per-service TDD (contract → test → implement). Root `make build test lint
typecheck` green at the end of every phase.

### Phase 1 — Single authored library + embed (aep-api ships it)
- `git mv services/aep-api/skills/builtin/* skills/` (4 stack skills, no
  metadata marker); stamp `metadata.aep.kind: platform` on the 5 flow skills.
- `embed.go`: one `LibraryFS` over vendored `embedded/`; delete `builtin/`
  + `flow/`; re-point `go:generate`; Makefile `vendor-skills(-check)`
  replaces `vendor-flow(-check)` (fix any root/CI wiring).
- `loadEmbedded*`: single loader; kind from frontmatter (absent→org,
  embedded values restricted to `platform|org` — else warn + org).
- Frontmatter parser regains a `metadata.aep.kind` field.
- **Test after**: loader unit tests (kind parsing, default, references);
  `make -C services/aep-api vendor-skills-check`; full root build.

### Phase 2 — Flat repo store + rename (aep-api skills feature)
- `repo_store.go`: `skillRepoDir(name)` (kind dropped from paths),
  `isCatalogPath`/`parseBundle` for both layouts (§4.1), kind rename
  everywhere (`kindRank`, editability = `custom|imported`, visibility =
  `!platform`), reserved-name validation.
- Mutation/import services stamp `metadata.aep.kind` on write.
- **Test after**: repurposed component/unit suites (`skill_component_test`,
  `repo_store_test`, `skill_pure_test`, mutation/import tests) with flat
  goldens; new tests: absent-kind→org, stamp-on-create/import,
  reserved-name 422.

### Phase 3 — Reconcile unification + legacy migration
- One `reconcileEmbedded` over the whole library with skip/purge rules
  (§3.3); `UpdatesAvailable` on `org` kind; migration staging (§4).
- **Test after**: reconcile tests — fresh-seed flat; drift overwrite
  (`DriftBuiltin` helper renamed `DriftOrg`); retired purge; custom-owned
  name skipped; **legacy tree in, one migration commit out** (custom skill
  preserved + stamped, shadow resolved custom-wins, kind dirs gone).

### Phase 4 — Wire surface sweep (contract, console, runner, goldens)
- Repo-wide sweep for `"builtin"` / `"flow"` skill-kind literals: models
  (`MaterializedName` callers), execution s2s tests, harvested goldens,
  `test/` integration suite, console-legacy (`SkillKind`, `skillKind.ts`,
  `openapi.gen.ts` hand-edit — spec deviated, do NOT regen), docs comments.
- Confirm the runner harness treats `materializedName` as opaque.
- **Test after**: `make test` root; console-legacy suite (reads packages
  from src); goldens re-harvested; grep returns no stale kind literals.

### Phase 5 — Agents service + evals/playground
- `scanCatalog` flat + legacy-tolerant; `materializeSkills` flat;
  playground/eval fixtures; prompt tests if catalog ordering shifts.
- **Test after**: `pnpm --filter @aep/agents test` + `test:eval`;
  playground smoke against the fixture snapshot (catalog now lists 9
  skills incl. the 4 stack skills).

### Phase 6 — Live-environment verification (§6)
- Rebuild + redeploy aep-api, agents, console-legacy images
  (`verify-fix-live-before-retest`: embedded skills changed ⇒ image rebuild
  is load-bearing; a version bump alone reconciles nothing).

## 6. Live-env checklist (fresh cluster; run after Phase 6 deploy)

1. **Health**: cluster Ready, compose services up, `aep-api` health +
   `agents` `/healthz` green. (Cluster was mid-respawn when this doc was
   written — re-verify first.)
2. **Fresh provisioning**: create org/project → `org-skills` GitHub repo tree
   is FLAT: 9 `skills/<name>/` dirs, no kind dirs; the 5 platform skills
   carry `metadata.aep.kind: platform`; stack skills carry no marker.
3. **API**: `GET /orgs/{org}/skills` → exactly the 4 `org`-kind skills,
   `editable=false`; platform skills absent; `GET /skills/updates` → `[]`;
   `POST /org/skills/sync` → no-op commit-free run.
4. **Legacy migration**: hand-commit a legacy nested tree (incl. one custom
   skill + one custom shadowing an org name) into a test org's repo →
   `POST /org/skills/sync` → ONE migration commit; flat layout; custom
   preserved + stamped + editable; shadow resolved custom-wins.
5. **Custom lifecycle**: create → flat dir + `kind: custom` stamped; edit;
   delete. Import a tarball → `kind: imported` stamped. Reserved name → 422.
6. **Generation e2e**: run a design turn — agent catalog (from the new
   snapshot layout) lists platform + org + custom skills; `loadSkill`
   returns bodies; design artifacts generate in order.
7. **Coding path**: dispatch a task → runner s2s skills payload shows
   `kind: "org"`, `materializedName: "org-<name>"`; runner materializes and
   the session sees the skills.
8. **Console-legacy skills page**: lists 4 skills with `Org` chips; custom
   CRUD works; updates badge behaves.

## 6b. Execution log (2026-07-08, local plane)

Phases 1–5 all green: aep-api full `go test ./...`; agents 130/130 unit +
37/37 evals; console-legacy 47/47 + typecheck; runner 19/19 + typecheck;
`golangci-lint` clean on touched packages. New contract tests:
`flat_layout_test.go` (stamp, dual-layout parse, flat seed, one-commit legacy
migration incl. shadow custom-wins + purge + steady-state no-op),
`TestFrontmatterKind`, `TestLoadEmbeddedLibrary`, flat/mixed `scanCatalog`.

Live (rebuilt aep-api + agents + console-legacy images, containers recreated):

- Before: old image served `kind: "builtin"`; org repo tree was legacy nested
  and **missing task-breakdown** (the pre-`2cd4edc` drift, live).
- New binary read the legacy repo through the dual-layout parser correctly
  (4 org-kind, read-only) and the badge showed all 4 stale (old `aep.version`
  frontmatter ⇒ SHA drift). ✓
- `POST /skills/sync` → **one commit** (`ac523c2`, parent = prior head
  `b4b18e4`), "9 written, 0 migrated, 0 retired"; tree flat, task-breakdown
  present, platform marker only on the 5 platform skills. ✓
- After: `GET /skills` → 4 × `kind: org`; `/skills/updates` → `[]`. ✓
- Custom lifecycle: create → `custom` + stamped file; reserved name
  (`platform`) → 400; spoofed `metadata.aep.kind: org` on create → overridden
  to `custom`; deletes 200; delete of an org skill → 403. ✓
- Fresh-org flat provisioning: covered by `TestProvision_SeedsFlatLayout`
  (single-org local plane has no second org to provision live).
- Playwright e2e (console): skills page shows the 4 org skills with `Org`
  chips, read-only, no update badges. ✓ Full generation e2e: requirements +
  design streamed live and completed; the published design's `skillsApplied`
  listed go/react-webapp/thunder-authentication/api-management/
  excalidraw-wireframes/high-level-architecture, and the turn's `_skills`
  snapshot on the volume was the flat 9-skill layout — the agent consumed
  the unified library end-to-end. ✓
- e2e also FOUND a pre-existing bug (not from this migration): console
  custom-skill create → 422, because the API schema wrongly REQUIRED
  `references` while the console dialog sends only {name, skillMd}. Fixed
  properly: `references` is now optional on CreateSkillInput +
  UpdateSkillInput (Go `omitempty` + hand-edited spec), pinned by
  `TestSkillsComponent_Create_WithoutReferences_201`, redeployed,
  re-verified live (create-without-references → 201). ✓

Runner pairing: the deployed `aep-coding-agent-runner:v5` preloads on
`kind === "builtin"`; the new BFF sends `org`, so v5 degrades gracefully
(all skills still materialize + discoverable, none preloaded). The updated
runner code (this change) accepts both; **push it as `v6` and bump
`runnerImageVersion` with the aep-api deploy** (matched-pair convention in
`config_loader.go`).

## 7. Risks / notes

- **Absent-kind = org means hand-committed repo skills are reconcile-managed**:
  an unstamped skill pushed directly to `org-skills` reads as `org` and will
  be purged as "retired" on the next reconcile. Deliberate (platform owns the
  `org` set); document in `skills-repo-storage.md` when updating it.
- **Mixed-version window**: new agents + old snapshots (nested) and old
  agents + new snapshots (flat) — covered by dual-layout `scanCatalog`; on
  the aep-api side old pods can't parse flat repos, so migration only runs
  from new pods (reconcile is the sole writer of embedded skills — fine).
- **openapi.yaml is hand-ahead** — never `make openapi` during Phase 4.
- Update `skills-repo-storage.md` §8/§6 + `shared-volume-clone-architecture.md`
  §17.8 references after shipping (design docs record final state).
