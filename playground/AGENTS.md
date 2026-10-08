# AGENTS.md — @aep/playground

The playground runs the real engineering agent and the real coding agent on a
plain project directory on this machine. It needs no git, no GitHub, no AEP
database and no cluster. `wire` builds and runs the app on this machine. Nothing
deploys. The engineering agent is the design agent's `/v1` edge and Turn socket,
booted in-process with the playground's adapters (`engine/agents-app.ts`).

`pnpm play --help` is the reference for each verb, flag and environment
variable. Do not copy that text into this file.

## Skill verify loop

Use this procedure for steps 1 and 3 of the `writing-skills` verify loop.

### Before the first run

- Docker mode is the default. It needs a running Docker daemon (`colima start`).
- `code` needs `go` on the PATH. Before the agent starts, it derives each
  dependency's `wiring` and `exposesAPI.auth` with aep-api's own derivation, as
  POST /build does (ADR-0003).
- If `aep-runner:dev` or `aep-runner-opencode:dev` is missing, the next `code`
  run builds both. The first build takes several minutes. It needs a
  GitHub token with the `read:packages` scope. To add that scope to your `gh`
  login, run `gh auth refresh -h github.com -s read:packages` one time.
- After that, the run does not build the images again. Run
  `FORCE=1 make build-runner` to build them again.
- `--host` runs the coding agent on your machine with bypassPermissions, not in
  a container. It uses your own `bal`, `go` and `claude` login, and the
  `agent-browser` that `@aep/playground` pins to the runner image's version.
  Use it only on a scratch project.
- `play` loads `deployments/.env` into its environment. Do not print that file.
  The coding run gets at most one credential variable:

  | invocation | credential |
  |---|---|
  | `code` (docker) | `AEP_CODING_ANTHROPIC_KEY`, else `ANTHROPIC_API_KEY` |
  | `code --host --api-key` | `AEP_CODING_ANTHROPIC_KEY`, else `ANTHROPIC_API_KEY` |
  | `code --host` | none: your own `claude login` |
  | any, with `AEP_MODEL_FORMAT` or `AEP_MODEL_BASE_URL` set | `AEP_MODEL_API_KEY` only |

### Select a fixture

Use the smallest project that loads the skill you change. The fixtures are in
`playground/.projects/`. That directory is gitignored, so these fixtures are
only on your machine. The tracked projects are the cases in
`evals/codegen/cases/`.

| fixture | contents | skills it exercises |
|---|---|---|
| `oc-hello-claude` | two Ballerina APIs (`GET /hello`, `GET /bye`); no database, no auth, no UI; approximately 3 min | `aep`, `ballerina`, `openapi-conventions` |
| `p4a-base` | the same specs as `oc-hello-claude`; never run | a clean source to copy for a new fixture |
| `expense-claims`, `onboarding-tracker` | realistic size; also `evals/codegen` cases | `react-webapp`, `mock-verification`, `thunder-authentication`; use them to confirm that a change holds at size |

Use a fixture that has `.aep-playground/undo/`, or a fixture that has no
component directories. A fixture that has component directories and no
`.aep-playground/undo/` is already built. `--restore` cannot reset it.

To make a fixture, copy `specs/` and `issues/` from the nearest project to a new
directory in `playground/.projects/`. Remove each `## Progress` and
`## Mock verification` section from the issues. Remove each dependency that
your skill does not need. Keep two components or more: two is the minimum that
exercises the working-set selection and the subagent fan-out.

A `web-application` adds a mock-verification round. A subagent starts the app in
mock mode and walks each wireframe flow with `agent-browser`. No fixture makes
this round short: a web-app run can take 40 minutes. In `--host` mode, the walk
binds a localhost port and drives a real browser on your machine. A killed run
can leave a `vite` process on the port. The next run then fails on
`--strictPort`.

### Run

```bash
pnpm --silent play /abs/path/to/project code --restore --yes > run.out 2>&1; echo "exit $?"
```

- Give an absolute path. A relative path resolves against pnpm's `INIT_CWD`. A
  project inside the checkout must be in `playground/.projects/`.
- Always use `--restore`. It returns the project to its state before the last
  `code` run: it removes the component directories that the run made. Each
  `code` run takes a new snapshot first. Thus one run without `--restore` makes
  the changed tree the new baseline.
- `--yes` gives the headless consent.
- A long run can be longer than your command timeout. Run it in the background
  and wait until it exits.

The run selects its own working set from `issues/`. It does not take an issue
number (ADR-0001). It also selects an `issues/<n>.md` that you write by hand.

### Examine the run

Near the end of `run.out` is the line `transcript: <runDir>`. The run directory is
`<project>/.aep-playground/runs/<run>/`, and `<run>` has the form
`<timestamp>-code`. To find an earlier run, list `<project>/.aep-playground/runs/`.
The names sort by time.

`run.out` has the step lines, with `[#N]` on each subagent line. After the step
lines, it has a merged pass under `── the run, merged ──`.

| what | where |
|---|---|
| each tool call, in order, with its duration | `pnpm --silent play <dir> log` (the newest run); `--run <run>` reads an earlier run |
| skills in the system prompt | `grep '\[skills\]' <runDir>/progress.ndjson`; the text is `.logs/prompt-appendix.md` |
| skills that the mirror withheld for this audience | `grep 'mirror withheld' <runDir>/progress.ndjson` |
| skills loaded on demand | the `Skill` lines in `play <dir> log` |
| outcome, tokens, end time | `grep run_settled <runDir>/progress.ndjson`; `costUsd` is null on a subscription token. The wall-clock time is from the first `ts` to this `ts` |
| the full session, subagents included | `.logs/runtime.log` |
| the transcript of a failed subagent | `agent-sessions/<agentId>/` (docker mode only) |

To compare two runs, put `play <dir> log --run <before>` next to
`play <dir> log`. Then compare the two `run_settled` lines. One run does not
prove a behavior: two runs of the same fixture can differ (for example, one
writes tests and one does not). For a claim about behavior, run two times or use
`evals/codegen`.

`play <dir> log` reads Claude Code runs only. For an OpenCode run, read
`progress.ndjson` and `.logs/runtime.log`.

### What a run reads live, and what needs a rebuild

| you change | it applies |
|---|---|
| `skills/**`, the `aep` overlay included | on the next run. Edit the repo's `skills/`, not `<project>/.claude/skills/`: each run writes that copy again |
| `runners/remote-worker/src/local.ts` | on the next run |
| another file in `runners/remote-worker/src/`, the runner Dockerfile | docker mode: after `FORCE=1 make build-runner`. Host mode: on the next run |
| `packages/bal-library-tool` | docker mode: after `make bal-library-tool`; a version bump also needs `FORCE=1 make build-runner`. Host mode: after the tool's `install-local.sh` |
| a model connection (`AEP_MODEL_*`) on an image built before it | after `FORCE=1 make build-runner` |

`MODE=local make workflow-skill` prints the `aep` skill that a playground run
reads.

### Spec-side skills

The coding run does not mirror a skill whose audience is design only. Tune it
with `requirements`, `design` or `tasks`. Load a skill that has no verb with
`pnpm play <dir> chat "/<skill> <message>"`.

A spec verb has no `--restore`. Before each run, copy the fixture to a new
directory in `playground/.projects/`. Add `--fresh` to start a new
conversation.

AI SDK DevTools records each engineering-agent call to
`playground/.devtools/generations.json`. To examine it, run
`npx @ai-sdk/devtools`.

### Repeated runs

For a scored comparison over more than one run, save the project with
`pnpm play <dir> eval-save <name>`. Then use `evals/codegen` (see its
`AGENTS.md`).

### Run the built app

```bash
pnpm play <dir> wire --role <name> --no-open   # prints READY <url>
```

`wire` builds each service from its Dockerfile and starts it with compose. Then
it serves the app as that role. To stop all of it, quit. Its state is in
`<project>/.aep-playground/wire/`. The design is
`design/decisions/ADR-0002-wired-mode.md`. The app's half of the contract is
`skills/react-webapp/references/mock-mode.md`.

## Maintain the playground

### Fidelity contract

The bytes that reach the model are the same as in production:

- the same server code path (the `/v1` edge and Turn socket, TurnStarter,
  TurnDesk, ThreadBook, snapshot layout and filter, write gates);
- the same instruction composition: the playground sends the line verbatim, and
  the design agent parses `/<command>` and composes it, as it does for the
  console;
- the same skills materialization;
- the same runner session options (`runners/remote-worker/src/runtime/claude/runtime.ts`);
- the same authored `aep` skill, assembled for `mode: "local"`.
  `skills/aep/overlays/local.md` replaces only the GitHub-shaped passages.
  `workflow_skill.test.ts` pins the shared text. It also asserts that neither
  mode gets the procedure of the other (remote-worker ADR-0004).

The project idea is in `specs/.agentic-engineer.toml`, the same descriptor that
aep-api commits. A turn snapshot removes dot-files. Thus the idea reaches a turn
only through the project lookup (`engine/tools-fake.ts` reads it, as
ae-studio-tools does in production), and the design agent puts it on the
`/start` turn.

### Divergences from production

| divergence | why | parity path |
|---|---|---|
| spec-turn snapshots exclude `issues/` | production spec turns never see tasks (they are in GitHub) | none needed: this is parity |
| MCP is the catalog stubs: the design tools are listed, every call answers "no platform catalog" | no cluster, no ae-studio-tools (`engine/tools-fake.ts`) | none locally; the pod's tools socket serves the org's catalog |
| no Room: a turn's edits stream back and the playground folds them to disk | no ae-collab locally; the folder is the source of truth | the pod joins the Room (`collab/local-room.ts`) |
| `/v1` admits the session's dev bearer secret (`kit/auth.ts`), on loopback | no Platform IdP locally | the pod's `idpAuthenticate` |
| `filesChangedExternally` follows the pod's rule (the last turn's snapshot differs), so it is set after any writing turn and resets with each session | the design agent keeps the last turn's facts in memory | the pod's behaviour, per pod lifetime |
| `code` derives `wiring` and `exposesAPI.auth` from the repo's resource-type manifests, not from the cluster's catalog | there is no cluster; the derivation code is production's (`services/aep-api/cmd/design-derive`, ADR-0003) | a cluster with other resource types derives differently |
| no CRT-annotation append, no lineage diffs in replans | platform resources and tags do not exist locally | edit by hand; replan stays file-based |
| issue `key` lineage is the constant `"local"`; no spec or design tags | no builds or tags locally | dedupe across replans still works |
| design and tasks gates are playground UX | production has no server gate on the spec paths | advisory only |
| an issue file has no status | the playground has no GitHub issue state, so each run derives "done" from the App Path (ADR-0001) | none needed |
| the coding agent runs in a throwaway `docker run` of the runner image, not a pod | no cluster; the image and the session options are production's | mandatory undo snapshot and first-run consent; `--host` has weaker parity (see Before the first run) |
| `wire` builds images and runs containers on your machine | no cluster, and nobody can click through an app that does not run | first-run consent per project; it never deploys (ADR-0002) |
| the workflow skill has no GitHub steps (issue files, no branch, no PR) | no remote | the `local.md` overlay, as in the fidelity contract |

### Where the rest is

`src/ports/` holds the swapped adapters (FsSpecWorkspace, FileConversationStore,
FsIssueStore). `src/engine/` holds session boot (`agents-app.ts`: the design
agent with the dev verifier, `tools-fake.ts`, the file store and the Turn
socket), the turn loop (`turn.ts`, `/v1`), Plan turns (`plan-turn.ts`, the Turn
socket), the project's thread (`thread.ts`) and the coding-run spawn.
Each TUI screen is also a headless verb in `src/commands.ts`. A project keeps
its playground state in `<project>/.aep-playground/`. The engineering agent
never sees a dot-directory.

Rationale that belongs to one file is a comment in that file: the crew pane
(`engine/crew-pane.ts`, `engine/crew-block.ts`), the transcript copy, the
credential rules and the SIGTERM teardown (`engine/coding-run.ts`), the model
connection (`kit/model-connection.ts`), the chat commands
(`tui/chat-commands.ts`).
