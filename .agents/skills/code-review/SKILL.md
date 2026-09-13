---
name: code-review
description: Review the diff since a fixed point on three axes, Standards (the AGENTS.md ladder, ADRs, design notes), Spec (the originating GitHub issue), and Fit (does the change sit at the scope of the module and docs it lands in). Use to review a branch, a PR, or work in progress.
---

Three parallel sub-agents review `git diff <fixed-point>...HEAD`, one per axis, so none pollutes another's context. This skill pins the inputs, dispatches all three, and reports them side by side.

## 1. Pin the fixed point

The user's ref if given, else `main`. Confirm `git rev-parse <ref>` resolves and the three-dot diff is non-empty before dispatching anything. Keep the diff command and `git log <ref>..HEAD --oneline` for every prompt.

## 2. Find the spec

In order: an issue number in the branch name, commit messages, or open PR body (`gh pr view --json body`), fetched with `gh issue view <n> --comments`; a path the user passed; a plan under `docs/design/draft/` matching the branch. Nothing found: ask once, and if there is no spec the Spec axis reports "no spec available" and is skipped.

## 3. Find the standards

For each changed file, the nearest `AGENTS.md` up the tree plus the root one, and any ADR in `docs/decisions/` or `design/` note the change touches. On top of those, the smell baseline below. Two rules bind the axis:

- **The repo overrides.** A documented standard wins; where it endorses something the baseline flags, suppress the smell.
- **Tooling is out of scope.** Anything `make lint`, `make typecheck`, `make deadcode-ts-check`, or `make license-check` would catch is not a finding.

## 4. Dispatch the three sub-agents

**Standards prompt:** the diff command, commit list, the standards files from step 3, the smell baseline pasted in full, and the brief: "Read every standards file. Report, per file or hunk, (a) every place the diff breaks a documented standard, citing file and rule; (b) every baseline smell, named, with the hunk quoted. Documented breaches may be hard violations; smells are always judgement calls. Under 400 words."

**Spec prompt:** the diff command, commit list, the fetched spec, and the brief: "Report (a) requirements missing or partial; (b) behaviour the spec did not ask for; (c) requirements that look implemented but wrong. Quote the spec line for each finding. Under 400 words."

**Fit prompt:** the diff command, commit list, and the brief: "Overfit is code, a comment, or a doc shaped around this change instead of the scope it lives in. Before judging, read outside the diff: for each changed file, its enclosing module and the nearest `AGENTS.md`, README, `design/` note, and ADR for that area. Then report, per changed file, the module's existing design and whether the change fits it or bends it (a special case, flag, or call-site patch where the design should absorb the need is a bend). Per comment and doc change, the scope of the document and what in the change sits above that scope: narrating the PR, restating a neighbouring doc, detail the bigger picture does not need. For every finding recommend the trim; less is better. Under 400 words."

## 5. Report

Print all three under `## Standards`, `## Spec`, and `## Fit`, verbatim or lightly cleaned, then one line per axis: finding count and the worst issue. The axes stay separate because a change can pass any of them while failing another; never merge or rerank across them.

## Smell baseline

Fowler's code smells (_Refactoring_, ch. 3) plus the repo's comment rule, applied on top of the documented standards. Each line reads *what it is* → *how to fix*.

- **Comment Debt**: code is the source of truth, so a comment earns its line only as an extremely concise high-level summary or a note on what the code cannot say easily (the low-level why). A comment that restates the code, narrates the change, or records a design decision is debt. → delete it; a design decision moves to an ADR.
- **Mysterious Name**: a name that does not reveal what the thing does or holds. → rename; if no honest name comes, the design is murky.
- **Duplicated Code**: the same logic shape in more than one hunk or file of the change. → extract the shared shape.
- **Feature Envy**: a function reaching into another type's data more than its own. → move it onto the data it envies.
- **Data Clumps**: the same few fields or params travelling together. → bundle them into one type.
- **Primitive Obsession**: a primitive or string standing in for a domain concept. → give the concept its own small type.
- **Repeated Switches**: the same `switch` or `if` cascade on the same type recurring across the change. → one map or polymorphism both sites share.
- **Shotgun Surgery**: one logical change forcing scattered edits across many files. → gather what changes together into one module.
- **Divergent Change**: one file edited for several unrelated reasons. → split so each module changes for one reason.
- **Speculative Generality**: abstraction, params, or hooks for needs the spec does not have. → delete; inline until a real need shows.
- **Message Chains**: long `a.b().c().d()` navigation the caller should not depend on. → hide the walk behind one method on the first object.
- **Middle Man**: a type or function that mostly delegates onward. → cut it, call the real target.
