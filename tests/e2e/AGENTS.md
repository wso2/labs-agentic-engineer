# AGENTS.md — tests/e2e

End-to-end tests of the console (`apps/console`) against the real local stack.

## What is here

- `acme-expenses-walk.sh`: the live walk of the running example, driven by
  `agent-browser` from bash; steps 1–2 (sign-in, New project). How to run:
  `README.md`. Add a step only once its screen is wired to aep-api; no
  placeholders for steps that are not.

- `ae-studio/`: the AE Studio dataplane scenario, fresh org to merged PR. It is a
  manual agent-browser run plus read-only check scripts; its checks are shell
  scripts, not Playwright specs.
  - `ae-studio/SCENARIO.md`: the steps, pass checks, run variables and Cloud
    deltas. Read it before any run.
  - `ae-studio/checks/`: one script per scenario phase (`p0-…` to `p10-…`),
    `evidence-grep.sh`, and the shared `lib.sh`. Run one with
    `RUN_DIR=<run folder> bash tests/e2e/ae-studio/checks/<script>.sh`. It prints
    `PASS|FAIL|SKIP <id> …` per check and exits 0 only if nothing failed.

## Who drives a run

- A Sonnet subagent drives every step it can.
- The user does the steps only a human can: GitHub org and PAT setup, the Google
  sign-in on Cloud, VPN, every "yes at run time", repo deletion.
- A failed check goes to an Opus subagent for root-cause diagnosis. A fix needs
  the user's yes unless the run's rulings (`Ruling:` lines in its ledger) record
  a standing authorization for that kind of fix; an agent-driven run acts only under those.

## Rules

- A check script is read-only: it never writes to the cluster, GitHub or the DB.
  The scenario names the few requests that carry a body and are refused by
  design; add no other.
- Secrets live only in `deployments/.env.e2e` (git-ignored) and in `0600` scratch
  files in the session scratchpad. Never on argv, in output, in a trace, in
  evidence, in a commit. `lib.sh` reads tokens into a `0600` header file with
  xtrace off; keep that when you add a check.
- `RUN_DIR` is a git-ignored evidence folder (usually
  `learning/<anything>/e2e-runs/<date>-<install>/`). It holds `run.env`, notes,
  transcripts and screenshots, never a token file, a HAR or the values file.
  `run.env` holds names, ids and token file paths, never a value.
- A check whose `run.env` value does not exist yet SKIPs (`need` in `lib.sh`).
  `STRICT=1` fails on a SKIP.
- Log greps match the JSON slog keys (`"msg":"turns.start"` with `"kind":"start"`),
  never `key=value`.
- After editing a script: `bash -n`, shellcheck v0.10.0 (`docker run --rm -v
  "$PWD:/mnt" -w /mnt koalaman/shellcheck:v0.10.0 -x tests/e2e/ae-studio/checks/*.sh`),
  and the leak test: run `bash -x` on a check against a stub run folder whose
  token file holds a random stub token; `grep -c -F -f <token file> <trace>` must
  print `0`.

## Walk conventions (`acme-expenses-walk.sh` and later walks)

- Run against the cluster from `deployments/` (`make dev-env` once, `make
  dev-update` after each source edit) — no mocked infra.
- Locate by role and accessible name (`find role … --name … --exact`); CSS only
  for an element with no role, with a comment saying why.
- Every check waits with a timeout and fails with `step N failed: <what>`.
- Anything that creates real resources (repositories, agent turns) stays behind
  `E2E_CREATE=1` and is cleaned up in the exit trap.
- Walk the flow by hand with agent-browser before scripting it.
