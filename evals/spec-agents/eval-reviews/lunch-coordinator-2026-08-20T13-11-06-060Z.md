# Eval review — requirements-section / lunch-coordinator

- Run: 2026-08-20T13:11:06.060Z
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
  - The PRD states 'Sign-in is via SSO through Thunder, the platform IDP.' This invents a different identity provider (Thunder) rather than stating Google-workspace sign-in, and does not mention Google Workspace at all.
- [x] (w2) Daily order round lifecycle: open (restaurant + cutoff) -> teammates add own items -> lock at cutoff
  - 'Any team member can open a new daily order (choosing the restaurant and cutoff time), teammates add their own items... At cutoff, the round locks automatically' and 'Cutoff is enforced by the system: at the set time, the order locks automatically and no further add/edit/remove is possible on it.'
- [ ] (w1) Consolidated order view grouped by item with per-person cost totals
  - The document only describes 'one consolidated list of everything the team wants' with consolidation 'simply groups the raw text people entered' — there is no mention of per-person cost totals anywhere; in fact cost-splitting is explicitly excluded ('Payment collection or cost-splitting among teammates').
- [ ] (w1) Payment explicitly out of scope beyond tracking who owes what
  - Out of Scope says 'Payment collection or cost-splitting among teammates' with no mention of tracking who owes what; this excludes cost tracking entirely rather than scoping payment out while retaining a 'who owes what' tracking feature.
- [x] (w1) Slack notifications at round open and cutoff; no email flows invented
  - 'A locked order triggers a Slack message to the team's channel announcing that ordering has closed' and 'A new order opening also posts a Slack message to the team channel' — no email flows are mentioned anywhere.
- [ ] (w1) Mobile-browser support noted
  - No mention of mobile-browser support anywhere in the document; it only says 'A web app for coordinating team lunch orders.' with no reference to mobile browsers.

Inventions flagged (unscored — human call):
- Sign-in via SSO through 'Thunder, the platform IDP' — a specific named identity provider not mentioned in the rubric, interview, or product idea, and contradicting the expected Google-workspace sign-in.
- Team size specified as 'a fixed group of ~40 members' — a specific headcount not derived from rubric, interview, or product idea.
- 'Only one order can be open at a time' concurrency restriction rule — an added constraint beyond the interview's simple 'one round per day' expectation.

## Human verdict

- [ ] Agree with the bands above
- [ ] Override: <section> should be <band> because …

Notes: Verification run for #578 (uncapped interview). The interview SHAPE is what
this run was checking, and it holds: turn 1 asks the spine, turn 2 writes the PRD
**and asks the next round in the same turn**, turn 3 refines the document in place
with editFile. Slack notifications — assumed away and failed in the pre-fix runs —
are captured here by that second round.

The two remaining rubric failures are not interview-shape defects: the Google-Workspace
miss is the org-defaults blindness (the Tech-stack default settles sign-in, so the agent
correctly never asks, and the judge reads the org default as an invention), and
per-person totals / mobile support were never volunteered by the sim, which only offers
facts adjacent to what it is asked. Scores across four runs of this change: 75 / 63 / 63 / 69.

