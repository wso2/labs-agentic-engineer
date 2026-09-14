# ADR-0004 — The workflow skills live in the library; local mode is an overlay

**Status:** Partly superseded (2026-08-03) · shipped 2026-08-02 · supersedes
[ADR-0001](ADR-0001-one-mode-composed-skill.md)

> [!IMPORTANT]
> **Its delivery half is superseded by
> [ADR-0005](ADR-0005-the-workflow-rides-the-project-mirror.md).** The one
> library and the overlay mechanism below stand as written — that is decisions
> 1–6, and the tests that replace the marker tests. What changed is *how the
> skills reach a session*: there is no plugin any more, so **decisions 7 and 8
> are void** and decision 9's path has a new value. The three skills declare
> `audience: [coding]` and arrive in the BFF's project-repo mirror like every
> other coding skill; the overlay is applied while that mirror is written.
> Read ADR-0005 for the delivery story and this ADR for the overlay's grammar
> and its safety argument.

## Context

There were two authored skill trees. Repo-root `skills/` held the design-flow and
stack skills, shipped in the `aep-api` image and reconciled into every org's
`org-skills` repo. `runners/remote-worker/plugin/skills/` held `aep`,
`aep-validation` and `playwright-cli`, baked into the runner image and loaded as a
Claude Code plugin.

Nothing about that split was principled. It meant two places to author a skill,
two frontmatter conventions (the runner's three had no `metadata.aep.kind` at
all), and two delivery stories to keep in your head — and it made the runner's
workflow skill invisible to every tool the library already had.

ADR-0001 had solved a different problem inside the runner tree: local mode (the
playground) used to be a **second plugin** whose "shared" project conventions were
a copy-paste of the platform's, and that copy had drifted in three measurable
ways. Its fix was one authored `SKILL.md` carrying `<!-- mode:github -->` /
`<!-- mode:local -->` regions, stripped per run. That held, and its measurements
still stand: of the 507 lines a local run read, 447 (88%) were the platform's own
text.

But markers in the skill are a cost the library makes visible. A skill in
`skills/` is read by humans, listed to every design-time agent, and installable
into a developer's own Claude Code. A document interleaved with a second
audience's procedure is the wrong artifact to be the canonical one.

## Decision

**One library.** The three runner skills move to `skills/`, stamped
`metadata.aep.kind: platform` — AE-owned, read-only in the console, seeded into
org repos like the other platform-owned flow skills. The org-repo copy is inert
duplication (the runner reads the image, not the clone) and that is the price of
uniformity, paid deliberately.

**The authored skill is the platform's run, with no mode markup in it.**
`skills/aep/SKILL.md` reads as what a dispatched run does.

**Local mode is an overlay**, `skills/aep/overlays/local.md`: a markdown file of
anchored edits, applied by `lib/skill_overlay.ts` when `lib/base_plugin.ts`
assembles a session for `mode: "local"`. Four directives —
`replace-section` / `append-section` / `drop-section` (heading-anchored) and
`replace-text` (an exact, multi-line, line-aligned literal).

Consequences of the shape, in the order they were decided:

1. **Every directive must match exactly once, or the run dies at startup.** This
   is the whole safety argument. The failure mode of find-and-replace against
   prose is a *silent* miss, which would leave the platform's `gh`/PR procedure in
   a local session — the exact failure ADR-0001 decision 7 existed to prevent. A
   heading that appears twice is as much of an error as one that appears never.
2. **The parser rejects what it does not understand.** A column-0 comment that
   is not a directive, a misspelled directive name, a `drop-section` carrying a
   payload, an unterminated `replace-text`, an overlay with no directives at all:
   all throw. Directives sit at column 0 precisely so the overlay can document
   its own grammar with indented examples.
3. **Section anchors are preferred; text anchors are the exception.** A prose
   anchor rots when someone rewords the paragraph — loudly, but someone still has
   to fix it. Four `**bold lead-ins**` in the skill became real headings
   (`### The endpoints: half`, `### Contracts`,
   `### Finding an org-service contract`, `### External dependencies`) so the
   overlay could anchor to structure instead. They were already acting as
   headings, and a 100-line section wanted subheadings anyway. The four text
   anchors that remain point at a fenced line, two clauses inside a numbered list
   item, and a bullet list — places no heading reaches.
4. **The trunk keeps GitHub mechanics in three named places** — `## Where you
   are`, `# The run`, and a `## Git and GitHub` subsection of `# Never`. The
   overlay can only reach what it can anchor, so a `git push` rule dropped into a
   shared contract section would be read as true by a local run. A test asserts
   every `git`/`gh` invocation in the skill is under one of those three.
5. **Markdown, not a TypeScript table of edits.** The payloads are skill prose
   thick with backticks and fenced blocks; in a template literal every backtick
   needs escaping, on the exact bytes that steer a model. And the overlay has to
   stay editable through the `skills/` mount the playground already has — a `.ts`
   file under `src/` is baked into the image, so tuning local guidance would need
   an image rebuild, which is precisely what the playground exists to avoid.
6. **`overlays/` is compose-time input, never skill content.** The assembler
   filters it out of the plugin it writes, and `aep-api`'s `loadLibrary` skips it
   when seeding (it would otherwise land in every org repo and in `ContentSHA`).
   The `aep` skill explicitly permits the agent to read its own skill dir, so an
   overlay beside `SKILL.md` in a production session is a second procedure the
   agent can find — ADR-0001 decision 5 in a new costume.
7. **The plugin is a selection, not the library.** `base_plugin.ts` lists the
   three runner skills explicitly. Handing a coding session the whole library
   would put `design`'s description in its skill list, one `loadSkill` away from a
   mandate to author `specs/`.
8. **Assembling stays inside `startCodingRun`** (ADR-0001 decision 6, unchanged):
   entrypoints pass a mode, never a directory.
9. **`$AEP_SKILLS_DIR` replaces hardcoded runner paths.** `aep-validation` invoked
   the platform's report generator through `/app/plugin/...`, which stopped
   existing when the plugin became an assembled artifact. The runner is the only
   layer that knows where the library is, so it stamps the path into the agent's
   env — the same argument as `promptWithProjectRoot`.

## What replaces the marker tests

`skill_compose.ts` and its 350 lines of marker tests are gone.
`base_plugin.test.ts` keeps the properties that were doing the real work, because
they are properties of the *outcome*, not of the mechanism:

- the shared sections are byte-identical across both composed bodies;
- neither body contains the other's landmarks;
- ~19 named cross-stack rules appear in both (each one was a real one-sided bug
  when local mode was a separate file);
- paired passages (a `replace-section`, or a `replace-text` with a non-empty
  replacement) are capped — the ADR-0001 ratchet, recounted, and it only ever
  goes down;
- github mode is byte-identical to the authored file, so what a reviewer reads is
  what a production run is steered by.

## Consequences

- Editing a project convention is one edit in one library, and it provably lands
  in both modes.
- The residual risk this shape carries, stated plainly: **drift by omission.** A
  new GitHub-shaped rule added outside the three named sections is wrong for local
  mode and has no anchor to remove it. Decision 4's test is what closes it; if
  that test is ever loosened, this ADR is no longer true.
- Reviewing a paired passage now means opening two files, where markers put the
  two variants side by side. Mitigated by the composed body being written under
  the run dir in local mode (`composeDir`) — "what did the agent actually read"
  stays one `cat`.
- ~200K is copied per run, unchanged, and it is what keeps bind-mounted live
  skill editing working.
- The hand-install path is now `make runner-plugin` + `claude plugin install`,
  which runs the same assembler a session runs. There is no checked-in plugin
  directory to drift, and the github-mode plugin is the authored text verbatim.
