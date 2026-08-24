# Eval review — chain / lunch-coordinator

- Run: 2026-08-03T12:22:29.965Z
- Transcript: playground/.projects/spec-agent-evals/chain-lunch-coordinator.transcript.md
- Raw trace: playground/.projects/spec-agent-evals/chain-lunch-coordinator.trace.json

## requirements — PASS (94)

Structural 100%:
- [x] prd.md exists
- [x] substantial (≥800 chars)
- [x] structured (≥3 headings)
- [x] interview happened
- [x] interview finished within cap

Rubric judge 88%:
- [x] (w2) Google-workspace sign-in captured; no separate registration flow invented
  - 'Sign-in is via Google Workspace SSO only — no separate account registration or password.' and User Story 1: 'As a Team Member, I want to sign in with my Google Workspace account, so that I can open or join lunch rounds without a separate registration or password.'
- [x] (w2) Daily order round lifecycle: open (restaurant + cutoff) -> teammates add own items -> lock at cutoff
  - Solution section: 'Any team member can open a lunch round against a restaurant with a known, priced menu and a cutoff time; teammates add their own items to the round before the cutoff; once the cutoff passes the round locks...' Also Stories 2, 6, 9.
- [x] (w1) Consolidated order view grouped by item with per-person cost totals
  - Story 10: 'As an Opener, I want to see my round's consolidated order — grouped by menu item with quantities, and totaled per person and overall — so that I can place one accurate order with the restaurant.'
- [x] (w1) Payment explicitly out of scope beyond tracking who owes what
  - Out of Scope: 'Payment collection, expense splitting, or settlement between teammates.' Per-person totals are tracked via Story 10 ('totaled per person'), satisfying tracking who owes what while payment itself is explicitly out of scope.
- [x] (w1) Slack notifications at round open and cutoff; no email flows invented
  - Stories 11 and 12: Slack message posted when a round opens and when cutoff passes; Product Decisions: 'Notifications are Slack-only... No email, and no proactive reminder before the cutoff.'
- [ ] (w1) Mobile-browser support noted
  - No mention of mobile-browser support anywhere in the document; the artifact only describes it as 'A web app' with no reference to mobile browser compatibility.

Inventions flagged (unscored — human call):
- Specific team size figure 'one ~40-person lunch crew' in Product Decisions — not sourced from rubric or interview facts.

## design — PASS (85)

Structural 86%:
- [x] design.md exists
- [x] ≥1 component designed
- [x] every design.json valid
- [ ] design.cell compiles — specs/design/design.cell missing
- [x] openapi.yaml documents valid
- [x] validation-criteria.json valid
- [x] section completed

Rubric judge 83%:
- [x] (w2) A web frontend component and a backend/API component are both designed
  - 'A React SPA (`lunch-webapp`) talks to a Ballerina API (`lunch-api`), which owns a Postgres database (`lunch-db`)...' Both lunch-webapp (web-application) and lunch-api (service) are defined as separate components with their own design.json files.
- [x] (w1) Google-workspace (OAuth/SSO) sign-in appears in the design, not a custom credential store
  - 'A React SPA (`lunch-webapp`) talks to a Ballerina API (`lunch-api`)... and validates sign-in through Thunder (federated to Google Workspace).' lunch-webapp design.json: 'Thunder-issued OIDC client (federated to Google Workspace) used for sign-in via OIDC + PKCE in the browser.' No custom credential store is present; TEAM_MEMBER entity has no password field.
- [x] (w1) The API models the order-round lifecycle including the cutoff lock
  - ROUND entity has status (open/closed) and cutoffAt; 'a round transitions to `closed` the moment `cutoffAt` passes and no longer accepts changes to its `ROUND_ITEM`s.' The API enforces this: POST/PATCH/DELETE on round items return '409: Round is closed — its cutoff has passed'.
- [ ] (w1) Slack notification integration appears in the design
  - Slack is never mentioned anywhere in design.md, lunch-api design.json/openapi.yaml, or lunch-webapp design.json. In fact lunch-api's description explicitly states: 'Does not send Slack notifications or handle payments — those are out of scope for this phase.' This directly contradicts the rubric requirement that Slack notification integration appear in the design.
- [x] (w1) Consolidated/grouped order view with per-person totals is designed
  - 'the opener sees a consolidated, grouped-and-totaled order to place themselves.' Domain model: 'A round's consolidated order is derived... group its ROUND_ITEMs by menuItemId, sum quantities, and total by addedBy and overall.' OpenAPI GET /rounds/{roundId}/consolidated returns ConsolidatedOrder with lines (grouped by menu item), personTotals (per-person), and grandTotal.

Inventions flagged (unscored — human call):
- Full CRUD restaurant/menu management endpoints (create/update restaurants, create/update/delete menu items) — a plausible elaboration to support the ordering flow but goes beyond what was explicitly discussed in the interview, though it's a reasonable prerequisite for the core loop.

## tasks — REVIEW (70)

Structural 100%:
- [x] plan produced issues
- [x] every component covered
- [x] dependsOn refs resolve
- [x] dependsOn acyclic
- [x] turn completed

Rubric judge 40%:
- [ ] (w2) Every designed component has at least one implementable task
  - Only two issues exist (lunch-api, lunch-webapp); there is no task for a Slack/notification component that the must-cover list explicitly calls out (item 2), indicating at least one designed component (the Slack integration) has no implementable task.
- [x] (w1) Frontend tasks list their backend/API providers in build order (dependsOn)
  - issues/2.md front-matter: `dependsOn: ["lunch-api"]` — the frontend task lists its backend/API provider in build order.
- [ ] (w1) A task covers the Slack notification integration
  - Neither issues/1.md nor issues/2.md mentions Slack, notifications, or any messaging integration anywhere in scope, acceptance criteria, or references.
- [x] (w1) Tasks are right-sized (a coherent unit of work each, not one giant task or micro-fragments)
  - issues/1.md and issues/2.md each define a single cohesive component's full scope (e.g., 'Implement lunch-api ... Restaurants ... Menu items ... Lunch rounds ... Round items ... Consolidated order ... Enforce cutoff lock ... Ownership check') with concrete acceptance criteria, representing a coherent, appropriately-sized unit of work rather than a giant catch-all or micro-fragmented tasks.

## Human verdict

- [ ] Agree with the bands above
- [ ] Override: <section> should be <band> because …

Notes:

