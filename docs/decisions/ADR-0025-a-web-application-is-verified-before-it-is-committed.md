# ADR-0025 — A web application is verified before it is committed

**Status:** Accepted · **Related:**
[ADR-0012](ADR-0012-one-debian-runner-image-for-both-task-kinds.md) (one image
serves both task kinds — this adds a browser driver to it),
[ADR-0014](ADR-0014-skill-audience-is-metadata-visible-not-loadable.md)
(skill audience — why a new coding-audience skill needs no runner change),
[ADR-0020](ADR-0020-a-run-species-is-a-workflow.md) (a run species is a
workflow — validation is the other species, and stays one)

## Context

A coding run finishes a `web-application` when the component compiles,
type-checks and bundles. Those three gates say the code is well-formed. They say
nothing about what the screens do, and the defects they miss are the ones a user
meets first: a page that renders the wrong content, a navigation arrow the
wireframe draws and the router never registered, a button wired to nothing, a
form that accepts input and discards it.

Everything downstream of the run assumed those defects would be found later.
`aep-validation` does find them — it drives the **deployed** system against the
validation criteria — but by then the build's pull request has merged. A defect
it reports becomes a `bug` issue, a second cycle, a second pull request and a
second deploy. The feedback loop for "this screen does not work" ran through the
whole platform when the cycle best placed to fix it — the one that had just
written the file — had finished and gone.

The obstacle was never intent. It was that a coding run has no running system:
no cluster, no sibling service, no identity provider. `references/component-
contract.md` says so directly, and it is right to — a build that starts talking
to infrastructure stops being a build.

## Decision

**A `web-application` is opened in a real browser and used before its work is
committed**, against a mock backend on the runner itself. Four parts:

**1. Mock mode is a dev-only mode of the app, authored by the builder.** MSW
intercepts `/api` in the browser (the pattern `apps/console` already uses), and a
small Vite plugin does the two things a request interceptor cannot: serve
`/env-config.js` before the bundle runs, and substitute `src/auth.ts` with a mock
that resolves a user without an IDP. `skills/react-webapp` owns the procedure and
carries the verbatim assets. Production is untouched and provably so — the
skill's Verify step greps `dist/` for `mock/` and `msw` and fails on a hit.

**`mockEnv` mirrors what the platform actually emits**, not what the app's
`src/env.ts` declares. That inversion is what makes mock mode a *detector*
rather than a stage set: an app that reads a sibling's address out of
`window._env_` dies here for exactly the reason it dies in a pod. Both fixtures
this was built against carried that defect, and mock mode reproduced it before a
cluster ever saw them.

**2. The walk is its own dispatch, and whoever walks is whoever fixes.** Its
whole procedure is `skills/mock-verification`; it drives the page through
`agent-browser`. It verifies and repairs in one pass — walk a line, fix the
moment it fails, walk that same line again, and only then start the next — so a
defect is repaired at the cheapest moment it will ever have, by the agent
holding it on screen, and the fix is proven by a click rather than by
compiling. It reports one verdict per story, and the third verdict is not a
hedge: `[~]` marks a story whose truth lives outside the app — a total the real
service computes, a permission the gateway enforces. Marking those honestly is
what keeps the other two verdicts worth reading.

The walk is therefore not a review of the build but the **last step of it**:
`references/component-contract.md` defines a `web-application` as green when it
builds **and** walks, and `react-webapp`'s Development flow numbers the walk as
a step like any other. What separates it from the build is the dispatch, not the
responsibility — the lead sends a fresh subagent once the build reports clean,
naming the component's App Path and the two skills. The builder's advantage over
that subagent is only its own build transcript: both read the same filesystem,
the same `wireframes.dsl` and the same contract, and both edit inside the same
App Path. A browser walk is dozens of turns long, and a builder pays for that
transcript on every one of them.

**3. Its scope is the component, not the issue.** The boundary comes from
`wireframes.dsl` and the product requirements — documents that sit on disk
whatever the issue set says — with the current issue for emphasis. Scoping a
verifier to the issue being worked asks it to confirm the new behaviour and
nothing else, which is the one shape that cannot see a regression; and a
regression is what a cycle produces, through a shared navigation bar, a layout,
a regenerated client, a page edited for an unrelated reason. This was settled by
experiment: two wordings that scoped the walk through the *issues* both failed,
because the lead derives its working set from issues that are **not done** and so
never reads the issue an earlier cycle closed. A scope the lead cannot name is
not a scope.

**4. Coverage is protected by the checklist and a bound, not by a second
agent.** An agent that both walks and fixes has one failure mode: it repairs the
defect on screen two and never opens screens five through nine. Two things hold
it open. The checklist is written **before the browser opens** — from
`wireframes.dsl` and the requirements — so a screen that was never reached is a
line carrying no verdict, where a list assembled as the walk goes simply never
mentions it. And a line gets **three attempts**, after which it is marked `[ ]`
and the walk moves on: a defect that resists three tries belongs in the report,
and the screens behind it are still unopened. Repairing on the spot is what
makes that bound safe — the walk owes nothing to a batch of edits made after it
ended, because every fix was already cleared by walking its own line again.

Because all of this happens **before** the commit, what a reviewer receives is
one pull request carrying the build *and* the fixes.

## The platform does not orchestrate this

There is no new API, no workflow activity, no database table, no status
callback. The loop lives entirely inside the coding session, driven by
`skills/aep`, and reaches the platform only as the resulting commits and pull
request. The mirror already hands an implementation run every coding-audience
skill, so a new skill needs no runner code change.

What the platform did have to change is the envelope the session runs in:

- **The runner image gains `agent-browser`** (`runners/remote-worker/Dockerfile`),
  pointed at the Chromium ADR-0012 already bakes for Playwright via
  `AGENT_BROWSER_EXECUTABLE_PATH`. No third browser is downloaded.
- **The coding job's deadline names three hours** (`codingDeadlineSeconds`),
  with the OpenChoreo ComponentType schema ceiling raised to match. The coding
  path previously sent no deadline at all and inherited the schema default, so
  `EnsureComponentType` had to learn to converge a stale ComponentType rather
  than return early on a 409.
- **Its memory reservation follows a measurement**, not the CPU split beside it:
  a Chromium and a Vite dev server inside the pod peak at 1.22 GiB, above the
  1Gi that used to be requested, and memory is the limit an overrun kills a
  cycle for.

## Alternatives rejected

- **Verify after deployment and let `aep-validation` own it.** That is the status
  quo, and it is still right for what it judges — a deployed system against live
  infrastructure is a different question with a different answer. What it cannot
  do is put the fix in the same pull request as the defect.
- **A read-only verifier subagent plus a separate fixer subagent.** Shipped
  first, then merged. The separation it bought was mostly nominal — a finding
  was re-judged by the walk that followed it either way — while the costs were
  real: two extra dispatches per round, verdicts hand-copied into a prompt, and
  a fixer that had to re-derive the cause a verifier was forbidden to diagnose.
  What survives from it is the property that mattered: one agent walks and
  fixes.
- **The builder walks the app it just built**, as the last step of its own
  dispatch. Also shipped first, then split out. The argument for it was that a
  builder holds the component open and so targets a fix better, and that turned
  out to be an advantage it does not have: the walk subagent opens the same
  files and edits the same App Path. What the builder actually carries into the
  browser is its build transcript, on every turn of a loop that is dozens of
  turns long.
- **Have the platform run the rounds** as workflow activities, for visibility in
  the run history. It would need the runner to report per-round state to an API
  that does not exist, to serve a loop whose whole cost is already inside one
  session. The tool calls reach the progress feed as they are.
- **A hand-rolled Vite middleware** instead of MSW. Written first, then deleted:
  it reimplemented matching and body parsing that MSW already does, in a repo
  that already runs MSW.
- **Generated Playwright specs** instead of an agent driving the page. A spec
  encodes what its author expected; the whole point here is to find what nobody
  expected. Specs are also a second artifact to maintain against screens that
  are still moving.

## Amendment 2026-09-03 — the unit is the flow, not the story

The first skill text keyed the checklist and the report by requirement story
and asked for nine checks per screen. The rewrite keys both by the DSL's `flow`
blocks (one role, its screens in walking order) and bounds each screen to three
questions — reached by clicking, every control acts, a change leaves the page as
the declared request — plus roles, session and two probes once each. Stories
stay the product oracle that `aep-validation` judges; the walk's verdicts name
screens, which is what a fix needs. The walk is framed as a smoke walk:
breadth over depth, missing features and obvious breakage, not data, layout or
wording. The lead's part became a literal dispatch prompt, and the walker posts
the walk's own status lines.

That narrows decisions 3 and 4: the boundary is still the component rather than
the issue, but the DSL alone draws it, and `specs/requirements/` is no longer a
walk input or a checklist source. Everything else above stands.

## Consequences

- A `web-application` costs two dispatches where a service costs one, and the
  walk is the longer of them: standing the app up, opening every screen it
  draws, and repairing as it goes. Across a milestone's whole issue set that is
  the reason for the three-hour deadline. Two is still well under the four to
  seven the verifier-plus-fixer shape cost.
- Nothing outside the walker's own skill describes the loop. The lead's part is
  to dispatch the walk once the build reports clean — naming the component's App
  Path, `mock-verification` and `agent-browser`, and nothing about how to walk —
  and to carry any still-open `[ ]` line into the run's record.
- The mock harness is committed with the app. It is dev-only, absent from
  `dist/`, and it doubles as the fixture a human can run locally with
  `npm run dev:mock`.
- A `[~]` verdict is a deliberate hand-off: stories whose truth lives outside the
  app remain `aep-validation`'s to judge, and the verdict names them so nobody
  assumes they were covered here.
- A defect three attempts could not clear does not block the cycle. It is
  carried into the run's record naming the screen and what happens on it.
