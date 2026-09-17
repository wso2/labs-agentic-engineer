---
name: send-pr
description: Take the current branch through the repo's PR checklist and open the pull request.
argument-hint: "[base branch, default main] [issue number or spec path]"
disable-model-invocation: true
---

# Send PR

A PR must be production ready before its marked as ready for review. A PR description is focused be refined to reflect the change in a high level consisely to the reviewer.

1. **Review the diff.** Run the `code-review` skill with the base branch as the fixed point. Done when every finding is either fixed or explicitly waived by the user.

3. **Update documentation.** Find the README, ADR, or `design/` note that already covers the changed behaviour and edit it at that document's scope: place the change in the bigger picture it describes, add nothing a neighbouring doc already says, and trim aggressively, less is better. Comments follow the same rule. Done when no doc contradicts the code on the branch and no doc narrates this PR.

4. **Create a draft PR** You have to focus on explaining the change consisely in a high level to a maintainer whos operating at the design level. You need to include diagrams, proof of live executions(with screenshots for any UI involement cases), API changes (if any) and reasoning, pointers to test cases which coveres real data of execusion which calls these apis, impacted user stories  etc in the PR description.  If you don't have any of these, keep the PR in draft, work until these are available and make the edits.

5. **Open the PR** Once you are confident that the PR is ready to be reivewed by a maintainer and PR description has all the info, mark it as ready.
