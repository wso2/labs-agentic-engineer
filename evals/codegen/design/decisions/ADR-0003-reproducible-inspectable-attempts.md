# ADR-0003: Reproducible, inspectable attempts

**Status:** Accepted (2026-10-05)

## Context

A delta between two sweeps is only readable if nothing but the thing under test
changed between them, and a low score is only useful if someone can find out
why. The harness's own agents, the browser tool and the machine can all drift,
and an attempt produces far more data than a root-cause pass needs. One walk
showed the risk: a wedged headless Chrome made six commands in a row time out,
and the walker then marked every remaining item failed.

## Decision

**Pin what the harness itself uses.**

- The planner, walker and judge models are pinned in `config.ts`
  (`MODELS`), not left to the SDK default. The model under test is a
  `configs.yaml` entry.
- The walker's `agent-browser` is this package's devDependency, put first on the
  walk's PATH. A test (`test/harness.test.ts`) holds its version equal to the
  runner image's, so both walks use the same tool.

**Keep the sweep shape honest.** Concurrency defaults to 1: an attempt is a
coding container, a compose project and a browser, and parallel attempts on a
laptop measure the laptop. Repeats default to 1; three is the floor for
believing a delta, and the report prints `inconclusive` for a delta inside the
spreads or with n=1.

**Record provenance.** Each attempt writes `provenance.json`: HEAD sha and
branch, uncommitted paths under `skills/`, `runners/remote-worker/`,
`playground/` and `evals/codegen/`, the runner image ID, and every model. It also
writes `skills.diff` (tracked changes plus untracked files under `skills/`) and a
copy of the checklist as it stood when the attempt started.

**Archive everything a root-cause pass could read, minus what is regenerable or
secret.** The archive keeps the generated project, the coding transcripts and
run dir, the walk with screenshots, the wire logs and every compose service's
stdout (captured before teardown removes the containers). `project/` leaves out:

- `node_modules` anywhere (reinstallable);
- build output (`target/`, `dist/`) directly at an App Path's root (regenerable
  from the sources beside it);
- `wire`'s secrets, bearers and private key;
- the undo snapshots (the case already is that state) and the coding run dirs
  (archived whole under `coding/`).

A case leaves out compiled renderings (`*.excalidraw`, `*.gen.json`), so no
attempt carries them. Teardown removes the compose volumes and the images
compose built for the attempt (`--rmi local`); pulled images stay.

**Sandbox the walker.** The walker uses the app only through its UI:

- Bash runs exactly one `agent-browser` command: shell metacharacters are
  refused.
- Verbs that act behind the UI or outside the isolated session (`eval`,
  `webmcp`, `chat`, `connect` and others) are refused as the first argument.
  `network route`/`unroute` are refused because answering the app's own
  requests would make every item pass. Session, profile and `--all` flags are
  refused anywhere on the line.
- Navigation is held to `localhost,127.0.0.1` through `--allowed-domains`.
- Read and Write stay inside the attempt's `walk/` directory.
- One browser command runs at a time (`BrowserLane`): overlapping commands race
  on the one page. A `batch` is checked command by command and must use
  `--bail`, so a failed command does not send later keys to the wrong element.
- `BrowserWatchdog` stops the walk as a harness error after three consecutive
  `agent-browser` timeouts with nothing completing between them.

## Consequences

- Changing a harness model or the `agent-browser` version is a deliberate
  edit, and a baseline from before it is not comparable.
- A dirty tree is visible in every attempt's provenance, not hidden in the score.
- Archives stay small enough to keep across many cases and repeats, but a
  root-cause pass must rebuild to inspect build output or dependencies.
- An unresponsive browser costs about six minutes at the usual 120 s timeout
  and is charged to the environment, not the app.
- A new `agent-browser` verb that reaches behind the UI must be added to the
  forbidden list when the pinned version is raised.
