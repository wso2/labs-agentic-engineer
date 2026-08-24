---
name: console-feature
description: Drive a console (frontend) feature through its issue-driven cycle, grilled before any code. Use when the user wants a console feature built or changed, or names an existing console feature issue; bug fixes and polish are exempt.
argument-hint: <feature idea in plain words, or an existing issue number>
---

# Console feature cycle

The **entry point for frontend feature work**: a feature goes through this
skill — grilled first, then built. It takes either end — a raw **idea** (no
issue yet), or an **issue number** for one already open.

`apps/console/design/development-flow.md` is the **spec**: it defines every
stage, every rule, and the issue template. **Read it now**, before anything
else. This skill carries only what the spec doesn't — which mode to route
into, where each pause sits, and the commands to run. Console work requires
`gh` auth.

Stopping at any pause is a normal exit, not a failure. Say that the feature
resumes with `/console-feature <issue-number>`; a stop mid-interview restarts
it, since nothing is recorded until the issue or the comment exists.

## On silence

Three kinds of pause, each named for what it does when the user doesn't
answer:

- **Re-ask** — every interview question, and the issue-draft confirmation.
  User input is the whole point: ask again and wait. Answering a grilling
  question on the user's behalf voids the interview.
- **Auto-proceed** — the recording pauses (decisions comment, ADR
  graduation, handshake issue). They transcribe decisions the user already
  made: offer, and on silence proceed.
- **Halt** — Build and Ship. On silence, or anything short of a clear yes,
  end the session cleanly and print the resume command.

## Route on the argument

- A bare number (`42`, `#42`) or an issue URL → **issue mode**.
- Anything else → **idea mode**, the text being the feature idea.
- Nothing → ask which of the two they have, then the matching mode.

## The interview

First read `apps/console/PRD.md` and the ADRs in
`apps/console/design/decisions/` — the interview is only as sharp as the
product picture behind it.

Run the `grill-me` skill on the subject — the raw idea in idea mode, the
issue as written in issue mode. If it isn't invocable in this session, run
the interview yourself in its spirit: relentless rounds of pointed questions
(AskUserQuestion) attacking the walkthrough's weak points, ending only when
**every unknown is decided**. Every question is a **re-ask** pause.

Keep the running outcome — decided / why / rejected — as you go. Where it
gets written down is the one thing the two modes don't share, and spec step 1
says which goes where.

## Idea mode (no issue yet)

1. **Check for overlap.** Where the idea touches existing work, `gh issue
   list --repo wso2/labs-agentic-engineer --label console --label feature`
   (`--state closed` for history).
2. **Grill the idea.** Nothing is written down until step 4.
3. **Draft the issue** (**re-ask**). Fill the spec's issue template from the
   interview outcome. Show the user the full draft — title and body — and
   get an explicit yes.
4. **Create it.** `gh issue create --repo wso2/labs-agentic-engineer
   --label console --label feature`, with the agreed title and body.
5. Continue to the stage walk at **ADR graduation**.

## Issue mode (the issue exists)

Covers both an issue someone filed by hand and a feature this skill started
earlier; step 3 tells them apart.

1. **Fetch it**, comments included: `gh issue view <n> --repo
   wso2/labs-agentic-engineer --comments`. **Closed** → the feature shipped
   or was abandoned, and a follow-up needs its own issue: say so and stop.
   Anything that isn't a console feature (a bug, a chore, another component)
   is exempt from this cycle: say so and stop.
2. **Find its PR**: `gh pr list --repo wso2/labs-agentic-engineer --search
   "<n>" --state all`.
   - **Merged** → frozen; further requests become a new issue referencing
     this one. Say so and stop, or offer to draft it.
   - **Open** → feedback lives on the PR; enter the Build stage's feedback
     loop.
3. **Is it grilled?** Yes if the body has a filled Decisions section, or a
   decisions comment exists. If not, grill it now, before anything else:
   - Read the discussion already on the issue — it is part of the subject,
     and points settled there get confirmed rather than re-litigated.
   - Run the interview on the issue as written.
   - **Post the decisions comment** (**auto-proceed**), showing it first.
   - Edit the body for whatever the interview changed, per spec step 1, and
     add the `console` and `feature` labels if the filer didn't.
4. **Detect the furthest completed stage** — ADR in
   `apps/console/design/decisions/`? contract change, mocks, UI, PRD entry
   on the feature branch? handshake issue open, and have its backend changes
   landed on that branch? — report what you found, then continue the stage
   walk from the next incomplete stage.

## Stage walk

Each stage **runs the spec's step of the same number** (flow steps 4–7) —
read what to do there. What follows is only the pause, the completion
criterion, and the commands. Keep the issue body current with
`gh issue edit` while it is open.

1. **ADR graduation** (**auto-proceed**). Test **every** decision the issue
   carries against the spec's three-part rule — the ones that fail it stay
   with the issue.
2. **Build the frontend on mocks** (**halt** to enter). Use the `oxygen-ui`
   skill for all UI work. Save screenshots to a local folder, print the
   paths, and ask the user to drag-and-drop them into the PR. Review
   comments re-enter this stage: implement,
   verify, push, checking `gh pr view --json state` before *every* push so a
   PR that merged meanwhile is never pushed to. Ends on the user confirming
   the feature in mock mode (**halt**) — that confirmation is what unlocks
   the handshake.
3. **BE handshake** (**auto-proceed**) — only if the issue's Contract
   changes section is non-empty. Open the `aep-api` issue per spec step 6,
   then stop and tell the user the feature waits on it: Ship can't start
   until the backend changes are on the branch.
4. **Ship** (**halt** to enter; entry condition: backend changes are on the
   feature branch, or the feature changed no contract). The user's
   confirmation from the local-setup test is what unlocks the merge. Before
   merging, check that the PR carries the PRD entry and closes the feature
   issue — plus the handshake issue, if stage 3 opened one. The merge is the
   last act: anything found afterward is a new issue.
