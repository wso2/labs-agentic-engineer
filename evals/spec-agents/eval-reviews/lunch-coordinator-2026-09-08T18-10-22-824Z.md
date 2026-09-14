# Eval review — requirements-section / lunch-coordinator

- Run: 2026-09-08T18:10:22.824Z
- Transcript: playground/.projects/spec-agent-evals/req-lunch-coordinator.transcript.md
- Raw trace: playground/.projects/spec-agent-evals/req-lunch-coordinator.trace.json

## requirements — REVIEW (69)

Structural 100%:
- [x] prd.md exists
- [x] substantial (≥800 chars)
- [x] structured (≥3 headings)
- [x] interview happened
- [x] interview finished within cap

Rubric judge 38%:
- [ ] (w2) Google-workspace sign-in captured; no separate registration flow invented
  - The artifact never mentions Google Workspace; instead it invents a proprietary identity provider: 'Sign-in: teammates sign in via SSO through Thunder, the platform identity provider (org default).' This substitutes an invented SSO provider for the specified Google Workspace sign-in.
- [x] (w2) Daily order round lifecycle: open (restaurant + cutoff) -> teammates add own items -> lock at cutoff
  - 'Any team member can open a daily order for a chosen restaurant with a cutoff time; teammates add their own items (with price) before the cutoff. Once the cutoff passes the order automatically locks...' and Product Decisions: 'the app automatically locks the order against new or edited items once that time passes.'
- [ ] (w1) Consolidated order view grouped by item with per-person cost totals
  - The document only describes 'a consolidated view of every item added to an order along with the running total' (Story 7) and a 'per-person cost breakdown' (Story 8), but never states that items are grouped/aggregated by item (e.g., combining duplicate items); it reads as a flat list of items rather than a view grouped by item type as specified in the interview decision.
- [x] (w1) Payment explicitly out of scope beyond tracking who owes what
  - Out of Scope: 'Actually placing or paying for the order with the restaurant... Payment collection or settlement between teammates inside the app.' Combined with Story 8's per-person cost breakdown 'so that I know how much to collect from or owe each teammate,' payment is explicitly out of scope beyond tracking who owes what.
- [ ] (w1) Slack notifications at round open and cutoff; no email flows invented
  - Product Decisions explicitly state: 'Notifications: in-app only for v1... no email, Slack, or push notifications.' This directly contradicts the requirement for Slack notifications at round open and cutoff.
- [ ] (w1) Mobile-browser support noted
  - No mention of mobile-browser support appears anywhere in the document (no section addresses device/browser support).

Inventions flagged (unscored — human call):
- Invented specific SSO provider name 'Thunder' as the platform identity provider, replacing the specified Google Workspace sign-in with an unrelated fictitious identity system.
- An 'order placed' status/archiving mechanic (Story 9: marking an order as 'placed' to archive it out of the open list) not mentioned in rubric or interview decisions.
- Single organization-wide currency assumption (explicitly marked *assumed* but still adds a requirement not sourced from rubric or interview).
- Org-wide single shared list of open orders (no team/department scoping) introduced as an explicit scope decision with an open question, not sourced from rubric or interview.

## Human verdict

- [ ] Agree with the bands above
- [ ] Override: <section> should be <band> because …

Notes:

