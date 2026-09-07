# Eval review — requirements-section / lunch-coordinator

- Run: 2026-09-01T07:53:15.585Z
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
  - Artifact states 'Team members sign in via SSO through Thunder, the platform IDP (org default).' This invents a specific SSO provider ('Thunder') rather than capturing Google-workspace sign-in as required, and does not mention Google Workspace at all.
- [x] (w2) Daily order round lifecycle: open (restaurant + cutoff) -> teammates add own items -> lock at cutoff
  - 'any team member can open a daily lunch order for a chosen restaurant with a cutoff time. Teammates add their own items (with price) before the cutoff. Once the cutoff passes, the opener sees one consolidated summary...' and 'an order to stop accepting changes automatically once its cutoff time passes.'
- [ ] (w1) Consolidated order view grouped by item with per-person cost totals
  - Artifact describes the consolidated summary as 'every item, grouped by person, with per-person and total cost' — this groups by person, not by item as required by the rubric, so the specific grouping structure is not correctly captured.
- [x] (w1) Payment explicitly out of scope beyond tracking who owes what
  - 'Payment processing, splitting bills, or any money actually changing hands in-app — the app only totals what each person owes.'
- [ ] (w1) Slack notifications at round open and cutoff; no email flows invented
  - No Slack notifications are mentioned anywhere; instead the artifact explicitly states 'There is no reminder or notification before a cutoff in v1 — team members are responsible for checking the app' and lists 'Reminders or notifications ahead of a cutoff' as Out of Scope, directly contradicting the required Slack notification feature.
- [ ] (w1) Mobile-browser support noted
  - No mention of mobile-browser support anywhere in the document; the app is only described as 'A web app' with no note about mobile browser compatibility.

Inventions flagged (unscored — human call):
- SSO via a specific named IDP 'Thunder' (org default) — not Google-workspace sign-in as implied by the rubric, and not mentioned in the interview.
- Viewing past daily orders / order history (User Story 7) — not derivable from rubric or interview decisions.
- Consolidated view grouped by person rather than by item — a structural deviation not supported by rubric or interview.

## Human verdict

- [ ] Agree with the bands above
- [ ] Override: <section> should be <band> because …

Notes:

