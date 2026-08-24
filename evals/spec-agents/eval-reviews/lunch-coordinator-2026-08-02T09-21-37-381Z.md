# Eval review — chain / lunch-coordinator

- Run: 2026-08-02T09:21:37.381Z
- Transcript: playground/.projects/spec-agent-evals/chain-lunch-coordinator.transcript.md
- Raw trace: playground/.projects/spec-agent-evals/chain-lunch-coordinator.trace.json

## requirements — PASS (81)

Structural 100%:
- [x] requirements.md exists
- [x] substantial (≥800 chars)
- [x] structured (≥3 headings)
- [x] interview happened
- [x] interview finished within cap

Rubric judge 63%:
- [x] (w2) Google-workspace sign-in captured; no separate registration flow invented
  - Users authenticate with their existing company Google Workspace account (Google sign-in). There is no separate registration or admin-managed membership list.
- [x] (w2) Daily order round lifecycle: open (restaurant + cutoff) -> teammates add own items -> lock at cutoff
  - Any signed-in user can open a new lunch order... requires a restaurant name and cutoff time... Any signed-in user can add one or more items... before cutoff... Once the cutoff time passes, the order automatically locks.
- [ ] (w1) Consolidated order view grouped by item with per-person cost totals
  - Artifact groups items by person, not by item: 'All items grouped by the person who added them. A per-person subtotal and an overall total...' The rubric requires items grouped by item with per-person totals, but the artifact only groups by person, never by item.
- [x] (w1) Payment explicitly out of scope beyond tracking who owes what
  - Placing the order with the restaurant and handling payment/collection among teammates are manual, out-of-app steps. / A per-person subtotal and an overall total... (tracking who owes what via subtotals).
- [ ] (w1) Slack notifications at round open and cutoff; no email flows invented
  - Notifications/reminders (not requested; may be considered later) is listed under Out of Scope, directly contradicting the requirement for Slack notifications at round open and cutoff.
- [ ] (w1) Mobile-browser support noted
  - No mention of mobile-browser support anywhere in the document.

Inventions flagged (unscored — human call):
- Assumption of a ~40-person single shared company pool (no sub-teams) — not derived from rubric or interview
- Only one active order may be open at a time for the whole company pool, with rejection logic for a second attempt
- A persistent history list of past locked/closed orders
- Explicit edit/remove-own-item capability before cutoff
- Detailed non-functional requirements (scale, reliability of cutoff enforcement) beyond what was discussed

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
  - Components: lunch-webapp (web-application) — SPA; lunch-api (service) — backend API owning persistence. 'The system is one React SPA talking to one Go API that owns all persistence.'
- [x] (w1) Google-workspace (OAuth/SSO) sign-in appears in the design, not a custom credential store
  - 'Sign-in — redirect unauthenticated visitors to Google Workspace sign-in (via the platform's Thunder-mediated OIDC flow)'; dependency 'company-auth' resourceType 'thunder-app' scopes 'openid profile email'; no custom credential store is defined anywhere.
- [x] (w1) The API models the order-round lifecycle including the cutoff lock
  - Order.status is 'derived: open while now < cutoffAt, else locked'; API endpoints /orders (open), /orders/current, cutoff enforcement described: 'the API treats an order as locked once the current time passes its cutoff... computed on read, not dependent on a background job or a client close call'; addOrderItem/updateOrderItem/deleteOrderItem all return 409/403 once locked.
- [ ] (w1) Slack notification integration appears in the design
  - No mention of Slack anywhere in design.md, design.json, or openapi.yaml for either component.
- [x] (w1) Consolidated/grouped order view with per-person totals is designed
  - 'Consolidated summary — read-only grouped-by-person view with per-person subtotals and an overall total'; OpenAPI schema OrderSummary with byPerson (PersonSummary: userId, userDisplayName, items, subtotal) and grandTotal.

## tasks — REVIEW (70)

Structural 100%:
- [x] plan produced issues
- [x] every component covered
- [x] dependsOn refs resolve
- [x] dependsOn acyclic
- [x] turn completed

Rubric judge 40%:
- [ ] (w2) Every designed component has at least one implementable task
  - Only two components/tasks exist in the plan (lunch-api, lunch-webapp). No task exists for a Slack notification component (see below), so if the design includes a Slack integration component, it has no implementable task.
- [x] (w1) Frontend tasks list their backend/API providers in build order (dependsOn)
  - issues/2.md front-matter: dependsOn: ["lunch-api"] for the lunch-webapp task, and scope text 'consuming the lunch-api service' / 'call lunch-api per its openapi.yaml contract'.
- [ ] (w1) A task covers the Slack notification integration
  - No mention of Slack anywhere in either issues/1.md or issues/2.md — there is no task, scope line, or acceptance criterion referencing Slack notifications.
- [x] (w1) Tasks are right-sized (a coherent unit of work each, not one giant task or micro-fragments)
  - The plan contains exactly two tasks, each scoped to a full component (lunch-api backend service and lunch-webapp SPA) with clearly bounded scope sections and acceptance criteria mapped to specific REQ/AC ids — coherent units of work rather than a single monolithic task or many micro-fragments.

Inventions flagged (unscored — human call):
- 'Validate X-User-Groups presence per platform convention even though this app has a single implicit role' — an extra header-validation requirement not obviously required by the rubric or product idea, though plausibly tied to an internal platform convention referenced by the plan.

## Human verdict

- [ ] Agree with the bands above
- [ ] Override: <section> should be <band> because …

Notes:

