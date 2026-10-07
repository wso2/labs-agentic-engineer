# ADR-0003 — The console calls the org's AE Studio directly

**Status:** Accepted · 2026-10-06 (as console ADR-0034 of the classic series;
renumbered into this series and rewritten for this console, 2026-10-07)
**Builds on:** classic console
[ADR-0003](https://github.com/wso2/labs-agentic-engineer/blob/classic-console/apps/console/design/decisions/ADR-0003-contract-and-codegen.md)
(contract-first codegen: each client is generated from a committed spec) and
[ADR-0004](https://github.com/wso2/labs-agentic-engineer/blob/classic-console/apps/console/design/decisions/ADR-0004-mock-layer.md)
(the mock layer), both at the `classic-console` tag;
[root ADR-0045](../../../../docs/decisions/ADR-0045-design-work-runs-in-the-organizations-ae-studio.md)
(design work runs in the org's AE Studio);
[root ADR-0046](../../../../docs/decisions/ADR-0046-ae-studio-checks-platform-idp-tokens-itself.md)
(AE Studio checks the user's token itself).
**Folds in:** the 2026-10-06 amendment of classic console
[ADR-0017](https://github.com/wso2/labs-agentic-engineer/blob/classic-console/apps/console/design/decisions/ADR-0017-references-are-transient-turn-inputs.md)
(where reference documents live), decision 8.

## Context

Chat turns and the spec Room run in the organization's AE Studio: one pod per
org with three containers, `ae-design-agent`, `ae-collab` and
`ae-studio-tools`
([design](../../../../components/dataplane/ae-system-project/ae-studio/design/README.md)).
Each container serves its own `/v1` on its own public host and checks the
platform IdP token on every request. The pod has no fixed address, can be
absent (no GitHub token yet), and rolls on every release, secret save or
model-connection edit.

So the console needs three things it does not need for `aep-api`: to learn
where AE Studio is, to decide what to show while AE Studio is not ready, and to
talk to a second API whose error bodies differ from `aep-api`'s.

## Decision

1. **`AeStudioGate` owns `GET /api/v1/ae-studio`.** `GatedShell`
   (`features/shell/components/GatedShell.tsx`) nests the gates as
   `AuthGuard > OnboardingGate > AeStudioGate > Shell`, so the AE Studio gate
   runs only for an onboarded org. The answer is a `state` (`absent`,
   `provisioning`, `ready`, `failed`) and, only when `ready`, `urls` for
   `designAgent`, `collab` and `tools`.

2. **What the gate shows follows the state and the session.**

   | State | Console |
   |---|---|
   | session's first answer is `provisioning` (an upgrade on visit) | the **hold**: the whole console waits on *Upgrading AE Studio*, for at most 5 min (`AE_STUDIO_HOLD_CAP_MS`), then the console with the banner |
   | `provisioning` after `ready`, after the cap, after `failed`, or after a failed first read | the console with the **banner** *AE Studio is restarting…* above the page (`Shell`'s main column); only what AE Studio serves waits |
   | `failed` | a full page, *AE Studio couldn't start*, with **Try again** (re-reads the state) and **Open Settings** |
   | `absent`, `ready`, first read in flight, or a failed read | the console, no hold |

   `/settings` is never held and never replaced by the failed page: a setting
   is the usual fix. A console shown on a failed first read is never pulled
   back under the hold; a later `provisioning` is a restart. The copy is in the
   lexicon's **AE Studio** section.

3. **The state is polled only while it moves, and re-read on an outage.**
   `useAeStudio` refetches every 2 s while `provisioning`, every 5 s while the
   read itself fails, and on window focus otherwise; the focus refetch also
   starts a converge on the backend. `useReReadAeStudioOnOutage` re-reads it
   whenever a request finds AE Studio not serving: a failed query with a pod
   503 or no answer, or `aep-api`'s 503 `ae_studio_unavailable`; and a
   design-agent call outside any query (the chat's), through the pod client's
   outage channel (`onPodOutage`). A restart then shows as the banner instead
   of one failure per surface.

4. **One generated client, for the design agent, built when its URL
   arrives.** `designAgent()` (`api/aeStudio.ts`) is an `openapi-fetch` client
   typed from `packages/contracts/api/ae-design-agent/v1/openapi.yaml` (the
   `gen` script). Each `useAeStudio` answer calls `setAeStudioUrls` before any
   consumer sees it: `ready` builds the client at `<designAgent>/v1` (same URL,
   same client), anything else drops it, and before `ready` the accessor
   throws `AeStudioNotReadyError`. The chat's two adapters
   (`agent-chat/api/turns.ts`, `api/conversation.ts`) are its only callers,
   through `designAgentCall`; the chat store loads a project's conversation
   only while the client exists, and again once it is back.

5. **The Room is joined at AE Studio's address, and kept past its token's
   `exp`.** `SpecRoom` (`spec/collab/specRoom.ts`) joins
   `<collab>/v1/rooms`, room `spec-<org>-<project>`, while `ready`, and keeps
   the held address and socket through a later `provisioning`. Every OIDC
   renewal goes up through Hocuspocus token sync (`sendToken()`). A bearer the
   Room drops (expired, or refused) renews the session once per room and
   rejoins; a second loss before that rejoin synced leaves the room offline. A
   commit waits 50 s, outside the pod's Files-socket budget. Spec files are
   read from the Room: the browser has no client for `ae-studio-tools`' `/v1`.

6. **Every client shares `authFetch`.** `aep-api`'s client and the pod client
   wrap `createAuthFetch`: the session's bearer on every request, one silent
   renew and retry on a 401, then the sign-in redirect. The pods take the
   user's own token, so the console holds no second credential.

7. **Errors keep their code and their pace.** `aep-api` answers
   `{code, message, details?}`; the pods answer problem+json
   `{type, title, status, detail?, code}`. `apiErrorMessage` takes the first of
   `message`, `detail`, `title`; `apiErrorCode` reads `code`, which both shapes
   carry, so callers branch on the code (`turn_in_progress`,
   `replay_truncated`, `github_not_connected`), never on a message.
   `PodRequestError` adds the status and `Retry-After` and reads a 503 as AE
   Studio restarting. Every `aep-api` read served through AE Studio (spec
   state, versions, validation reports, design dependencies, tasks, issues,
   skills) throws `ApiRequestError` with the `Retry-After`, so `api/retry.ts`
   gives a restart six tries at its pace, and the spec state and versions say
   *AE Studio is restarting — retrying…*, *Connect GitHub to continue* or *AE
   Studio is misconfigured — contact your administrator* by the code
   (`ae-studio/model/unavailable.ts`).

8. **Reference documents are transient turn inputs cached in AE Studio.** A
   new project's documents go up through `aep-api`
   (`POST /projects/{projectName}/references`), which streams them to the
   org's AE Studio; `ae-studio-tools` keeps them in the pod's cache and
   overlays them into the turn's snapshot under
   `specs/requirements/references/`. A pod roll wipes them, so they are never
   durable and never committed.

9. **Mocks serve the pod on fixed fake origins.** `mocks/fixtures/aeStudio.ts`
   answers `ready` with `http://ae-design-agent.mock`, `ws://ae-collab.mock`
   and `http://ae-studio-tools.mock`; the conversation handlers match the
   design-agent origin exactly, so they never collide with `*/api/v1/...`.
   Mock mode keeps a local stand-in for the Room. The `aep:mock:aeStudio`
   knob plays `provisioning`, `failed` and `absent`; `aep:mock:aeStudio:reads`
   answers the spec state and versions with each refusal.

## Consequences

- A turn's SSE stream goes straight from the design agent to the browser. The
  first attach replays from frame 0; a stream cut short resumes with
  `?from=<last frame + 1>`; a long running turn whose replay overflowed
  (409 `replay_truncated`) is waited out on its status; a send refused with
  409 `turn_in_progress` attaches to the running turn instead of failing.
- The console never builds a pod URL. A new AE Studio route is a contract
  change in the container's spec plus a regenerated type, like any `aep-api`
  route.
- `aep-api` still answers everything that is not design work, so Settings,
  Builds, Deploy and Usage work while AE Studio is absent, restarting or
  failed; only their reads that go through AE Studio wait.
- The dev server proxies only `/aep-api-service`; the in-cluster nginx
  forwards only `/aep-api-service/`. The Room and the design agent are reached
  at their own hosts, whose CORS allow-list names the console's origins.

## Rejected

- **Proxy the pods through `aep-api` (or a same-origin `/collab`).** One
  origin and one error shape, but a second hop on every SSE frame and Room
  message, and `aep-api` holding the user's session for calls it does not
  own. Checking the user's token in the pod is what lets the console call it
  directly (root ADR-0046).
- **Read spec files from `ae-studio-tools`' `/v1` beside the Room.** The Room
  already holds every file the user and the agent are editing; a second
  source would disagree with it mid-edit.
- **One client for every container.** They have different specs on different
  hosts, so one client would need hand-written types or a merged spec, which
  breaks one-contract-one-client codegen.
