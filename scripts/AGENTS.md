# AGENTS.md — scripts/

One-command dev tasks against a running local stack — anything that should be a
single command.

What lives here acts on **projects the platform created** — their BFF API and
their GitHub repos. Nothing that builds or runs the platform itself belongs here:
the local plane's lifecycle (`setup`/`start`/`stop`/`teardown`) and the runner
image build live in `deployments/scripts/`, driven by the root `Makefile`.

## Conventions

- Scripts are thin and idempotent; prefer wrapping the `Makefile` verbs over
  duplicating logic.
