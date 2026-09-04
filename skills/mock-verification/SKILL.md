---
name: mock-verification
description: Load once a `web-application` builds clean, to finish it — stand the app up in mock mode with no cluster behind it, smoke-walk every flow in a real browser, and fix each failure the moment you find it. Judging a DEPLOYED system against live infrastructure is `aep-validation`'s job instead.
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
service and no IDP (the harness is `react-webapp`'s `references/mock-mode.md`).
You verify and repair in one pass, holding the component open.

**This is a smoke walk: fast, breadth over depth.** You are looking for a screen
that is missing, an arrow that goes nowhere, a control that does nothing, a
request that never leaves the page. You are not judging data, layout or
wording. Your scope is the whole component, every flow a user can walk and
whichever issue built it: a cycle's regressions land in shared chrome and in
pages nobody meant to touch, and breadth is what finds them.

## What you verify

The unit is a **flow**: a `flow` block in
`specs/design/components/<component>/wireframes.dsl`, one role and the screens
that role walks, entry screen first. Walk every flow the file declares, under its
role (`?role=<name>`), by clicking from its entry screen. A screen in no flow
gets a block of its own. **The DSL is your only map**: you open a source file to
repair, never to learn a route. A component with no `wireframes.dsl` gives you
its routes instead, one flow per role the app registers.

Three questions per screen. Each is one action and one snapshot, with the first
plausible input; a screen is answered in a handful of actions, and past that you
are past the smoke.

| | Question | Evidence |
|---|---|---|
| **Reach** | Did the arrow that names this screen bring you here, and does every `->` it draws land where it says? | the snapshot of the target |
| **Act** | Does every drawn control change something visible when used? A create is in the next list, a filter narrows, a toggle flips a row. | the snapshot after the action |
| **Request** | Did a change leave the page as the request the contract declares, with the status it declares? A row that flips and sends nothing is the one defect a build cannot see. | `agent-browser network requests` |

Three guards and two probes, once each rather than per screen:

- **Roles** (`mock/roles.ts` exists): each flow's entry screen under its own
  role, and once under a role the DSL gives no flow there. Both directions are
  defects.
- **Session** (an auth dependency): `?auth=out` on one entry screen runs the
  app's own guard, and `signIn()` brings you back. Then click **Sign out** where
  the navbar draws it: the app leaves the screen through `signOut()`. The mock
  signs the next load in again, so whether the session is gone is `[~]`; the
  click leaving the page is not.
- **Console** (the CLI's own console verb, read before you stop): a page that
  renders and throws is broken for whoever touches it next, and the error text
  is the finding.
- **Probes**: submit one form empty; open one detail route with an id that does
  not exist. The wireframe draws the happy path; these are the two states it
  implies.

Outside the walk, marked `[~]`: what the mock or the gateway supplies — a
computed total, a generated checklist, a 403. The mock answers to
`openapi.yaml`, so it can prove the request went out, never that the number is
right. `aep-validation` judges that against the deployed system, and anything
finer than obvious with it.

## 1 · Checklist

Before the browser opens, write `/tmp/walk-<component>.md` from your map alone
(the DSL, or the app's routes when there is none): one block per flow, one block
`screens in no flow` when the map has any, one line per screen naming its
controls and arrows, then one line each for Roles, Session, Probes and Console.
It is your walk order, and filled in it is your report. A screen you never reach
is then a line with no mark, where a list assembled as you go simply never
mentions it.

**Done when:** every screen and every flow in the map has a line, and so does
each of Roles, Session, Probes and Console.

## 2 · Stand it up

From the App Path:

```bash
setsid --fork bash -c 'echo $$ > /tmp/mock-<component>.pgid; exec npm run dev:mock -- --port 5173 --strictPort' > /tmp/mock-<component>.log 2>&1 &
for i in $(seq 1 30); do curl -sf http://localhost:5173/ >/dev/null && break; sleep 2; done
curl -sf http://localhost:5173/ >/dev/null || tail -40 /tmp/mock-<component>.log
```

Every part is load-bearing. `npm run` is three processes, so the process
**group** is the only handle that reaps them all; `setsid --fork` makes one and
the leader writes its own id (`$!` would name `setsid`, which has already
exited). `--strictPort` keeps a browser at 5173 off a previous run's server: a
refusal to bind means one is still up, so end it with
`kill -- -"$(cat /tmp/mock-<component>.pgid)"` and start again. This machine has
no `ps`, `pgrep`, `pkill` or `lsof`.

**Done when:** the URL answers and the log holds no error. If it will not start,
that is the whole finding: fix it and start again.

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
block, where there is no state to lose: a role switch, `?auth=out`, an unknown
id.

**Roles, Session, Probes and Console are lines like any other** — walk them off
the checklist, and spend their full loads at the start of a block.

Restart the server after a change to `vite.config.ts`, `mock/` or a dependency;
everything else hot-reloads. Post a status line when a line is fixed or given up
(**Status line**).

**Done when:** every line carries a mark.

## 4 · Report

The checklist, filled in, in exactly this shape. It goes back to whoever
dispatched you: its first line is what the status line and the pull request
quote, and its `[ ]` lines are what the pull request carries.

```text
Mock verification: <component> · <N> screens, <M> flows · <p> pass, <f> fixed, <o> open

flow "<name>" (<role>)
- [x] <Screen>: <one clause: what you did and what the page did back>
- [x] <Screen>: FIXED <what was wrong>; <what you changed>. Re-walked: <what it does now>.
- [ ] <Screen>: <what happens>. Tried <what>, 3 attempts. <what still happens>.
- [~] <Screen>: <the truth that lives outside the app>.
screens in no flow
- [x] <Screen>: <reached at its route under <role>; what you did and what the page did back>
- [x] Roles: <role> on <route> bounced to <route>; <role> reached <screen>.
- [x] Session: ?auth=out ran the guard, signIn() returned to <screen>; Sign out left <screen>.
- [x] Probes: the empty <form> <what it did>; <detail route> with an unknown id <what it rendered>.
- [x] Console: clean | <error text>.
```

A green line is one clause; only `FIXED`, `[ ]` and `[~]` lines carry more. An
unreachable screen is a `[ ]` naming the navigation that failed, never an `[x]`
read off the source.

Then stop the server and confirm the port let go:

```bash
kill -- -"$(cat /tmp/mock-<component>.pgid)"
curl -sf http://localhost:5173/ >/dev/null && echo "STILL UP" || echo "STOPPED"
```

**Done when:** every line carries one of the three marks and no dev server is
left running.

## Status line

A person watching the build reads these. Three shapes, and your prompt says
where they go:

```text
Walking <component> in mock mode: <N> screens, <M> flows.
<Screen>: <what the page did>. Fixed, re-walked green.
<Screen>: <what the page did>. Open after 3 attempts.
Walk done: <N> screens, <p> pass, <f> fixed, <o> open.
```

The first at the start, the middle two whenever a line is settled either way,
the last at the end. A screen that passes is not news; the count at the end is.

## Never

- **Make the mock agree with the app.** `mock/handlers.ts` answers to
  `openapi.yaml`, the same document `src/generated/` came from, and to nothing
  else. A handler bent until a screen passes hides the defect from the deployed
  system too. A `501` is a handler you never wrote: write it against the
  contract.
- Run `git`, commit, or open a pull request. The record belongs to the agent
  that dispatched you: hand it the report block above, and post progress only
  where your prompt says to.
