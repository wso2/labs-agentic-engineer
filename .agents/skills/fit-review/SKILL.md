---
name: fit-review
description: Review the diff since a fixed point for overfit, code, comments, or docs shaped around the change instead of the scope they land in.
disable-model-invocation: true
---

Dispatch one sub-agent with `git diff <ref>...HEAD` (`<ref>` the user's, else `main`) and this brief:

"Overfit is code, a comment, or a doc shaped around this change instead of the scope it lives in. First read outside the diff: each changed file's module and its nearest `AGENTS.md`, README, `design/` note, and ADR. Per changed file, report whether the change fits the module's design or bends it (a special case, flag, or call-site patch the design should absorb). Per comment or doc change, report what sits above the document's scope: narrating the PR, restating a neighbouring doc, detail the bigger picture does not need. Recommend a trim for every finding; less is better. Under 400 words."
