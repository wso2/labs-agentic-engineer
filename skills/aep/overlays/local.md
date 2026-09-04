# `aep` — local mode overlay

This file is **not a skill**. It is the set of edits that turn the `aep` skill
into the playground's local-mode workflow: a plain project directory, `issues/*.md`
instead of the issues API, no remote, no pull request. `lib/skill_overlay.ts`
applies it when the mirror is written for `mode: "local"`
(`ADR-0004`); the platform's own runs read `SKILL.md` untouched, and nothing
copies this file into a session's plugin.

Everything above the first directive is prose and ignored. Four kinds of
directive, and every one must match its anchor in `SKILL.md` **exactly once** — a
non-matching or twice-matching anchor fails the run at startup rather than
silently leaving the platform's procedure in a local session. A section directive
must also target a **leaf** section, one holding no further heading: widening a
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
headings inside fenced code blocks are not headings. **Prefer a section
directive.** A `replace-text` anchor is prose, so it rots the moment someone
rewords the paragraph it points at — a loud failure, but still one somebody has
to come and fix. Every one below reaches text no heading can: clauses inside a
numbered list item, a line inside a fenced block, and a bullet list mid-section.

<!-- replace-section: ## Where you are -->

The cwd is a plain local directory the developer chose, and the run is scoped to
the whole project. There is **no git remote, no GitHub, and no PR** — you edit
the project in place, and the project tree is the whole record. Every convention
below is the platform's, unchanged: what you hone here transfers to a real run,
so honour it exactly.

<!-- replace-section: ### The set -->

List every `issues/<n>.md` under `issues/`. Each is markdown with YAML
frontmatter — `issueNumber`, `component`, `title`, `dependsOn` (component names,
not issue numbers), `origin` — then a one-line `> **Rationale:**` and the scope,
acceptance notes and files to touch.

There is **no status field** here, and you never add one. An issue is done
because the App Paths of the components it names already satisfy **its** Scope —
not merely because those components exist. Read each path from its `design.json`
and look. Satisfied → leave the issue out of the working set. Missing, empty, or
short of the Scope → it's in.

**The declarations.** An issue's `dependsOn` frontmatter names the **components**
its component consumes at runtime (component names, not issue numbers). That is a
runtime relationship, not a build order — it never holds an issue back.

<!-- drop-section: ### Establish branch identity -->

<!-- replace-text -->
   Read its comments too (`gh issue view <number> --comments`): a
   "Platform-resolved dependencies" comment carries an `org-service`'s
   coordinates.
<!-- with -->
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
   **Read that `--name-only` list against what you changed.** `git add` on a
   directory drops every ignored path inside it **silently, at exit 0**, leaving
   `git status` clean; the staged list is the only place the omission shows. What
   is missing there is missing from the build context, and the first sign is a red
   build minutes later in a component that compiled perfectly on your disk.
<!-- with -->
<!-- /replace-text -->

<!-- replace-text -->
   `(#N)` is what a crash resume reads to know this issue is done — push as you
   go, so a crash never loses more than the issue in flight.
<!-- with -->
   **Never push, never add a remote.** The commit is a diffing courtesy for the
   developer, not load-bearing, so guard it rather than probing for a repository:
   ```bash
   git rev-parse --is-inside-work-tree >/dev/null 2>&1 && git add … && git commit …
   ```
   A project that is not a repository is normal here — skip the commit and just
   edit files.
<!-- /replace-text -->

<!-- drop-section: ### The status line -->

<!-- replace-text -->
   <App Path>; never run `git`. Status line: <the gh issue comment command with #N filled in>.
<!-- with -->
   <App Path>; never run `git`.
<!-- /replace-text -->

<!-- replace-text -->
For **each** issue in the ordered set — and whoever works it, you inline or a
subagent you handed it to, keeps its status line current from start to done
(**The status line**):
<!-- with -->
For **each** issue in the ordered set:
<!-- /replace-text -->

<!-- replace-text -->
9. **its issue's status line** — the `gh issue comment` command above with **its**
   issue number filled in, and the rule that goes with it (**The status line**):
   one line, at both ends of its work and whenever the answer changes between
   them. That command is the only `gh` it may run, and its own issue is the only
   issue it may touch.
<!-- with -->
<!-- /replace-text -->

<!-- replace-section: ### The record -->

There is no PR to open and no status field to set. For **every issue you touched
this session** — finished or not — append a `## Progress` section to its issue
file (create it if absent) with a short, dated note:

- **Finished**: what you built, how you verified it.
- **Not finished**: what you tried, and the **Green** diagnostic. Leave the issue
  exactly as-is otherwise.

Touch nothing else in the frontmatter (`issueNumber`, `component`, `title`,
`dependsOn`, `origin`, `key` are the planner's). Never invent a status field — an
issue's done-ness is read from its App Path next run, same as this run read it.

<!-- replace-text -->
- **Work pushed but no PR open** → open the PR with a `Resolves` line for each
  `(#N)` in `git log origin/main..HEAD`.
- **A PR already open for this branch and the working set is empty** → verify
  its `Resolves` list covers every `(#N)` on the branch, add any missing with
  `gh pr edit --body ...`, and exit. Do not open a second PR.
- **Empty working set and nothing pushed** → nothing to do. Exit cleanly and say
  so.
<!-- with -->
<!-- /replace-text -->

<!-- replace-section: ### The `endpoints:` half -->

There is no resolver here: author one entry per `component` / `org-service`
dependency off the design. Nothing injects an address either, so what the code
actually runs on is its own default for `<DEP_NAME>_URL`.

<!-- drop-section: ### Finding an `org-service` contract -->

<!-- replace-section: ## Git and GitHub -->

- Add a git remote, `git push`, or run any `gh` command. There is no remote.
- Renumber, delete, or bulk-rewrite issue files, or add a status field to one —
  only the `## Progress` section of an issue you touched is yours to write.
- Delete or rewrite `.aep-playground/` (the playground's state dir).
