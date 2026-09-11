# ADR-0001 — One authored workflow skill, composed per mode

**Status:** Superseded by
[ADR-0004](ADR-0004-library-owned-workflow-skills.md) (2026-08-02) · originally
shipped 2026-07-29

> **What changed:** the skill moved to the repo-root library and the in-file
> `<!-- mode:… -->` markers became an anchored overlay
> (`skills/aep/overlays/local.md`). What did NOT change is why this ADR exists:
> local mode must never be a second copy of the platform's skill. The drift this
> document measured — a CORS rule that was never copied over, a filesystem
> boundary stricter on one side for no reason, 55 lines duplicated in a later
> edit — is the failure ADR-0004 still has to prevent, and its content findings
> below are all live in the trunk today. Read this one for the evidence; read
> ADR-0004 for the mechanism.

## Context

The coding agent runs in two places. The platform dispatches it into a pod with
a repo clone, an authenticated `gh`, a GitHub milestone to work and a pull
request to open. The playground (`@aep/playground`) runs the same agent on a
developer's plain project directory: issues are `issues/<n>.md` files, there is
no remote, and a run finishes by writing a progress note.

Everything else is supposed to be the same, and that "everything else" is most
of the skill: what a component's App Path is, the `workload.yaml` grammar,
endpoint visibility, the constraints (no required env vars, no stubs, never
hand-write lockfile checksums), `ClusterResourceType` rendering rules. The
playground exists so a developer can tune exactly that guidance without
standing up a cluster.

It was implemented as a **second plugin**: `plugin-local/`, containing a skill
named `aep-local`, a near-copy of `plugin/skills/aep/SKILL.md`. The copy
carried a note saying its shared sections were "shared VERBATIM" with `aep`,
and `playground/test/steer-parity.test.ts` pinned two of those sections
byte-for-byte.

That did not hold, and could not:

- Two of the three sections the note claimed were shared were **not** pinned by
  the test, and one (`ClusterResourceType` rules, ~55 lines) had been copied
  into the local file in a later, separate edit.
- `aep`'s deny-list forbade adding CORS middleware; `aep-local`'s did not
  mention it. Nothing about CORS is GitHub-specific — the rule simply never got
  copied over.
- `aep-local`'s filesystem-boundary rule was materially **stricter** than
  `aep`'s ("do not even list or probe paths outside the cwd" vs "don't touch
  other repos"), again for no mode-related reason.

So the playground was tuning a document that had already drifted from the one
production loads, which defeats its purpose. Every future skill edit needed two
edits, and the drift was invisible unless someone happened to extend the parity
test to the section they touched.

## Decision

**One authored skill file**, `plugin/skills/aep/SKILL.md`, with the
mode-specific regions marked in place. Markers work at two scales, same syntax —
block, when the marker is alone on its line:

```markdown
<!-- mode:github -->
Ask the **issues API**, live, once per pick:
`gh issue list --milestone "<title>" --state open …`
<!-- /mode -->
<!-- mode:local -->
List every `issues/<n>.md` under `issues/`. Each is markdown with YAML
frontmatter …
<!-- /mode -->
```

and inline, when it has content beside it and closes on the same line:

```markdown
git commit -m "<type>: <short summary> (#<number>)"
<!-- mode:github -->git push -u origin HEAD<!-- /mode -->
```

`lib/skill_compose.ts` strips the regions that don't apply and writes the
resolved plugin to a scratch dir, which is what the SDK session loads.
`plugin-local/` and the `aep-local` skill are deleted; both modes load the
plugin named `aep` and allow the skill named `aep:aep` (which the agent then
invokes — allowing a skill does not load its body; see ADR-0003).

**The platform text is the trunk.** A `mode:github` wrapper is a cost, not a
neutral choice: it means the passage has a local twin to keep in step. So the
default is unwrapped shared text, and a region is gated only when the ungated
version would make one mode attempt something it cannot do. Reading a paragraph
that is merely inert (a visibility table, a convention that won't come up) is far
cheaper than maintaining a second copy of it.

The metric that matters is **paired** regions — two variants of one passage,
which a human must edit in lockstep. Lone regions (one mode adds a step, or
omits one) cost nothing to keep in step because there is no twin. The first
merge had 17 paired regions; an audit of all 18 conditional regions found only 7
were genuine mechanism differences, and the thinning pass that followed brought
paired regions to 7 while local mode *gained* three sections it had been missing
(the API-contract procedure, the whole external-dependency web-research section
including its prompt-injection and secret-in-query rules, and the
install-outside-the-package-manager rule). `skill_compose.test.ts` caps the
paired count so the ratchet cannot slip back.

Inline markers are what made that possible: without them, varying two words
(`git push`, "sole git writer") forces two copies of the surrounding paragraph —
the same duplication at smaller scale. A template engine (Handlebars, Mustache)
was considered for this and rejected: their default treatment of an unknown
variable is falsy, so `{{#if lcoal}}` would silently drop a region from *both*
outputs and fail nothing. Handlebars' `strict: true` fixes that, but as opt-in
config someone can drop, and the whole point of (7) below is that markup
mistakes must be loud.

Consequences of the shape, in the order they were decided:

1. **Composition happens in code, not in prose.** The alternative was one file
   telling the agent "if you are running locally, do X" and letting it work out
   which branch applies. The runner already knows unambiguously; making the
   model re-derive it every run adds a failure mode for no gain.
2. **At runtime, not as checked-in codegen.** A `make gen` step that emits two
   static files is only correct if someone remembers to run it — and the
   playground is run with `tsx` directly, bypassing any build. Composing per run
   cannot go stale, and it keeps the dev flow's bind-mounted plugin dir
   (`setup-k3d.sh`) live-editable.
3. **Markers in place, not partial files.** Each delta is a clause, a paragraph
   or a section. Split across `partials/*.md` they would be individually tidier
   and collectively unreadable — nobody could review the workflow end to end.
4. **Mode is stated, never inferred.** `BaseAgentConfig.mode`, set by the
   entrypoint. Deriving it from an empty `repoUrl` or an absent MCP token would
   couple the agent's whole procedure to a signal that could change meaning for
   an unrelated reason. Default is `github` because that is the safe direction:
   a local run composed for GitHub dies on its first `gh` call, whereas a
   production run composed for local would be told there is no remote and no PR
   to open, and would quietly finish having landed nothing.
5. **Both modes go through the strip step.** Markers are HTML comments —
   invisible when rendered, perfectly readable to a model. There is no "raw"
   fast path for production; an unstripped `mode:local` block in a production
   session is exactly the failure in (4).
6. **Composing is a single choke point inside `startCodingRun`.** Entrypoints
   pass a mode, not a composed directory, so a new caller cannot forget to
   compose and hand the SDK the authored source with both procedures in it.
7. **Malformed markup fails the run.** Unknown mode name, nested block,
   unclosed block, stray close — all throw at startup. The composed body is what
   steers the agent; a markup slip must not silently ship half a procedure.

Content bugs the merge and audit exposed were fixed rather than preserved as mode
differences, because none of them was one:

- The CORS deny-list rule is now shared (it was github-only).
- The stricter filesystem-boundary rule (previously local-only) now applies to
  both. Production's rule got stricter as a result — deliberate; a production pod
  has no more business probing outside its workspace than a developer's machine
  does.
- The github deny-list defined a ledger issue as "an issue in the milestone with
  no labels", contradicting Discovery, which defines it as any open milestone
  issue *without* the `aep` label and says it "may carry their own labels like
  `bug`". As written the deny-list permitted working a human's `bug`-labelled
  ledger issue. Now points at Discovery's definition.
- The `aep` skill's own frontmatter description told a Task-tool subagent, in its
  skill catalog, to "open ONE pull request" and "run `git` and `gh` normally" —
  directly contradicting the body's rule that a subagent never runs either. The
  description is now one shared, mode-neutral summary, which removes the
  contradiction as well as the duplication. It also means the authored file is a
  valid skill document on its own, rather than one whose `description:` only
  exists post-composition.

Section numbers (`## 1. Discovery`, "re-list (§1)") were dropped in favour of
names, and the at-a-glance overview was un-numbered into bullets. The two modes
have different step counts, so numbered headings and a numbered overview would
have forced every heading, every cross-reference, and the whole overview list to
become mode-conditional — the numbering was accidental coupling, not content.

## Consequences

- Editing the workflow is one edit. Editing a *project convention* is one edit
  that provably lands in both modes.
- The playground now exercises materially more of the platform's guidance than it
  did as a separate skill. Of the 507 lines a local run reads, 447 (88%) are the
  same text a production run reads; only 60 are local-specific. Three sections it
  silently lacked are now in both.
- The playground's `aep ⇄ aep-local` section-parity test is gone: sharing is
  structural now. What replaces it (`skill_compose.test.ts`) guards the opposite
  mistake — wrapping text that should be shared in a mode block — by asserting
  the shared sections are byte-identical across both composed outputs, and that
  neither mode's output contains the other's landmarks.
- There is no checked-in copy of the composed output to keep in sync. The tests
  assert properties (no marker leaks, exactly one `description:`, per-mode
  landmarks present/absent, shared sections identical), because a golden fixture
  would reintroduce the duplicate-copy problem this ADR removes.
- ~200K is copied per run. Immaterial next to a coding session, and it is what
  makes bind-mounted live skill editing work.
- The coding-run prompt still exists twice, in two languages: the platform's is
  built in Go (`delivery/codingagent/coding_executor.go`), the playground's in
  `local.ts`. That cannot be collapsed into one function, so the clause naming
  the skill is pinned in both by `playground/test/steer-parity.test.ts`.
