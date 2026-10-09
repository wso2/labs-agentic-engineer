# Developer guide — specs from an existing codebase

`code-to-spec` is a developer skill (`.agents/skills/code-to-spec/`) for
recreating an application that already exists as code on AEP. It reads the
codebase and writes the part of the `specs/` tree that `/start` and
`/interview` would otherwise elicit from a human; `/design` then runs
unchanged. It is not a platform flow: nothing in the console offers it, and
the design agent cannot run it.

## What it produces

```
specs/requirements/prd.md, product-wide.md, features/F<n>-<slug>.md   per the bundled prd-contract
specs/requirements/sources/<module>.md                                 one coverage file per module and document
specs/design/domain-model.md                                           one erDiagram from the code's types
.inventory/*.md                                                        working notes, not part of the spec
```

It deliberately writes nothing else under `specs/design/`. The cell, the
components, roles, flows, contracts and acceptance criteria are derived by
`/design` from the requirements with the org catalog and the write gates,
so the recreate gets the platform's architecture rather than a translation
of the legacy one. What the legacy code *is* — endpoints, integrations,
authorization tables — is recorded in the coverage files as context.
Behaviour that must survive the rewrite is a requirements line; a rule left
only in a coverage file does not reach the build.

## Installing and running it

The skill directory is self-contained: it carries the two platform contracts
it follows under `references/`, so it needs no checkout of this repository.
Copy it to where Claude Code looks for personal skills and run it from any
directory:

```bash
cp -R .agents/skills/code-to-spec ~/.claude/skills/code-to-spec
```

```
/code-to-spec /path/to/digiops-finance/apps/allocation
/code-to-spec /path/to/single-app-repo ~/work/my-app-specs
```

The product root is the directory that *is* the product: a repository, or
one app inside a monorepo. Output defaults to `<name>-specs/` under the
current directory, and the skill refuses to write inside the product's own
repository. Someone working in this repository can point the output at
`playground/.projects/<name>` instead, which git ignores here, and run
`pnpm play playground/.projects/<name> design` to see what `/design` makes of
it before it enters a real project.

Expect a long run: six inventories over the whole codebase first, then the
feature cut, then the files. Review the result against the bundled
`references/prd-contract.md` before handing it over, with particular
attention to the `*assumed*` lines and the Open Questions — those are the
skill's inferences and the facts only the owning team holds.

**Confidential codebases.** Derived specs describe internal business
processes. Keep them in gitignored or private locations and never commit them
to this repository as fixtures or examples. The skill copies configuration
key names only, never values; check its output all the same.

## Getting the tree into an AEP project

The handoff travels with the skill: `.agents/skills/code-to-spec/references/handoff.md`
has the ordered steps. In short, create the project, let the kickoff `/start`
flush, close every spec tab so the room unloads, push the requirements and the
domain model to the project repository, reopen, then run `/design`. The room
writes the files back once in its own markdown escaping; that one-for-one diff
is normal.

## Keeping the bundled contracts current

`references/prd-contract.md` and `references/domain-model-shape.md` are
generated copies of `skills/prd-contract/SKILL.md` and step 3 of
`skills/design/SKILL.md`. When either platform skill changes, re-run

```bash
node .agents/skills/code-to-spec/scripts/sync-references.mjs
```

and commit the result. `make test` runs `scripts/sync-references.test.mjs`,
which fails while the copies are stale, and also fails if `SKILL.md` ever
names a path into this repository again, so the skill cannot quietly stop
being standalone.

## What this is not

- Not an import run on the platform. A source-repository input on project
  create and a run that clones it are later work, if the skill proves out.
- Not a change to `prd-contract`, `start` or `design`. The skill carries
  copies; the platform's skills stay the single source of the file shapes.
