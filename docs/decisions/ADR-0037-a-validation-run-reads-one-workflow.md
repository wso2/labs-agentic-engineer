# ADR-0037 — A validation run reads one workflow, and a coding run never sees it

**Status:** Accepted · 2026-09-25 · **Amends** the always-on clause of the
runner's [ADR-0005](../../runners/remote-worker/design/decisions/ADR-0005-the-workflow-rides-the-project-mirror.md).

A validation run preloaded two workflow bodies: `aep` and `acceptance-run`. A
callout in `aep` said the validation skill "REPLACES **The run** below …
Everything else here still applies." Everything else was written for a coding
run, and on the points that matter it contradicted the validation procedure:

| `aep` said | the validation run must |
|---|---|
| never touch a `validation` issue | work exactly that issue, and comment on it |
| a PR with no `Resolves #N` will not merge | say `Validates #N`, never a closing keyword |
| force-push only during a conflict rebase | push its reused branch with `--force-with-lease` every cycle |
| the prompt names a milestone | read an issue; the prompt names no milestone |

The agent had to arbitrate between two procedures, and a rule that loses an
arbitration is a rule the run does not follow — #137/#140 were the force-push
row. It also carried about 25 KB of fan-out, branch-identity and `.gitignore`
procedure on every turn. The rails worth keeping (specs are read-only, nothing is
authored outside the project, no secret in a search) lived in
`aep/references/component-contract.md`, which a validation run is never handed.

## Decisions

1. **One workflow per task kind.** `alwaysOnSkills` returns `["aep"]` for a
   coding run and `["validation-task"]` for a validation run. `aep` carries no
   validation text at all.
2. **The skill is renamed `validation-task`.** Once it holds the whole run —
   workspace facts, rails, landing — "running the acceptance criteria" names a
   part of it. *Validation task* is the glossary's word for the issue this run
   works, so the name reuses a defined term rather than coining one.
3. **The shared rails are restated, not shared.** Git authentication, the
   git/GitHub never-list, secrets in searches and fetched pages appear in both
   skills. The restatement is deliberate: the validation versions are
   different rules (the table above), so a shared third skill would need
   per-run exceptions and add a layer to save about fifteen lines.
4. **A coding run's allowlist drops `validation-task`.** `audience` says only
   `design` or `coding`, and both task kinds read the one project mirror, so
   the skill was in every coding run's catalog as a procedure the run could
   load. `implementationSkills` filters it out of the whole-mirror allowlist.
5. **The prompt and the issue body carry no procedure.** The validation prompt
   names the issue and defers to the skill, as the coding prompt does. The
   issue body lists the task's inputs and nothing the skill states: the report
   checker, the summary comment and the `Validates #N` contract are gone from
   it. A body is never rewritten on reopen, so a copy there outlives any change
   to the skill.

## Rejected

- **A `validation` audience value.** It models the constraint in metadata and
  would let a future validation skill work without a runner edit, but it
  changes the BFF's mirror rule, ADR-0014 and the console for one skill.
  Revisit when there is a second one.
- **Keeping `aep` and fixing the contradictions in place.** Every rule would
  need a validation exception, and the agent would still read a coding
  procedure it must not run.

## Consequences

- A validation issue filed before this change and reopened for a later attempt
  still names `acceptance-run/scripts/check-report.mjs` in its body. The
  preloaded skill names the right path, and a wrong one fails loudly.
- Org skill repos drop `acceptance-run` on the next reconcile:
  `reconcileEmbedded` purges a platform skill the library no longer ships.
