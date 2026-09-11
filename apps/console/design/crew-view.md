# The crew view

How one cycle of a coding run is read on the Builds page's **Coding agent log**
card. Written after the fact; it describes what is there.

> **A reader must never wonder whether the run is stuck.**

That sentence governs everything below. A run fans out to several agents at
once, and the longest recorded one took 55 minutes with 41 of them spent inside
a single spawned agent. A flat log could say what had happened; it could not say
whether anything was *still* happening, because a stall on a log surface looks
exactly like a log that has scrolled.

## Two views, never both

| | answers | when a reader wants it |
|---|---|---|
| **Crew** (default) | who is doing what right now | while the run is going |
| **Timeline** | where did the time go, and what ran at once | after, or when it is taking too long |

One toggle switches between them and they are **never shown at once**: both draw
the same agents, so stacking them spends the cycle's height twice on one fact.
The choice is remembered per browser (`localStorage`, key
`aep:builds:run-view`, every access in a `try/catch` — a private window must not
break the page) and it is **one choice for the whole page**, not one per
accordion: a run holds several cycles, and switching one while the others stayed
put put a crew and a timeline on screen together, which is the thing the toggle
exists to prevent.

Crew is the default because liveness is the primary job. A timeline is a
retrospective, and a retrospective is the wrong thing to open on a live run.

Beside the toggle sits the one fact both views share: **how many agents, how many
are running, and how long since the last event**. The age is shown only while
something is running — on a settled cycle it would count how long ago the build
was, which the page header already says.

## A crew of one is still a crew

A validation cycle runs a single validator; a coding cycle is one agent until it
fans out. Those cycles get the **same tree and the same inspector** a fanned-out
one gets, from the first agent.

The alternative — draw the flat form until a second agent arrives, then become a
tree — costs more than the row it saves: the layout changes shape mid-run, at the
moment the reader is watching it most closely, and a surface they had just
learned is replaced by another one. With the tree there from the start, a spawned
agent **appears in place**, as a row under the lead, and nothing else moves.

The **timeline** is the exception, and not on chrome-avoidance grounds: a
timeline compares lanes, and one lane compares nothing — it draws a single bar
spanning the cycle, which the hint beside it already says in words. So the toggle
appears with the second agent. That *adds a control* rather than moving anything
already on screen, which is the rule above. A reader whose remembered choice is
the timeline still gets the crew on a single-agent cycle; their choice stands for
the next cycle that fanned out.

## What each region owes the reader

**Crew tree.** One row per agent, indented by the depth the runtime *declared*
(`agent_started.depth` / `parentAgentId`) rather than by anything inferred. Each
row: a state dot, the agent's name, one live sub-line in the runtime's own words,
and how long it has been going.

**The tree holds agents and nothing else.** It carried a row per backgrounded
shell command until a live run produced 47 of them, and the column that answers
"who is working" became mostly raw command lines — the answer buried in its own
evidence. Two earlier attempts to save those rows are worth recording as
rejected, because both treated a symptom: truncating them from the other end
(what identifies a command is its head, not its tail, unlike a path), and
eliding them in the middle. Neither addressed the row count, and the row count
was the problem.

The concern that put them there stands: an orphaned `dev:mock` still holding a
port after the run ends has to have somebody's name on it. That name is on its
`task_settled` row in the **inspector**, under the agent that ran it.
`CrewMember.tasks` is still computed and still true; nothing in the tree renders
it. The cost, stated plainly: a subagent's commands are one click away rather
than on screen from the start, and for the lead — the default selection — they
are already on screen.

Under those sit the agent's **plan** entries — the lead's own task list, which
reaches the feed as `work_item {source: "plan"}` and is folded onto its owner by
`buildCrew`. An entry the lead handed to a spawned agent draws under *that*
agent, because "what was this one sent to do" is the question a reader has about
its row; an entry naming an agent no event ever declared falls back to the lead
rather than vanishing. They stay on a settled agent: the list is what it set out
to do and whether it got there, which only becomes a record once the run is over.
Deliberately quiet — only the glyph carries the entry's weight, so a fifteen-entry
list cannot outshout the one question this view exists to answer.

**Inspector.** The selected agent's steps, with each outcome merged onto the
action row it answers (the same `mergeOutcomes` rule the flat feed used, on the
same `AgentSteps` component). Its header is the runtime's *own* totals —
duration, tool count, lines written — because the runtime measured the agent's
whole life including the parts that never reached this feed. Under it, the
agent's closing report: a spawned agent's transcript dies with its pod, so this
is the only copy. Then its **plan**, above its steps — what it set out to do,
above what it did. That repeats the tree's rows for the selected agent, the same
way a settled agent's report is both its tree sub-line and the note here — the
labelled copy, where the tree's rows sit unlabelled under the agent whose list
they are. Nothing here is invented; there is no narration tab and no file
list.

**Timeline.** One lane per agent on one axis, the cycle's first word to its last.
A lane is coloured by its agent's state and split into **solid** (working) and
**faded** (waiting on another agent) stretches. A depth-2 agent is a lane like
any other — the indent lives in its *label*, never in its bar, because indenting
the bar would move it on the time axis. Picking a lane returns to **Crew** with
that agent selected: "where did the time go" always ends in "so what was it
doing".

## Liveness, exactly

Liveness is a property of every row, not a line somebody has to find. Every
agent's last-activity age is recomputed from a **client ticker** — it has to
move while nothing arrives, which is the whole point, and a quiet SSE stream
gives React no reason to re-render on its own. The ticker runs only while
something is still running.

Colour follows one rule, and the order is the content:

- **blue** while events or heartbeats arrive — or while the agent has simply not
  been quiet long enough to be interesting;
- **blue, "waiting on X"** when the agent is blocked inside a *foreground*
  spawned agent. Not idle, and never amber: the silence is fully explained by a
  row one level down, and a fan-out call is therefore not "a tool in flight". A
  background child never blocks its parent;
- **amber** after **60 seconds** of silence *with a tool call still unanswered*.
  Twice the longest normal gap in the recorded 55-minute run; the watchdog this
  replaces used 120s and was too slow to reassure anyone;
- **red** only when the runtime reported a failure, or a deadline terminated the
  run.

**Silence is never a verdict.** An agent quiet for ten minutes with nothing
outstanding is blue with a ten-minute age beside it. The age is the honest
report.

Colour is never the only signal: the state's word sits on the sub-line beside the
dot, and a settled agent's dot is a ring rather than a disc.

An amber row always says what it is amber *about*. Where the watchdog spoke, its
sentence is the caption (`heartbeat.waitingOn` + `ref` + `elapsedMs`, or a
`notice` for a retry, a compaction or a rate limit); where it did not — which is
most of the time, since the recorded runs carry almost no heartbeats — the
unanswered call is named instead.

A notice is promoted to a caption only while it is still the newest thing that
happened. A retry explains the current silence; the same retry two tool calls
later explains nothing.

## Where the meaning lives

`buildCrew(events, now)` in **`@aep/progress-view`** owns all of it: the tree,
each agent's state, its caption, its age, its lane spans and the 60-second rule.
It is pure and takes `now` as an argument, so this console's ticker is the only
clock in the picture and a test can drive the model to the exact instant a rule
fires. It is unit-tested there against two real recordings.

The console owns only the drawing: the toggle, the two layouts, the ticker, and
the mapping from a semantic tone to an Oxygen palette entry
(`components/logTone.ts`). No theme token crosses into the package — a TUI
imports it too.

## Files

| | |
|---|---|
| `components/RunCrew.tsx` | the toggle, the hint, the clock, the selection |
| `components/CrewTree.tsx` | one row per agent, with its tasks and its plan |
| `components/CrewInspector.tsx` | one agent's header, report, plan and steps |
| `components/AgentPlan.tsx` | a plan entry's row, and the inspector's list |
| `components/CrewTimeline.tsx` | one lane per agent |
| `components/AgentSteps.tsx` | the shared row, report note and empty copy |
| `hooks/useRunView.ts` | the remembered choice, shared across the page |
| `hooks/useTicker.ts` | the second hand |
