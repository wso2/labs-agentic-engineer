---
name: mock-verification
description: "Smoke-walk a `web-application` in a real browser once it builds clean — stand it up in mock mode, walk every flow its wireframes draw, fix each failure where you find it, post progress item by item. Required for every change to a webapp component. Judging a DEPLOYED system is `aep-validation`'s job instead."
metadata:
  aep:
    kind: platform
    audience: [coding]
---

# Mock verification

A clean build says the code is well formed and nothing about what the screens
do: a route the wireframe draws and the router never registered, a button wired
to nothing, a form that flips a row and sends no request. So you open the app
and use it. Mock mode stands it up on this machine with no cluster, no sibling
service and no IDP. You verify and repair in one pass.

**This is a smoke walk: breadth over depth.** You are looking for a screen that
is missing, an arrow that goes nowhere, a control that does nothing, a request
that never leaves the page. Data, layout and wording are not your questions.
Your scope is the whole component: a cycle's regressions land in shared chrome
and on pages nobody meant to touch, so every flow is walked, whichever issue
built it.

## What you verify

### The map

`specs/design/components/<component>/wireframes.dsl`, under the project root —
your working directory, one level above the App Path you edit in. A `flow`
block is the unit: one role and the screens that role walks, entry screen
first. Walk every flow under its role (`?role=<name>`) by clicking from its
entry screen; a screen in no flow is reached at its route. **The DSL is your
only map**: you open a source file to repair, never to learn a route. A
component with no `wireframes.dsl` gives you its registered routes instead, one
flow per role.

### Per screen

Three questions, each one action and one snapshot with the first plausible
input. A screen is answered in a handful of actions; past that you are past the
smoke.

| | Question | Evidence |
|---|---|---|
| **Reach** | Did the arrow that names this screen bring you here, and does every `->` it draws land where it says? | the target's snapshot |
| **Act** | Does every drawn control change something visible when used? A create is in the next list, a filter narrows, a toggle flips a row. | the snapshot after the action |
| **Request** | Did a change leave the page as the request the contract declares, with the status it declares? A row that flips and sends nothing is the one defect a build cannot see. | `agent-browser network requests` |

### Once per app

- **Roles** (`mock/roles.ts` exists): each flow's entry screen under its own
  role (`?role=<name>`), and once under a role the DSL gives no flow there.
  Both directions are defects. Then three more, which the scope model makes
  walkable and which no build can see:
  - **Every role, and the no-role visitor.** Walk `?role=<name>` for each role
    in `mock/roles.ts`, then `?role=` (empty — signed in, holding nothing).
    The empty one must give **`NoAccess` replacing the shell**, naming the
    groups to ask to be added to. A shell with an empty nav around it is a
    defect; so is being let into a screen.
  - **Forbidden, by hand.** Open one screen a role must NOT reach, by URL,
    under that role. Expect that screen's **`Forbidden` state INSIDE the app
    shell** — the rail the role can use still there — naming the role that
    would unlock it. Never a redirect to sign-in, never a blank page, never the
    screen itself.
  - **The mock must have refused something.** None of this means anything
    unless `mock/handlers.ts` enforces the handle the contract declares and
    answers 403 + `insufficient_scope` on it. A handler that answers 200
    regardless, or that accepts a sibling handle (`claims:read-all` where the
    contract says `claims:read`), makes the whole walk green and hides the
    defect it exists to find. Check one refusal in
    `agent-browser network requests` before you trust the rest.

  A screen a role is *meant* to reach but cannot, because the design does not
  grant it the handle the operation declares, is **open**, naming the role, the
  screen and the handle. It is a design defect, not a code one: do not widen a
  mock handler or a route guard to make it pass.
- **Session** (an auth dependency): `?auth=out` on one entry screen runs the
  app's own guard and `signIn()` brings you back; then **Sign out** where the
  navbar draws it leaves the screen through `signOut()`. The mock signs the
  next load in again, so the session being gone is outside; the click leaving
  the page is not.
- **Probes**: submit one form empty; open one detail route with an id that does
  not exist. The wireframe draws the happy path; these are the two states it
  implies.
- **Console** (`agent-browser console`, read whole, once, before you stop): a
  page that renders and throws is broken for whoever touches it next, and the
  error text is the finding.

### Outcomes

Every item ends in exactly one:

- **done** — what you did and what the page did back.
- **fixed** — what was wrong; what you changed; what it does now, re-walked.
- **open** — what happens, after three attempts.
- **outside** — the truth lives outside the app: a computed total, a generated
  checklist, what the real IdP grants a real account. The mock answers to
  `openapi.yaml`, so it proves the request went out, never that the number is
  right; `aep-validation` judges that against the deployed system. A **403 is
  not outside** — the mock answers it from the contract's own handle, so
  `Forbidden` is something you walk and see.

An unreachable screen is **open**, naming the navigation that failed, never
**done** read off the source.

## Progress

Your prompt says where progress goes. Post there, in these three shapes and no
other: the plan once, one line per item as it settles, the close once.

```text
Mock verification: <component> — <N> items
1. <Screen> (<role>): <its controls>; -> <the screens its arrows name>
2. <Screen>: ...
<N-2>. Session
<N-1>. Probes
<N>. Console
```

```text
<n>/<N> <Screen>: done — <what you did and what the page did back>
<n>/<N> <Screen>: fixed — <what was wrong>; <what you changed>; re-walked, <what it does now>
<n>/<N> <Screen>: open — <what happens>, 3 attempts
<n>/<N> <Screen>: outside — <the truth that lives outside the app>
```

```text
Mock verification done: <component> — <N> items · <d> done, <f> fixed, <o> open (<n> <Screen>, ...), <t> outside
```

The newest line is where the walk is, and that is what the person watching
reads. An item with no line is a screen you never reached, and it stays visible
as one.

## 1 · Stand it up

From the App Path:

```bash
bash "$AEP_SKILLS_DIR/mock-verification/scripts/walk.sh" up
```

It starts `npm run dev:mock` on a free port, reaps a stale server from an
earlier attempt first, and prints `READY <url>`. The url is what you open;
`?role=` and `?auth=out` go on it. If it prints the log instead, that is your
first finding: the harness is `react-webapp`'s `references/mock-mode.md`; fix
it and run `up` again.

**Done when:** `READY`.

## 2 · Plan

Before the browser opens, post the plan from the map alone: one numbered item
per screen in each flow, in walking order under its role, then screens in no
flow, then Roles (with `mock/roles.ts`), Session (with an auth dependency),
Probes, and Console. Posting it first puts your coverage in front of the
person watching while there is still time to say a screen is missing.

**Done when:** every screen and flow in the map has a number, and the plan is
posted.

## 3 · Walk

Load `agent-browser` and follow it: open, snapshot, act on what the snapshot
shows.

**An item ends green, and posted.** Walk it; if it fails, fix it now, walk that
same item again, and see it pass; then post its line, and only then open the
next. A line posted at the end for an item settled an hour ago is a history,
not progress. Every failure is yours, whichever issue put it there.
**Repair the app, never the plan.**

**Three attempts on an item, then post it open and walk on.** The screens
behind it are still unopened. A fix is the wiring a screen is missing; a defect
that wants a redesign is open with its cause named.

**Click between screens.** Mock state lives in the page, so `open`, reload and
back restore the seed data: a record you created a moment ago is gone, and that
is the mock telling the truth, not the app. Spend full loads at the start of a
block, where there is no state to lose — a role switch, `?auth=out`, an unknown
id. The once-per-app items are walked off the plan like any other.

`restart` after a change to `vite.config.ts`, `mock/` or a dependency;
everything else hot-reloads.

**Done when:** every number has its line, each posted before the next item was
opened.

## 4 · Close

Post the closing line, then:

```bash
bash "$AEP_SKILLS_DIR/mock-verification/scripts/walk.sh" down
```

`down` stops the server, closes the browser and confirms the port let go. Hand
back to whoever dispatched you the closing line and the numbered list with each
item's outcome: the closing line is what the pull request quotes, and the open
items are what it carries.

**Done when:** the close is posted, `down` printed `STOPPED`, and the list is
in your reply.

## Never

- **Make the mock agree with the app.** `mock/handlers.ts` answers to
  `openapi.yaml`, the same document `src/generated/` came from, and to nothing
  else. A handler bent until a screen passes hides the defect from the deployed
  system too. A `501` is a handler you never wrote: write it against the
  contract.
- **Widen a scope to make a walk pass.** Not in a handler, not in a route
  guard, not in `mock/roles.ts`. Scope comparison is a whole-string match at the
  gateway and in the service, so a mock that accepts a sibling handle passes a
  walk the deployed system answers 403 to. A role that cannot reach its own
  screen is a **design defect**: post it open, naming the role, the screen and
  the handle.
- **Run `git`, commit, or open a pull request.** The record belongs to the agent
  that dispatched you. Progress, where your prompt says it goes, is the only
  writing you do outside the App Path.
