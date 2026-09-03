# AGENTS.md — @aep/playground

Root-level local-filesystem playground: runs the **real** engineering agent
(in-process `createApp` boot) and the **real** coding agent (remote-worker
`local.ts`) against a plain project directory. Purpose: edit a `SKILL.md`, a
prompt, or a steer copy → rerun one phase → observe. No git, no GitHub, no
Postgres, no cluster.

The coding agent loads the **same `aep` skill production loads**, assembled for
`mode: "local"` — one authored `skills/aep/SKILL.md`, with the GitHub-shaped
passages swapped by the anchored edits in `skills/aep/overlays/local.md`, so
tuning it here IS tuning the platform's. See
`runners/remote-worker/design/decisions/ADR-0004-library-owned-workflow-skills.md`.

## Run

```
pnpm play                              # usage help (bare invocation)
pnpm play menu                         # picker → phase menu
pnpm play <dir>                        # chat home (dir created if missing; /menu for the dashboard)
pnpm play <dir> requirements --idea "…"
pnpm play <dir> design | tasks | check | undo
pnpm play <dir> code [--restore] [--yes]   # ONE coding-agent session works the
                                            # whole project — no per-issue run
pnpm play help | -h | --help           # same usage help
```

## Tuning the coding run

`pnpm play <dir> code --restore --yes` is the edit → rerun loop: `--restore`
rolls the project back to the snapshot taken before the last coding run, which
removes the generated component directories and reverts the `## Progress`
sections on the issues, so the next run starts from the same state the last one
did. One command, no manual cleanup, and the comparison is honest.

**Tune against a deliberately minimal project, not a realistic one.** Two
components is the floor that still exercises what matters — the working-set
derivation and the subagent fan-out — and the components themselves should be
trivial: one endpoint, one screen, no database, no auth, no platform resources.
A realistic project spends most of its wall clock on work that is the same every
time you rerun it, which is exactly the part you are not tuning. Measured on this
repo: a two-component todo app with Postgres and OIDC runs ~12-16 min, while the
same shape reduced to `GET /hello` plus one screen runs in a few minutes and
halves the attached skill set (3 skills against 6). No attached skill is
preloaded — the run loads what it reaches for — so a smaller fixture buys
fewer loads and less work, not a smaller system prompt.

Keep the small one around and rerun it; reach for a realistic project only to
confirm a change holds at size. Since `playground/.projects/` is gitignored,
a fixture like that is yours alone — author it by copying the `specs/` +
`issues/` shape of an existing project and stripping every dependency.

**A `web-application` costs an extra round.** Its build is followed by mock
verification: a subagent starts the app with `npm run dev:mock`, walks its
stories with `agent-browser`, and reports a verdict per story, which the lead
fixes and re-verifies (`skills/mock-verification`, and `react-webapp`'s
`references/mock-mode.md` for the mock itself). In docker mode that is all
inside the container. **In `--host` mode it is your machine**: the run binds a
localhost port and drives a real browser under bypassPermissions. Nothing is
installed — `agent-browser` and the browser are resolved off your `PATH`, the
same way `playwright-cli` already is — but a run that dies badly can leave a
`vite` process holding its port, and the next run's `--strictPort` will say so
rather than quietly reading the old server.

That is the one part of the loop with no minimal fixture: a web app small enough
to be fast still has screens to walk. Budget for it, or tune API-only changes on
a project with no `web-application` in it.

Relative `<dir>` paths resolve against pnpm's `INIT_CWD`, so pass an absolute
path from a script or a shell whose cwd you have not changed.

`play <dir>` drops straight into **chat** — the home surface. A directly-named
dir is created after a prompt (headless refuses a missing dir rather than
creating silently), and a NEWLY created project captures its idea right there —
from `--idea`, or by asking once. In chat, slash commands drive every phase
without leaving: `/start [idea]` kicks the project off (interview →
requirements); `/spec` `/design` `/<skill>` load a working-tree skill and follow
it (a flow turn, the same channel the plan turn uses); `/task` `/code` `/validate`
`/undo` run the existing phase engines; `/menu` opens the status dashboard,
`/help` the guide.

The idea lives in `<project>/specs/.agentic-engineer.toml` — the **project
descriptor**, identical to what aep-api commits on project create. It marks the
directory as an Agentic Engineer project and carries the idea `/start` builds
requirements from. Being dot-prefixed it is stripped from every turn snapshot,
so the agent can never read it: the idea reaches a turn ONLY as a FACT on the
turn spec (`engine/turn-spec.ts`'s `startSpec`), exactly as aep-api attaches it
in production. The wording it becomes is the agents service's
(`services/agents/src/prompts/turn.ts`).

`code` mirrors prod's milestone cycle (ADR-0011): the CLI never picks an issue
or an order — the `aep` skill discovers its own working set from
`issues/` (does each issue's App Path already look done?), orders it, and
works as many as it reasonably can in one session, fanning independent ones
out to subagents. See `design/decisions/ADR-0001-milestone-batch-coding-run.md`.

No review/browse affordances: the playground auto-writes files and the user's
editor (VS Code) is where browsing, diffs, and hand-edits happen — including
authoring `issues/<n>.md` by hand (picked up automatically).

Flags: `--idea`, `--target`, `--fresh` (rotate the general conversation),
`--silent`, `--restore`, `--yes` (headless coding consent), `--host` +
`--api-key` (coding-run mode and its auth). Every verb exits nonzero on failure
— the edit-skill → rerun loop is scriptable.

Relative project paths resolve against where you launched `pnpm play` (pnpm's
`INIT_CWD`). The picker's default is `<repo>/playground/.projects/my-app` —
`playground/.projects/` is the ONE place inside the checkout where projects
may live (a gitignored dot-dir, invisible to lint + license gates). Anywhere
else inside the repo is refused.

Requires `ANTHROPIC_API_KEY` (env or `deployments/.env`) for the **engineering**
agent, which is an AI SDK model call with no other way to authenticate. The
**coding** agent is a Claude Code session and authenticates by mode: a docker run
gets the key (a container reaches no credential store), while `--host` withholds
it and lets the SDK use the developer's own credentials — the ones `claude login`
wrote — so a local tuning loop bills your subscription, not the platform's key.
`code --host --api-key` opts back into key auth.

`AEP_CODING_ANTHROPIC_KEY` bills **coding** runs to a separate credential — the
local half of the platform's per-org coding-agent key (ADR-0016). It takes
either a Console API key (`sk-ant-api…`) or a `claude setup-token` OAuth token
(`sk-ant-oat…`, which bills a Claude subscription instead of API credits), and
the prefix decides which. Either credential satisfies docker mode's pre-flight.

It changes WHICH credential is used, not WHETHER one is:

| invocation | credential |
|---|---|
| `code` (docker) | `AEP_CODING_ANTHROPIC_KEY`, else `ANTHROPIC_API_KEY` |
| `code --host --api-key` | `AEP_CODING_ANTHROPIC_KEY`, else `ANTHROPIC_API_KEY` |
| `code --host` | none — your own `claude login` |

Docker needs no flag because a container reaches no keychain to fall back to.
Host mode still requires `--api-key`: defining a variable is not the same act as
asking this run to authenticate with it, and a bypassPermissions process on your
own filesystem should not pick up a shared credential because a file elsewhere
happened to define one.

An API key arrives as `ANTHROPIC_API_KEY`, an OAuth token as
`CLAUDE_CODE_OAUTH_TOKEN` — and the run gets **exactly one of them**, same as in
production. That exclusivity is load-bearing: Claude Code ranks
`ANTHROPIC_API_KEY` above `CLAUDE_CODE_OAUTH_TOKEN`, so a run holding both would
authenticate with the API key and ignore the token silently.

Skills load from the
working-tree `skills/` on EVERY turn — edits apply next run, no rebuild. That now
covers the coding run's own workflow skill and its local-mode overlay too: the
library is mounted over the image's `/app/skills`, so `aep/SKILL.md` and
`aep/overlays/local.md` are live-editable exactly like a stack skill.

**`bal library` is live-editable on the same terms — in docker mode.** The
`ballerina` skill drives that one tool by name, and a skill is only as good as
what the tool it names returns, so tuning the two has to be one loop:

```bash
make bal-library-tool   # ~3s; builds the jar the next run mounts
pnpm play <dir> code --yes --restore
```

Docker mode mounts that jar over the one the image INSTALLED, so it needs no
rebuild; the baked copy — compiled by the runner image's own first stage from
`packages/bal-library-tool` (ADR-0008) — is what a cluster run gets. Every run
that overlays it says so, naming the jar. The version the mount targets is read
from the tool's `gradle.properties`, which is what the image's build derives it
from; bumping it without `make build-runner FORCE=1` aims the mount at a path
that image does not have.

Host mode cannot overlay anything: `bal library` is a `bal` tool, resolved out of
**your own** `~/.ballerina`, so the loop there is the tool's `install-local.sh`.
Rather than write into your home behind your back, a host run tells you when your
working-tree jar was built after the installed one landed — by mtime, because a
gradle jar is not byte-reproducible and a content comparison would call every
unchanged rebuild stale — or when the tool is absent, in which case the skill
falls through to `code-rules.md`, the same branch a stale image produces and the
reason the skill documents it.

**AI SDK DevTools is always on** (`src/devtools-default.ts`): every
engineering-agent LLM call — the composed prompt, tool calls, usage, timing —
is captured to `playground/.devtools/generations.json` (gitignored). Inspect
with `npx @ai-sdk/devtools` (port 4983). Opt out per run with
`AGENT_DEVTOOLS=false pnpm play …`. The coding agent is an Agent SDK session,
not an AI SDK model — its full transcript is the run's
`.aep-playground/runs/<ts>/…/claude.log` instead. Beside it,
`agent-sessions/` is the SDK's own per-session scratch, redirected there with
`CLAUDE_CODE_TMPDIR` so a stalled subagent's diagnostic file survives
`docker run --rm` (rationale inline in `engine/coding-run.ts`) — docker mode only,
and never pre-created on the host: the CLI refuses a temp dir it does not own.

## Fidelity contract

The bytes reaching the model are production-identical: the same server code
path (auth middleware, TurnGuard, workspace shape, snapshot filter, write
gates), the same instruction composition (the playground sends a `TurnSpec` and
the agents service composes it, exactly as it does for aep-api — there is one
composer, so there is nothing to drift), the same skills materialization,
the same runner session options (`resolveBaseAgentConfig` defaults are
unit-pinned in remote-worker), and and the same authored workflow skill
out of the same library (only the GitHub-shaped passages are swapped, by
`skills/aep/overlays/local.md` — `workflow_skill.test.ts` pins which text is shared
and asserts neither mode leaks the other's procedure; ADR-0004 in
remote-worker).

## Scope ends when the code lands

The playground covers requirements → design → tasks → code, ending when the
coding-agent session stops (some issues may stay open — a later run picks them
up, same as prod). There is NO build/deploy half and none should be
added: no image builds, no `docker build` of the authored Dockerfile, no
deploy attempt, no validation-task runs. Two things that look build-ish stay
deliberately — the agent's local toolchain verification (`go build`,
`tsc --noEmit`; code quality, not a platform build) and `workload.yaml` +
`Dockerfile` authoring (the component's shape; what you hone here must stay
platform-ready). Taking a project further is a HANDOFF, not a playground
feature: the project is a plain directory with production-layout `specs/` —
push it to a repo and let the platform's normal flow build/deploy it.

## Documented divergences from production (do not mistake for platform behavior)

| Divergence | Why | Parity path |
|---|---|---|
| `issues/` excluded from spec-turn snapshots | production spec turns never see tasks (they live in GitHub) | n/a — this IS parity in effect |
| MCP off by default | no cluster; avoids a localhost mint attempt per turn | run aep-api locally + AEP_MCP_URL (its MCP resolver) |
| No CRT-annotation append, no lineage diffs in replans | platform resources/tags don't exist locally | manual edit; replan is still files-based |
| Issue `key` lineage constant `"local"`; no spec/design tags | no builds/tags locally | dedupe across replans still works |
| Design/tasks gates are playground-side UX | production has no server gate on the console's spec paths | advisory only |
| No status field on an issue file | prod's own `derivedStatus` is read from GitHub issue state, never cached; the playground has no such oracle, so it re-derives "is this done" from whether the App Path looks implemented, every run | none needed — deleting a component's code puts its issue back in the working set |
| Coding agent runs in a throwaway `docker run` of the runner image, not a pod | no cluster; the image and the session options are production's | mandatory undo snapshot + first-run consent. `--host` opts out of the container entirely and runs bypassPermissions ON THE HOST against the developer's own toolchain **and the developer's own Claude credentials** — weaker parity, so point it at scratch/git-tracked projects. A project with a `web-application` widens that further: verifying one binds a localhost port and drives a real browser on your machine (see below) |
| No GitHub-shaped steps in the workflow skill (issue files, no branch, no PR) | there is no remote to discover issues from or open a PR against | the deliberate one: the same authored `aep` skill, assembled for `mode: "local"`; only the passages `skills/aep/overlays/local.md` anchors are swapped, everything else is production's text verbatim |

## Layout

`src/ports/` — the swapped adapters (FsSpecWorkspace, FileConversationStore,
FsIssueStore). `src/engine/` — session boot, the §5 turn loop, instruction
composition, coding-run spawn. `src/tui/` — clack screens; every screen is
also a headless verb in `src/commands.ts`. `test/` — mock-model phase tests
(no tokens) + the parity pins.

A playground project keeps its state in `<project>/.aep-playground/`
(conversations, runs, undo snapshots, project.json) — a dot-dir, so
engineering-agent turns never see it. The project's idea is NOT there: it lives
in `specs/.agentic-engineer.toml`, which is project content committed in
production, not playground-local state.
