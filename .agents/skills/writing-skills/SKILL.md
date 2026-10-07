---
name: writing-skills
description: "Use before you change a file in skills/ or .agents/skills/, or an AGENTS.md file."
---

Each change to these files starts as a proposal. Write the proposal in a
Claude Doc. If the docs connector is not available, write it as an HTML
artifact. For a change of one or two lines, write the proposal in the chat.
Then make the change and do the verify loop. Record in the proposal what you
did. Do not commit until the user examines the proposal and the diff.

## Proposal Format

The proposal has these four sections:

- **Evidence**: The data that shows the need for the change. For a fix, this is a run that failed or an issue. For an optimization, this is the size or the duplication. For a new skill, this is a gap that no skill owns.
- **Change**: The files that you change, with the text before and after the change.
- **Placement**: The reason that this skill owns the change, and the location in the skill that solves the root cause. For a new skill, the reason that no current skill owns it. If the root cause is not in a skill, its location (for example, the `--help` of a tool or agent code).
- **Changelog**: A record of each change that you make, and the eval results of each pass of the verify loop.

## Verify Loop

1. If the change is in `skills/`, run the playground before you edit the skill:
   - Use a Sonnet subagent. Refer to `playground/AGENTS.md` for the procedure.
   - For a fix, make the failure occur again.
   - For an optimization, record the behavior that you must keep.
   - For a new skill, show the gap.
   - The playground does not deploy. Do not use it for `validation-task` or for a skill that needs a live deployment. For these skills, do a live run in the cluster.
2. Change the skill. Select the best solution for the problem.
3. If you did step 1, run the playground again. Compare the result and the tool-call logs with the result of step 1. For a new skill, make sure that the agent loads the skill from its description at the correct time.
4. Examine the change against each Skill Statement Rule. If the change does not follow a rule, change the skill and examine it again.
5. Record each pass of the loop in the Changelog section of the proposal.

## Skill Statement Rules

- Put in a skill the information that an agent needs to make its decisions.
- The agent uses the name and the description to select the skill. Write a description that tells clearly what the skill does and when to use it. Keep it as short as possible.
- Give each fact one owner and one area.
- `metadata.aep.audience` sets which agents read a skill: design agents, coding runs, or both (if the field is not there). Put a fact in a skill that its reader loads.
- Record the history of a rule in git or in an ADR, not in the skill.
- Make the skill easy to maintain. Put related items together.
- If the agent can read content only when it needs it, put that content in a reference file, not in the skill body.
- Do not add a patch to a skill for a problem that you found. Find the root cause and fix it where it occurs.
- Use ASD-STE100 format.
