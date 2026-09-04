---
name: mock-verification
description: Smoke-walk a `web-application` in a real browser once it builds clean — stand it up in mock mode, walk every flow its wireframes draw, fix each failure where you find it, keep one checklist current on the issue. Required for every change to a webapp component. Judging a DEPLOYED system is `aep-validation`'s job instead.
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

The mechanics — the dev server, the port, the browser, the issue comment — are
one script, so you write none of them:

```bash
bash "$AEP_SKILLS_DIR/mock-verification/scripts/walk.sh" <up | restart | post [issue#] | down>
```

## What you verify

### The map

`specs/design/components/<component>/wireframes.dsl`, under the project root —
your working directory, one level above the App Path you edit in. A `flow`
block is the unit: one role and the screens that role walks, entry screen
first. Walk every flow under its role (`?role=<name>`) by clicking from its
entry screen; a screen in no flow gets a block of its own, reached at its route.
**The DSL is your only map**: you open a source file to repair, never to learn a
route. A component with no `wireframes.dsl` gives you its registered routes
instead, one flow per role.

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
  role, and once under a role the DSL gives no flow there. Both directions are
  defects.
- **Session** (an auth dependency): `?auth=out` on one entry screen runs the
  app's own guard and `signIn()` brings you back; then **Sign out** where the
  navbar draws it leaves the screen through `signOut()`. The mock signs the
  next load in again, so the session being gone is `[~]`; the click leaving the
  page is not.
- **Probes**: submit one form empty; open one detail route with an id that does
  not exist. The wireframe draws the happy path; these are the two states it
  implies.
- **Console** (`agent-browser console`, read whole, once, before you stop): a
  page that renders and throws is broken for whoever touches it next, and the
  error text is the finding.

### Marks

One line per screen and per once-per-app item. Unmarked, it is a line to walk.
Walked, it carries exactly one of:

```text
- [x] <Screen>: <one clause: what you did and what the page did back>
- [x] <Screen>: FIXED <what was wrong>; <what you changed>. Re-walked: <what it does now>.
- [ ] <Screen>: <what happens>. Tried <what>, 3 attempts. <what still happens>.
- [~] <Screen>: <the truth that lives outside the app>
```

`[~]` is what the mock or the gateway supplies — a computed total, a generated
checklist, a 403. The mock answers to `openapi.yaml`, so it proves the request
went out, never that the number is right; `aep-validation` judges that against
the deployed system. An unreachable screen is a `[ ]` naming the navigation that
failed, never an `[x]` read off the source.

## 1 · Stand it up

From the App Path:

```bash
bash "$AEP_SKILLS_DIR/mock-verification/scripts/walk.sh" up
```

It starts `npm run dev:mock` on a free port, reaps a stale server from an
earlier attempt first, and prints `READY <url> · checklist <file>`. The url is
what you open (`?role=` and `?auth=out` go on it); the file is where the
checklist goes. If it prints the log instead, that is your first finding: the
harness is `react-webapp`'s `references/mock-mode.md`; fix it and run `up`
again.

**Done when:** `READY`.

## 2 · Checklist

Before the browser opens, write the checklist file from the map alone:

```text
Mock verification: <component>

flow "<name>" (<role>)
- <Screen>: <its controls>; -> <the screens its arrows name>
- <Screen>: ...
screens in no flow
- <Screen>: <its controls>; -> <targets>
once per app
- Roles
- Session
- Probes
- Console
```

Then publish it:

```bash
bash "$AEP_SKILLS_DIR/mock-verification/scripts/walk.sh" post <issue#>
```

`post` rewrites the first line with the counts and posts the file as one
comment on the issue your prompt names, editing that same comment on every
later call. A prompt that names no issue: leave the number off, and the file is
the report.

This is your walk order and, marked up, your report: a screen you never reach
is a line with no mark, where a list assembled as you go simply never mentions
it. Posting it before the browser opens puts your coverage in front of the
person watching while there is still time to say a screen is missing.

**Done when:** every screen and flow in the map has a line, so does each
once-per-app item, and `post` has run.

## 3 · Walk

Load `agent-browser` and follow it: open, snapshot, act on what the snapshot
shows.

**A line ends green.** Walk it; if it fails, fix it now, walk that same line
again, and see it pass before the next. Every failure is yours, whichever issue
put it there. **Repair the app, never the checklist.**

**Three attempts on a line, then mark it `[ ]` and walk on.** The screens
behind it are still unopened. A fix is the wiring a screen is missing; a defect
that wants a redesign is a `[ ]` with its cause named.

**Click between screens.** Mock state lives in the page, so `open`, reload and
back restore the seed data: a record you created a moment ago is gone, and that
is the mock telling the truth, not the app. Spend full loads at the start of a
block, where there is no state to lose — a role switch, `?auth=out`, an unknown
id. The once-per-app lines are walked off the checklist like any other.

**Mark the line as it settles, then `post`.** The comment's first line is what
the person watching reads, and the counts are the progress. `restart` after a
change to `vite.config.ts`, `mock/` or a dependency; everything else
hot-reloads.

**Done when:** every line carries a mark.

## 4 · Report

```bash
bash "$AEP_SKILLS_DIR/mock-verification/scripts/walk.sh" post <issue#>
bash "$AEP_SKILLS_DIR/mock-verification/scripts/walk.sh" down
```

`down` stops the server, closes the browser and confirms the port let go. Hand
the checklist back to whoever dispatched you, whole: its first line is what the
pull request quotes, and its `[ ]` lines are what the pull request carries.

**Done when:** the last `post` is up, `down` printed `STOPPED`, and the
checklist is in your reply.

## Never

- **Make the mock agree with the app.** `mock/handlers.ts` answers to
  `openapi.yaml`, the same document `src/generated/` came from, and to nothing
  else. A handler bent until a screen passes hides the defect from the deployed
  system too. A `501` is a handler you never wrote: write it against the
  contract.
- **Run `git`, commit, or open a pull request.** The record belongs to the agent
  that dispatched you. The checklist comment on your own issue is the only
  writing you do outside the App Path.
