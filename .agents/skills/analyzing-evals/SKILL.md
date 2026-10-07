---
name: analyzing-evals
description: "Use to explain a codegen eval result under evals/codegen/.runs/: why an attempt lost points, the root cause, and what to optimize in skills, assets, CLIs or agent code. Writes a Claude Doc with proof, root cause and fix location."
---

# Analyzing evals

Find the cause of each lost point. Then find the location of the fix. Do not
fix anything with this skill: it gives the evidence for a skill change
(`writing-skills`) or a code change.

The archive layout and the commands are in `evals/codegen/README.md`. The
location of each fact of a coding run is in `playground/AGENTS.md`, section
"Examine the run".

## 1. Read the result

1. Read the sweep's `report.md`. Select each attempt with a low score, a hard
   fail or a harness error.
2. For each attempt, read `attempt.json`. A hard fail has no walk and no
   verdict: read its `symptom`, `wire/wire.log` and `coding/play.log`, then go
   to step 2.
3. Read `judge/verdict.json` and `walk/result.json`. The items are in
   `checklist.yaml` of the attempt, not of the case: the case's file can change
   after the sweep. An older attempt has no `checklist.yaml`. Then get the item
   text from `walk/result.json`, and say so in the analysis.
4. Open the screenshot of each failed item in `walk/shots/`.
5. If the sweep has repeats, group each failed item across the attempts of
   the same case and config. A failure in most attempts is a pattern. A
   failure in one attempt is a suspicion. Find the root cause of patterns
   first.

For a large sweep, give each case to a subagent that uses this skill. Then
merge their findings into one doc.

## 2. Classify each failure

Do this before you look for a root cause. A fix in the wrong system breaks a
correct one.

| Class | Signal | Owner of the fix |
| --- | --- | --- |
| Eval defect | The item asks for a control that `wireframes.dsl` does not draw, or a state that wired mode cannot show. Or the walker misread the page. | The case's `checklist.yaml`, or the planner (`evals/codegen/src/planner.ts`, `wired-auth.ts`) |
| Harness defect | A `harness-error`, or wire, Docker or the walker failed for a reason outside the generated code. | `evals/codegen` or `playground` code |
| Spec defect | The case's `specs/` contradict each other, or cannot make the precondition. The checklist's gaps header names these. | The design skill that wrote that artifact |
| Fixture defect | The case's input is not what production sends. For example, production derives a field (such as `wiring` in `design.json`) that the playground does not. | The playground, or `eval-save` |
| Generator defect | The generated app does not do what the specs say. | Step 3 |

To confirm an eval defect, change the checklist and run `rewalk`. Do not start
a new coding run for it.

## 3. Find the root cause of a generator defect

1. Find the wrong file in `project/`. For a failed request, read
   `wire/logs/services.log`.
2. Find the tool call that wrote the file:
   `make eval-codegen ARGS="log --attempt <attempt dir>"`. Search the output
   for the file name.
3. Read the reasoning near that call. Add `--thinking`.
4. Find the instructions that the agent had: the skills in
   `project/.claude/skills/` (the exact text of that run), the system prompt in
   `coding/<run>/.logs/prompt-appendix.md`, and the `Skill` lines in the log.
   The report that each subagent gave the lead is a `task_notification` line
   in `.logs/runtime.log`. It often names a gap that the agent saw.
5. Compare that text with HEAD. `provenance.json` and `skills.diff` show the
   uncommitted edits that the run used.
6. Put the cause in one location:
   - No skill has the rule.
   - A skill has the rule, but the agent did not load it (description,
     audience, or not pinned).
   - The agent loaded the rule but did not obey it. The rule is outside the
     numbered steps, or a different rule overrides it.
   - Two skills tell different things.
   - A tool's `--help` or output misled the agent.
   - A boilerplate asset is wrong.
   - The agent or runner code is wrong.

## 4. Find what to optimize

Read the coding run with `log --attempt <attempt dir>` and with `--slow`.
Measure each candidate. Then give it an owner.

| Signal | Measure | Usual owner |
| --- | --- | --- |
| A slow call or a long gap | seconds | the tool, or the skill step |
| A command fails, then the agent tries again | count | the tool's `--help`, or the skill step |
| The agent reads the same file again | count | the skill: state the fact one time, where the agent needs it |
| A large tool result | characters | the CLI output |
| The same hand-written code in each run | lines | an asset of the stack skill |
| Mock-verification items that the walk fixed | items | the build skill that missed them |
| Time and tokens of each subagent | `log --attempt <attempt dir> --usage` | the fan-out step |

## 5. Write the analysis as a Claude Doc

Write the result in a Claude Doc. If the docs connector is not available,
write it as an HTML artifact. Start with a table of all findings: class,
symptom, root cause location, fix location, confidence. Then write one
section for each finding:

- **Symptom**: what the user sees, or the measure. Give the item id or the
  signal.
- **Proof**: the data that shows the cause. Quote the log lines with their
  `seq` (reasoning: its line in `runtime.log`), the code with its file and
  line, and the skill text. Upload the
  screenshot of a failed item into the doc.
- **Root cause**: the chain from the instruction to the wrong result, and the
  location from step 2 or step 3.
- **Fix location**: the file, and the section in it, that must change. Tell
  why the fix goes there and not where the symptom is.
- **Confidence**: one attempt gives a suspicion only. To confirm, use
  `--repeats 3`, a `rewalk`, or the same case in an earlier sweep.

A finding about a skill is the Evidence section of a `writing-skills` proposal.
