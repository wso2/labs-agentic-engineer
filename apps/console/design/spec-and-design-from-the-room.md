# Spec and design from the room

The console shows the requirements and the design as their documents say,
worked out in the browser from the collab room, so an edit — the user's or the
agent's — shows at once and nothing says the same thing twice. The platform
serves only what the documents cannot say.

## The room

`features/spec/collab/specRoom.ts`: one Hocuspocus room per project
(`spec-<org>-<project>`) for the whole app, counted by its users and closed a
few seconds after the last lets go. Its lifecycle is the old console's
`useCollabSpec` (rejoin fresh after a drop, back off, latch on a refused
bearer). Files are keyed by their repo paths (`specs/requirements/prd.md`).
`useSpecDoc` returns its doc once synced; a build flushes it first.

## The spec

`useSpecModel` (`useSpecWorkspace.ts`): features, names, purposes and stages
from the live lines (`model/features.ts`); `designedFrom` and the documents
from `GET /spec/state`; documents' coverage from `sources/*.md`
(`model/coverage.ts`). Everything the workspace shows — chips, Next up, the ID
index, design work — is derived from that (`model/workspace.ts`,
`designWork.ts`). Stage "Interviewing" is the chat's running `/interview`.

## The design

`useDesignModel` (`features/design/useDesignModel.ts`): the catalog from the
room's design files (`model/catalog.ts`), dependencies from the build
preflight, the running design from the chat. Commenting is mock-only
(`DesignModel.commenting`) until it ships.

## Builds

`useBuilds` joins `GET /versions` (what each version built) with the version
ledger (how its run went). Validation is grouped by feature from the snapshot
(`model/validation.ts`), which carries the version's scope and each failure's
standing.

## Mock mode

`VITE_API_MODE=mock` keeps a local doc seeded from the mock's files and the
mock's approved design review; the agent's writes are applied to the local doc
from the turn's stream.
