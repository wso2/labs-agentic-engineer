# AGENTS.md — scripts/

One-command dev tasks against a running local stack — anything that should be a
single command.

What lives here acts on **projects the platform created** — their BFF API and
their GitHub repos. Nothing that builds or runs the platform itself belongs here:
the local plane's lifecycle (`make dev-env` to install, `make dev-update` to
redeploy after a source edit) and the runner image build (`make build-runner`)
live in `deployments/scripts/`, driven by the root `Makefile`.

## Conventions

- Scripts are thin and idempotent; prefer wrapping the `Makefile` verbs over
  duplicating logic.
- A one-shot **repo migration** belongs here too (`migrate-issue-labels.mjs`
  relabels a project's issues onto the current label vocabulary). It must be
  idempotent and must print exactly what it would do under `--dry-run` — it acts
  on a real project's GitHub issues, and a human has to be able to read the plan
  before it runs.
