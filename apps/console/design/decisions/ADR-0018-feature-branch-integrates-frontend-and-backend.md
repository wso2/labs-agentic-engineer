# ADR-0018: The feature branch integrates frontend and backend

- **Status:** Accepted
- **Date:** 2026-08-17
- **Context:** the flow had the frontend and the backend sending **separate
  PRs to `main`**, both referencing a handshake issue that had to exist
  *before* the frontend was built. Two problems. The handshake was written
  before anyone knew what the API should be, so it was a sketch — while the
  mock build is what actually discovers the contract, by making real UI code
  and typed mocks live against it. And with both halves merging
  independently, `main` could hold a frontend calling an endpoint that
  didn't exist yet, so the first place the two met was `main` itself.
  This repo is a monorepo — `apps/console` and `services/aep-api` are one
  tree — so nothing forced them apart.

## Decision

- **The feature branch is the integration point.** The frontend is built on
  mocks and pushed there; the backend's PR then targets **that branch**, not
  `main`. Only the branch merges to `main`, so a feature never reaches
  `main` in halves.
- **The handshake issue is created from a mock-validated frontend**, not
  before the build. Its body is the contract diff already on the branch —
  a proposal that has been proven to work against real UI code — plus the
  rationale, the feature issue link, and **the feature branch name**.
- **Two validation gates, proving different things.** Mock mode proves the
  frontend and produces the contract proposal (flow step 5); the local setup
  proves the two halves agree (flow step 7). Neither substitutes for the
  other.
- **The feature PR closes every issue the feature opened** — the feature
  issue always, and the handshake issue whenever there was one. GitHub fires
  closing keywords only when a PR merges into the default branch, so the
  backend's PR into the feature branch cannot close its own issue.

## Consequences

- The contract request improves: the backend is handed a diff that a working
  frontend already exercises, instead of a sketch written before anyone had
  built anything.
- The backend can no longer merge independently of the feature that asked
  for it, and vice versa. That is the point, and the cost is a longer-lived
  feature branch that has to be kept current with `main`.
- Mock-mode frontend work still proceeds against an unagreed contract, which
  is what ADR-0001's handshake pattern was protecting; only the meeting
  point moved from `main` to the branch. ADR-0017's "implemented before
  merge" now has a concrete location: on the branch.
- A feature with no contract change skips the handshake entirely and goes
  straight from mock validation to the local-setup validation.
- The old promise that a handshake issue exists *before* build is withdrawn.
  Anyone wanting early backend notice should say so on the feature issue —
  the handshake issue itself now means "here is the proven diff, please
  implement it".
