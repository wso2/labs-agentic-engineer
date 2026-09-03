---
name: mock-verification
description: Load once a `web-application` builds clean, to finish it — stand the app up in mock mode with no cluster behind it, walk every screen in a real browser, and fix each failure the moment you find it. Judging a DEPLOYED system against live infrastructure is `aep-validation`'s job instead.
metadata:
  aep:
    kind: platform
    audience: [coding]
---

# Mock verification

This app compiles, type-checks and bundles — and none of that says what happens
when somebody opens it. A page can render the wrong screen, drop a navigation
arrow its wireframe draws, or leave a button wired to nothing, and every one of
those builds perfectly clean.

**So you open it.** Mock mode stands the app up on this machine with no cluster,
no sibling service and no IDP behind it — the harness at `react-webapp`'s
`references/mock-mode.md`. **The build starts a web application; the walk is what
finishes it.**

**You verify and you repair, in one pass.** Walk a line, and the moment it fails
fix it, walk that same line again, and only then start the next. You are not
filing a report for somebody else to act on — you hold the component open, and
the cheapest moment a defect will ever be is the one you found it in.

**The issue is where the change landed, never where the walk stops.** Everything
a user can reach in this component is yours to walk, however long ago it was
built. A cycle produces regressions — through a shared navigation bar, a layout,
a regenerated client, a page edited for an unrelated reason — and a walk scoped
to what just changed is the one shape that cannot see one.

## 1 · Write the checklist

Before the browser opens. It is your walk order, and its filled-in verdicts are
your report. One line per screen, and under it every control that screen draws.

1. **`specs/design/components/<component>/wireframes.dsl`** — the boundary. Every
   `screen` is a route that must exist, every `-> Screen` arrow is navigation
   that must work, every `flow` is a journey that must walk end to end by
   clicking, every control drawn is a control that must do something when used.
   A component with no `wireframes.dsl` gives you its routes instead — every one
   the app registers.
2. **`specs/requirements/`** — *why*. `prd.md` (or `requirements.md` on an older
   project) says what the actor wanted, so a screen that satisfies an issue's
   wording while defeating that is a defect. Where the requirements number their
   stories, use those numbers.
3. **Your issue's `## Scope` and `## Acceptance criteria`** — walked first and
   hardest. They add emphasis to the list; they never trim it.

Settling the list first is what makes coverage checkable: a screen you never
reached shows up as a line with no verdict, where a list assembled as you go
simply never mentions it.

**Done when:** every screen the contract names has a line, and every control it
draws sits under one.

## 2 · Stand it up

From the App Path:

```bash
setsid --fork bash -c 'echo $$ > /tmp/mock-<component>.pgid; exec npm run dev:mock -- --port 5173 --strictPort' > /tmp/mock-<component>.log 2>&1 &
for i in $(seq 1 30); do curl -sf http://localhost:5173/ >/dev/null && break; sleep 2; done
curl -sf http://localhost:5173/ >/dev/null || tail -40 /tmp/mock-<component>.log
```

That line looks fussy and every part of it is load-bearing. `npm run` is three
processes — npm, the `sh -c` it spawns, and the server — so the **group** is the
only handle that reaps all three; kill any single pid and the server outlives it
holding the port. `setsid --fork` puts the group under its own session leader,
and the leader then **writes its own id**: `$$` inside the quoted command is the
group, and `exec` keeps that pid when npm takes over.

**Do not reach for `$!` here.** It names `setsid`, which forks and exits
immediately, so the pid you record is dead and its number is not the group's —
`kill -- -"$(cat …)"` then answers `No such process` while the server keeps
serving. This shell has job control on, which is exactly the condition that
makes `setsid` fork.

This machine has no `ps`, `pgrep`, `pkill` or `lsof`, so a server you cannot
address by its group is yours for the rest of the session.

`--strictPort` is deliberate: without it Vite moves quietly to the next free
port, and a browser pointed at 5173 would then be reading a **previous** run's
server while you drew conclusions about this one. A refusal to bind means an
earlier server still holds the port — end it with its group file
(`kill -- -"$(cat /tmp/mock-<component>.pgid)"`) and start again.

**Done when:** the URL answers and the log holds no error. If it will not start,
that is the whole finding — fix it and start again; there is nothing to walk.

## 3 · Walk it, a line at a time

Load `agent-browser` and follow it. Open the app, snapshot to see what rendered,
act on what the snapshot shows.

**A line ends green.** Walk it; if it fails, fix it now, walk that same line
again, and see it pass before you start the next. A fix walked immediately is a
fix proven, where a fix batched to the end is an edit that merely compiles.
**Repair the app, never the checklist.**

**Every failure is yours, whichever issue put the defect there.** The walk
reaches past your issue precisely so it finds these; setting one aside as
somebody else's leaves the defect on the screen and wastes the walk that found
it. Name what you repaired in your report.

**Three attempts on a line, then mark it `[ ]` and walk on.** A defect that
resists three tries belongs in the report, and the screens behind it are still
unopened. Restart the server after a change to `vite.config.ts`, `mock/` or a
dependency; the dev server hot-reloads everything else.

For each line of the checklist:

- **Reach the screen the way a user would** — from the entry screen, by
  clicking. A route reachable only by typing its URL is a defect when a `flow`
  says a link should have taken you there.
- **Do what the story describes** — add the item, submit the form, filter the
  list, follow the arrow — then snapshot and read what changed. A create really
  does appear in the next list.
- **Use every control the screen draws.** A control wired to nothing is precisely
  what a clean build cannot see: it type-checks, it renders, and it does nothing.
- **A change is not made until it leaves the page.** A screen that flips a
  checkbox or greys a row has told you what it *intends*; the request is what
  makes it true. Check with the CLI's network verb that the call went out and
  what it answered.
- **Move between screens by clicking.** Mock state lives in the page, so `open`,
  reload and back each re-run the module and restore the seed data — a record you
  created a moment ago is simply gone, and the screen showing that is telling the
  truth about a mock, not about the app. Spend full loads at the START of a
  block, where there is no state to lose: a role switch, `?auth=out`, an
  invalid-id check.
- **Check the states a wireframe implies but does not draw**: a table with no
  rows, a form with an empty required field, an invalid id.
- **Read the console** with the CLI's own verb. A page that renders and throws is
  broken for whoever touches it next, and the error text is the finding.
- **With roles** (`mock/roles.ts` exists): visit the screen under each role that
  should reach it, via `?role=<name>`, and under one that should not. Both
  directions are defects — a gated screen rendering for the wrong role, and one
  refusing the right role.
- **Signed out**, where a story covers it: `?auth=out` makes `currentUser()`
  resolve null, so the app's own guard runs. `signIn()` then drops the parameter,
  which stands in for returning from the IDP, so the whole journey walks.

**Done when:** every line is green, marked `[~]`, or carries what you tried and
what still happens — and every verdict names what you did and what the page did
back.

## 4 · Report

One block, exactly this shape. It goes back to whoever dispatched you, and its
open lines are what the pull request carries as a diagnostic — so each names a
screen and what happens on it.

```text
Mock verification — <component>

- [x] 3 · Add a todo — /todos/new: typed "Buy milk", clicked "Create todo";
      POST /api/todos → 201, row present in the list on return.
- [x] 7 · Mark a todo done — FIXED: the checkbox flipped and sent nothing;
      wired onChange to PATCH /api/todos/:id. Re-walked — PATCH → 200 and the
      row stays done across in-app navigation.
- [ ] 9 · Open a todo — /todos: the title is plain text, not a link;
      wireframes.dsl draws `table "Title | Due" -> TodoDetail`, and /todos/1
      renders the 404 page. TodoDetail has no route registered; adding one needs
      a screen the design does not specify.
- [~] 12 · Overdue count is accurate — truth lives outside the app: the count is
      computed by todo-api, and the mock returns seeded values.

Roles: Manager saw every row; Owner saw only their own (correct).
Console: no errors.
```

Three verdicts:

- `[x]` — it does what the story says. Add `FIXED:` and what you changed when
  you got it there.
- `[ ]` — still broken after three attempts. Name the screen, the action, what
  happens instead, and what you tried.
- `[~]` — **the story's truth lives outside the app**: a total the real service
  computes, a permission the gateway enforces, a mail that gets sent. Not a
  failure, and nothing to fix. Say which in one line — marking these honestly is
  what keeps the other two worth reading, and they remain `aep-validation`'s to
  judge once the system is deployed.

Then stop the server and confirm the port let go — a teardown you did not check
is how the next round inherits a held port:

```bash
kill -- -"$(cat /tmp/mock-<component>.pgid)"
curl -sf http://localhost:5173/ >/dev/null && echo "STILL UP" || echo "STOPPED"
```

**Done when:** every checklist line carries one of the three marks, and no dev
server is left running.

## Never

- **Make the mock agree with the app.** `mock/handlers.ts` answers to
  `openapi.yaml` — the same document `src/generated/` came from — and to nothing
  else. A handler bent until a screen passes is a green report about nothing, and
  it hides the defect from the deployed system too. A `501` is different: that is
  a handler you never wrote, so write it against the contract.
- **Judge a story you could not reach.** An unreachable screen is a `[ ]` naming
  the navigation that failed, never an `[x]` inferred from the source.
- Run `git`, commit, or open a pull request. The record belongs to the agent that
  dispatched you — hand it the report block above, and post progress only where
  your prompt says to.
