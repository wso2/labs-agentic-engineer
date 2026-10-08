# `aep` — local mode overlay

This file is not a skill. It is the set of edits that turn the `aep` skill
into the playground's local-mode workflow: a plain project directory, `issues/*.md`
instead of the issues API, no remote, no pull request. `lib/skill_overlay.ts`
applies it when the mirror is written for `mode: "local"`
(`ADR-0004`); the platform's own runs read `SKILL.md` untouched, and nothing
copies this file into a session's plugin.

Everything above the first directive is prose and ignored. Four kinds of
directive, and every one must match its anchor in `SKILL.md` exactly once — a
non-matching or twice-matching anchor fails the run at startup rather than
silently leaving the platform's procedure in a local session. A section directive
must also target a leaf section, one holding no further heading: widening a
range over the section below it is the one way a matching anchor still loses
text, so `skill_overlay.ts` rejects it.

    <!-- replace-section: ## Heading -->    the payload becomes that section's body,
                                            heading kept
    <!-- append-section: ## Heading -->     the payload is added to the end of it
    <!-- drop-section: ## Heading -->       heading and body both go (no payload)
    <!-- replace-text -->                   exact text, matched at a line boundary
    …find…
    <!-- with -->
    …replacement (empty = delete those lines)…
    <!-- /replace-text -->

A section runs from its heading to the next heading of the same or higher level;
headings inside fenced code blocks are not headings. Prefer a section
directive. A `replace-text` anchor is prose, so it rots the moment someone
rewords the paragraph it points at — a loud failure, but still one somebody has
to come and fix. Every one below reaches text no heading can: clauses inside a
numbered list item, a line inside a fenced block, and a bullet list mid-section.

<!-- replace-section: ## Where you are -->

The working directory is a local folder, and the run works the whole project.
There is no git remote, no GitHub, and no PR. You edit the project in place,
and the files are the record. All other rules are the rules of the platform,
so what you tune here applies to a real run.

<!-- replace-section: ### The set -->

The working set is the `issues/<n>.md` files. Each file has YAML frontmatter
(`issueNumber`, `component`, `title`, `dependsOn`, `origin`), a
`> **Rationale:**` line, and the scope.

There is no status field. Do not add one. An issue is done when the App Paths
of its components already satisfy its Scope. Read each path from its
`design.json`, and look at the files. `dependsOn` names the components that
the component uses at runtime. It is not a build order.

Work the issues in ascending number. Get the working set again before each
wave.

<!-- drop-section: ### Establish branch identity -->

<!-- replace-text -->
   <App Path>; never run `git`. Progress: `gh issue comment <N> --body "<line>"`.
<!-- with -->
   <App Path> and `issues/<n>.md`; never run `git`. Progress: append the line
   to `issues/<n>.md` under `## Mock verification`.
<!-- /replace-text -->

<!-- replace-text -->
   git diff --cached --name-only    # what is ACTUALLY staged — read it
<!-- with -->
<!-- /replace-text -->

<!-- replace-text -->
   git push -u origin HEAD          # -u only on the first push
<!-- with -->
<!-- /replace-text -->

<!-- replace-text -->
   A restart finds the done issues from `(#N)`, so push after each issue.
<!-- with -->
   Do not push, and do not add a remote. The commit only helps the developer
   see the changes. Do it only in a repository:
   `git rev-parse --is-inside-work-tree >/dev/null 2>&1 && git add … && git commit …`.
   If the project is not a repository, only edit the files.
<!-- /replace-text -->

<!-- drop-section: ### The status line -->

<!-- replace-text -->
4. The data that you resolved: the `## Component <its name>` block of a
   "Platform-resolved dependencies" comment, and each `org-service` contract
   (pasted, as a path, or "undocumented").
5. Its issue's status line: the `gh issue comment` command with its issue
   number, and the rule in The status line.
   That command is the only `gh` it may run.
<!-- with -->
<!-- /replace-text -->

<!-- replace-section: ### The record -->

There is no PR and no status field. For each issue that you touched, finished
or not, add a dated note to the `## Progress` section of its issue file. Write
what you built and how you verified it. If the work is not finished, write what
you tried and the diagnostic (see Green in the component contract). Record
each spec change in the note of the issue whose gap it closes. Do not change
other parts of the file: the frontmatter belongs to the planner.

<!-- drop-section: ### Be idempotent -->

<!-- replace-section: #### The `endpoints:` half -->

There is no resolver here. Write one entry for each `component` or
`org-service` dependency from the design. The platform injects no address, so
the code uses its own default for `<DEP_NAME>_URL`.

<!-- drop-section: #### Finding an `org-service` contract -->

<!-- replace-section: ## Git and GitHub -->

- Do not add a git remote, run `git push`, or run a `gh` command.
- Do not renumber, delete or rewrite issue files, and do not add a status
  field. Write only the `## Progress` section of an issue that you touched.
- Do not delete or rewrite `.aep-playground/` (the state folder of the
  playground).
