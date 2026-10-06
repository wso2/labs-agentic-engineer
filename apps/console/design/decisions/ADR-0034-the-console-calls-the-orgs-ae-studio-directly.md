# ADR-0034: The console calls the org's AE Studio directly

- **Status:** Accepted
- **Date:** 2026-10-06
- **Builds on:** [ADR-0003](./ADR-0003-contract-and-codegen.md) (contract-first
  codegen, which holds: each new client is generated from a committed spec),
  [ADR-0004](./ADR-0004-mock-layer.md) (the mock layer),
  [root ADR-0040](../../../../docs/decisions/ADR-0040-design-work-runs-in-the-organizations-ae-studio.md)
  (design work runs in the org's AE Studio),
  [root ADR-0041](../../../../docs/decisions/ADR-0041-ae-studio-checks-platform-idp-tokens-itself.md)
  (AE Studio checks the user's token itself).

## Context

Chat turns, the spec Room and spec file reads run in the organization's AE
Studio: one pod per org with three containers, `ae-design-agent`, `ae-collab`
and `ae-studio-tools`
([design](../../../../components/dataplane/ae-system-project/ae-studio/design/README.md)).
Each container serves its own `/v1` API on its own public host, and checks the
platform IdP token on every request. The pod has no fixed address, can be
absent (no GitHub token yet), and rolls on every release, secret save or
model-connection edit.

So the console needs three things it did not need when `aep-api` was its only
backend: to learn where AE Studio is, to decide what to show while AE Studio is
not ready, and to talk to two more APIs whose error bodies differ from
`aep-api`'s.

## Decisions

1. **`AeStudioGate` owns `GET /api/v1/ae-studio`.** It sits right inside
   `OnboardingGate` in `routes/__root.tsx`, so it runs only for an onboarded
   org. The answer is a `state` (`absent`, `provisioning`, `ready`, `failed`)
   and, only when `ready`, `urls` for `designAgent`, `collab` and `tools`.

2. **What the gate shows follows the state and the session.**

   | State | Console |
   |---|---|
   | session's first answer is `provisioning` (an upgrade on visit) | the **hold**: the whole console waits on *Upgrading AE Studio*, for at most 5 min (`AE_STUDIO_HOLD_CAP_MS`), then the console with the banner |
   | `provisioning` after `ready`, after the cap, after `failed`, or after a failed first read | the console with the **banner** *AE Studio is restarting…*; only pod-backed surfaces wait |
   | `failed` | a full page, *AE Studio couldn't start*, with **Try again** (re-reads the state) and **Open Settings** |
   | `absent`, `ready`, first read in flight, or a failed read | the console, no hold |

   `/settings` is never held and never replaced by the failed page: a setting
   is the usual fix. A console shown on a failed first read is never pulled
   back under the hold; a later `provisioning` is a restart. The copy is in the
   lexicon's **AE Studio** section.

3. **The state is polled only while it moves.** `useAeStudio` refetches every
   2 s while `provisioning`, every 5 s while the read itself fails, and on
   window focus otherwise. The focus refetch matters beyond freshness: the GET
   is what starts a converge on the backend. A pod read that answers 503 or
   not at all re-reads the state (`useReReadAeStudioOnOutage`), so a restart
   surfaces as the banner instead of one failure per read.

4. **One generated client per OpenAPI container, built when its URL arrives.**
   `designAgent()` and `studioTools()` (`api/aeStudio.ts`) are `openapi-fetch`
   clients typed from `packages/contracts/api/ae-design-agent/v1/openapi.yaml`
   and `packages/contracts/api/ae-studio-tools/v1/openapi.yaml` (the `gen`
   script). Each `useAeStudio` answer calls `setAeStudioUrls` before any
   consumer sees it: `ready` builds a client at `<url>/v1` (same URL, same
   client), anything else drops both. Before `ready` either accessor throws
   `AeStudioNotReadyError`. Pod-backed queries spread `usePodQueryOptions()`,
   which enables them only while `ready` and paces a 503 by its
   `Retry-After` (5 s without one). The Room is the third container and not an
   OpenAPI client: `useCollabSpec` joins `<collab>/v1/rooms` while `ready` and
   keeps the held socket through a later `provisioning`.

5. **Every client shares `authFetch`.** `aep-api`'s client and both pod
   clients wrap `createAuthFetch`: the session's bearer on every request, one
   silent renew and retry on a 401, then the sign-in redirect. The pods take
   the user's own token, so the console holds no second credential. The Room
   sends the same token and pushes each renewal to the open socket.

6. **`errors.ts` reads both error shapes.** `aep-api` answers
   `{code, message, details?}`; the pods answer problem+json
   `{type, title, status, detail?, code}`. `apiErrorMessage` takes the first of
   `message`, `detail`, `title`; `apiErrorCode` reads `code`, which both shapes
   carry, so callers branch on the code (`turn_in_progress`,
   `replay_truncated`, `github_not_connected`) and never on a message.
   `StudioToolsError` adds the status and `Retry-After` for pod reads.

7. **Mocks serve the pods on fixed fake origins.** `mocks/fixtures/aeStudio.ts`
   answers `ready` with `http://ae-design-agent.mock`, `ws://ae-collab.mock`
   and `http://ae-studio-tools.mock`; pod handlers match those origins exactly,
   so they never collide with `*/api/v1/...`. The `aep:mock:aeStudio`
   localStorage knob plays `provisioning`, `failed` and `absent`.

## Consequences

- A turn's SSE stream goes straight from the design agent to the browser. A
  reload or a dropped stream re-attaches with `?from=<frame>`; a send refused
  with 409 `turn_in_progress` attaches to the running turn instead of failing.
- The console never builds a pod URL. A new AE Studio route is a contract
  change in the container's spec plus a regenerated type, like any `aep-api`
  route.
- `aep-api` still answers everything that is not design work, so a
  `ready`-only surface is the exception: Settings, Builds, Deployments and
  Usage work while AE Studio is absent, restarting or failed.

## Rejected

- **Proxy the pods through `aep-api`.** One origin and one error shape, but a
  second hop on every SSE frame and Room message, and `aep-api` holding the
  user's session for calls it does not own. Checking the user's token in the
  pod is what lets the console call it directly (root ADR-0041).
- **One client for all three containers.** They have different specs on
  different hosts, so one client would need hand-written types or a merged
  spec, which breaks ADR-0003's one-contract-one-client codegen.
