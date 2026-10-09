# AGENTS.md — services/

| Folder | Tech |
|---|---|
| `aep-api/` | Go BFF; replays the GitHub webhook deliveries the org's AE Studio forwards and calls its `ae-studio-tools` for every git operation |

The design agent, the Room and the AE Studio tools run in each organization's
pod, not here: see [`../components/AGENTS.md`](../components/AGENTS.md).

## Conventions

- Config/env parsing in one place per service; add every var to `.env.example`.

## Documentation — current state, never a plan

- Every `README.md` / `design/` doc describes the **shipped end state**, not the journey. No
  migration/phase language ("defer to P9", "not yet carved", "still in models/"). Plans live in
  issues/PRs, not the tree.
- `aep-api` is documented as a **README ladder** ([`aep-api/README.md`](aep-api/README.md), ADR-0008):
  the service README is the map hub (domains · conventions · cross-cutting invariants); each domain
  README covers that domain (Slices · Ports · Owns · Invariants); `internal/arch` tests are the
  executable truth. Cross-cutting rules live once at the hub, not per domain.
- Change the architecture → update its README in the **same commit**. A README describing a superseded
  layout is a bug.

## Practices

- Test driven development is preferred. Write tests first, then implement the feature. Define the contract first, then write the test case for that contract, then implement the feature. You can tweak along the way.
- API changes are contract-first: edit `packages/contracts/api/`, regenerate every consumer ([`packages/contracts/AGENTS.md`](../packages/contracts/AGENTS.md)), and let the strict-server compile errors drive the handler updates.
- Before making changes, think on the code structure and where does the change belong. 
- Dead code: the gate is in the root `AGENTS.md`; `make -C aep-api deadcode-check` treats tests as non-callers, and its marker policy is inline in `aep-api/scripts/deadcode.sh`.
