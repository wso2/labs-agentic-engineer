# spec — Spec Authoring & Versioning

> **L2 · a domain.** Part of the [aep-api architecture](../../README.md).

Turn a prompt into a versioned requirements+design Spec stored as committed truth in git, let humans and
agents co-edit it live, cut and read the `v<N>` Spec version, and steer authoring with the org's Skill
library. **Single write-authority over the git spec-content store and its version tags.**

```mermaid
flowchart LR
  API(["/api/v1"]) --> SL
  CB(["/collab/validate"]) -.-> SL
  subgraph spec
    SL["slices — genaiturns · files · tags · skills · collab"]
    CORE["artifacts store/versioning + turn engine + files + design + skills services"]
    SL --> CORE
    CORE --> GIT[("git: requirements.md · specs/design/** · v<N> tags · org-skills repo")]
    CORE --> TURNS[("agent_turns")]
  end
  CORE -->|Workspace · GitOps engine| SC[[sourcecontrol]]
  CORE -->|CRTMarkers port| DEP[[dependencies]]
  CORE -->|anthropic key · git tokens| SEC[[platform/secrets]]
  CORE -->|the genai fold| FOLD[["platform/agentfold"]]
```

## Slices
| Slice | Use-cases | Entry |
|---|---|---|
| `genaiturns` | create / get / active / stream turn + get-conversation (the AgentTurn lifecycle) | `.../agents/{cid}/messages`, `.../turns/...` |
| `files` | list / read / apply files over the project workspace | `GET/POST .../files...` |
| `tags` | list the project's `v<N>` spec version tags | `GET .../tags` |
| `skills` | list / create / update / delete / import / sync / get the org Skill library | `/skills...` |
| `collab` | the collab session descriptor + the S2S room-access oracle | `.../spec/collab-session`, `GET /collab/validate` |

*Still flat in the domain root (not carved into finer slices): the artifacts store/versioning machinery,
the genai turn engine (runner/broker/sweeper), and the files / design / skills services.*

## Ports
| Port | Dir | Peer · contract |
|---|---|---|
| `Workspace` · `GitOpsService` · `RepoService` | needs | `sourcecontrol` — the gitfs engine hosting all spec + skills git content |
| `resourceMarkerCatalog` (returns `CRTMarkers`) | needs | `dependencies` — the PE-authored CRT marker vocabulary, projected at the root |
| `AnthropicKeyResolver` · git-token `Resolver` | needs | `platform/secrets` — per-org keys + sealed git tokens |
| `ArtifactService` · `ArtifactStore` · `SplitFrontmatter` | offers | `delivery` / `projects` / `dependencies` — design reads, spec-save, status snapshots |
| `UsageReader` (turn token rollups + drafting-cycle boundary, #245) | offers | `projects` — the get-project-usage spec-phase figures |
| `CredentialsRefreshService`-adjacent turn/tag reads | offers | build/devflow (SpecTagger, validation criteria) |

## Owns
- git spec content (`requirements.md`, `specs/design/**`), the annotated `v<N>` tag (the version store),
  the org-skills repo, `AgentTurn` (turn lifecycle) + the resumable-turn SSE broker (in-memory seam).
- **The Skill library.** One flat authored library at repo-root `skills/` (`<name>/SKILL.md` +
  `references/`), COPY'd into the image and read at runtime from `config.SkillsDir` (default `/app/skills`)
  — not go:embed'd. Kind (`platform | org | custom | imported`) lives in frontmatter `metadata.aep.kind`,
  absent ⇒ `org`: platform/org are library-shipped + reconcile-managed (read-only), custom/imported are
  user-owned + editable. Each org's flat `org-skills` repo (kind in frontmatter) is reconciled
  content-SHA-wise — seed / overwrite / purge platform+org, skip user-owned.
- **Persistence**: the `agent_turns` gorm lives in this domain (`repository_turn.go` over the
  `agent_turn.go` entity), single write-authority. Spec content itself is not gorm — it lives in git,
  reached through sourcecontrol's `Workspace`/gitfs engine.

## Invariants — don't break
- **Single write-authority** over the git spec-content store and its `v<N>` tags — every save/tag/discard
  runs through this domain's gitfs Workspace engine; no other domain writes spec content.
- **`CRTMarkers` is a projection, not a re-export.** design-save reads the dependencies marker catalog
  through the `resourceMarkerCatalog` port in spec's OWN vocabulary (`CRTMarkers`), mapped by a root
  adapter — the spec domain names the dependencies domain nowhere.
- The `/collab/validate` oracle recovers the acting org from VERIFIED claims and refuses any room whose
  `spec-<org>-` prefix mismatches — never a hint of whether the room exists. Platform-wide rules (tenant
  gate, secrets fence) → [../../README.md](../../README.md).
- The genai turn is **committed-truth**: the fold (`platform/agentfold`) verifies hash-parity before the
  commit; a mismatch rejects the turn and leaves `main` untouched.
- **Skill read-only is enforced by the mutation guards, not by visibility.** `Resolve`/`List` return every
  kind — platform skills list read-only on the skills page; reserved names/prefixes block name collisions.
