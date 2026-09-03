# ADR-0010 — An issue's newest comment is its status line, and the actor working it writes it

**Status:** Accepted

## Context

The console's Task row renders **the issue's newest non-machine comment, flattened
to its first line** (`taskRowNote` in `apps/console/src/features/builds/lib/taskRow.ts`).
The BFF drops the platform's own machine-branded comments on read
(`commentViews` in `delivery/task/reads.go`), so that field carries "what a person
wrote or an agent said" and nothing else. A live, per-issue status field already
existed on the surface.

Nothing kept it current. The `aep` skill let only the **lead** comment, and only
twice in a cycle: one `Started: …` line per issue at fan-out dispatch, and a
diagnostic on anything left unfinished at the end. So:

- a fan-out wave froze every one of its rows on its dispatch line for as long as
  the wave ran — twenty minutes and up, on the exact issues where most of the
  work was happening;
- an issue the lead worked **inline** got no comment at all, start to finish;
- the subagent, the only actor that knew what was happening, was banned from
  saying it (`A subagent never runs git and never runs gh — no commit, push,
  branch, comment or PR`).

The run's progress feed does carry every tool call, including a foreground
subagent's (which is why `fanout_foreground.ts` exists). But the feed is keyed to
the *run*, not to an issue, and a person following one component follows its
issue.

ADR-0009 decided the opposite for `progress_item`: **infer** progress from tool
calls the agent has to make anyway, because "an instruction to report progress is
one the agent can skip with nobody noticing for a whole run". That reasoning does
not carry here and the two decisions do not conflict. A `progress_item` is a
status enum the platform can derive from a filename; a status line is prose about
intent on a durable GitHub artefact, and nothing can infer it from a `Write`
call. The failure mode is also different: a skipped `progress_item` leaves a row
`Pending`, while a skipped status line leaves the previous one standing — stale,
but never wrong about anything that has not happened.

## Decisions

1. **The newest comment is the status line, and its first line is the whole
   claim.** One line, present tense, naming the component and what is happening
   to it. Everything else about the issue is already on the issue.

2. **Whoever works an issue keeps its line current** — the lead on one it took
   inline, the subagent on one it was handed. Stated actor-neutrally in
   `## 2 · Work the issues`, as `### The status line`, and reachable by both
   readers of the skill. Only the actor doing the work knows what is happening,
   and a lead relaying a subagent's state would be inventing it.

3. **A subagent gets exactly one `gh` and no `git` at all.** `gh issue comment`
   on its own issue; the branch, the commits and the pull request stay the lead's
   ("You are the sole git writer"). The fan-out prompt list carries this as item
   9 — the command with the issue number filled in, plus the rule — because a
   subagent starts from its prompt and nothing else.

   Enforcement is skill prose, as the rest of that split already is: the SDK
   hands a subagent the same `allowedTools` as its parent (`runner.ts`), so there
   is no tool boundary to lean on here and never was.

4. **The rule cannot live in `references/component-contract.md`**, which is
   otherwise the subagent's whole contract. A reference ships byte-identical into
   a playground session — no overlay reaches it — so `workflow_skill.test.ts`
   forbids platform mechanics (`gh`, `git push`, `pull request`) in every file
   beside `SKILL.md`. Anything mode-dependent stays in the body, and this is.

5. **The lead's dispatch comment is deleted.** The subagent's own opening line
   lands within a turn of dispatch and says more, so the lead's was one comment
   restating a fact the subagent was about to state better. Its purpose — "the
   only thing a person watching sees between dispatch and the pull request" — is
   what the status line now serves continuously.

6. **Bounded by the answer, not by a schedule.** *Post when the one-line answer
   changes*, with a floor at both ends of the work. That is checkable against the
   comment already on the issue (if it is the line you would write, do not post),
   it needs no timer, and it puts the comment count at two to four per issue
   rather than one per phase whether or not anything moved. The beats that
   normally qualify are named: a component goes green, a web app's walk starts, a
   walk ends.

7. **`mock-verification` bans the record, not the reporting.** Its deny-list said
   "Run `git` or `gh`, commit, or comment anywhere"; it now names the record —
   `git`, commits, the pull request — and defers *where progress goes* to the
   prompt. It has to: the skill ships byte-identical into a playground session,
   which has no issue to post on.

## Consequences

- **Local mode drops the whole feature for one duplicated sentence.** A
  `drop-section` for `### The status line` and an empty `replace-text` for
  fan-out item 9 are both free; the per-issue loop's lead-in is not, and costs
  the overlay's duplication ratchet one of its eight paired passages (6 → 7).
  That sentence is where it is on purpose: a rule stated only in a section
  *beside* a numbered sequence gets skipped, so the obligation is named in the
  sentence the loop's steps hang off and the section behind it carries the
  shape.
- **More issue comments per cycle.** Each returns to the platform as an
  `issue_comment` webhook and is persisted raw in `webhook_payloads`. A status
  line names a component and a state; no credential belongs on one, and the
  test-user logins that do live on the roles gate ticket, which is a different
  issue and already redacted (`PublishedCredentialsMarker`).
- **A stale line reads as a live one.** A subagent that dies mid-phase leaves its
  last line standing, and the row's elapsed timer is what distinguishes running
  from stuck. That is the same guarantee the row had before, on fewer lines.
