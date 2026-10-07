---
name: comment-debt
description: Find comments in the diff since a fixed point that restate the code, narrate the change, or record a design decision.
disable-model-invocation: true
---

Scan every comment added or changed in `git diff <ref>...HEAD` (`<ref>` the user's, else `main`).

Code is the source of truth. A comment earns its line only as a very concise high-level summary or a low-level why the code cannot say. Report each comment that restates the code, narrates the change, or records a design decision, quoted with `file:line`. Fix: delete it; a design decision moves to an ADR.
