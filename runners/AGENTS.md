# AGENTS.md — runners/

One-shot / job images (not long-lived services). Run to completion in a pod.

**Status:** `remote-worker/` is the coding-agent runner — a TS Claude Agent SDK
one-shot pod that provisions a workspace and runs the Agent SDK against it.
Skills are **authored in `<repo>/skills/`, not here** (`skills/AGENTS.md` has
the authoring rules) and **delivered by the BFF, not here either**: a run reads
the `.claude/skills/` mirror in its own clone. What this package owns is
consuming that mirror correctly — the always-on workflow, the allowlist, and the
playground's stand-in for the BFF write. The dev flow bind-mounts the library
into the runner pod at `/app/skills` for live skill edits (see
`deployments/scripts/setup-k3d.sh`), which is what the playground mirrors from.

## Conventions

- One entry point per pod (`src/oneshot.ts`); everything reachable from it.
- **Never put a credential in a git URL or in argv.** All clones go through
  `lib/git_clone.ts` — an authenticated URL leaks into `child_process` error
  messages (which the BFF forwards to the console build log), into `ps`, and into
  `.git/config`. Rationale inline in `git_clone.ts`; the BFF keeps a shape-based
  second line of defense in `delivery/codingagent/redact.go`.
- **Git credentials — two modes, one helper value in `.git/config`.**
  When `GITHUB_TOKEN` / `GH_TOKEN` is set (cloud Jobs mount the org PAT),
  `workspace.ts` installs `gh auth git-credential` (the same helper
  `gh auth setup-git` uses), pinned to the **real** `gh` absolute path so the
  `.aep/gh` wrapper cannot intercept. Clone and push share that path; they do
  **not** call `credentials/refresh`. When those env vars are absent, every
  authenticated git op goes through `lib/credhelper.ts` → refresh (clone via
  `git -c`, then the same script installed durably). No GIT_ASKPASS, no token
  in argv or URL. Don't add a third path. Changes to the generated refresh
  scripts must keep `credhelper.test.ts` green — it drives them with real `git`.
- Runner `console.*` is a **user-facing** channel, and it shares the file
  descriptor the NDJSON progress feed writes to. `installConsoleScrubber()` at
  each entry point converts every call into a scrubbed `log` progress event, so
  the feed stays parseable NDJSON end to end; don't bypass it by writing to
  `process.stdout` directly. The BFF still wraps any non-NDJSON pod line into a
  build-log event, but that is now a safety net, not the normal path.
- **The progress contract is `lib/progress/schema.ts`, and it moves with three
  mirrors**: `contracts/progress.go`, the three progress schemas in
  `packages/contracts/api/v1/openapi.yaml` (contract-first — then `make gen-api`),
  and the WORDING, which is nobody's here: `@aep/progress-view` renders every
  line for both the console and the playground, so an event without a case there
  reaches a user as a blank row or a raw field dump. Decisions and the SDK's
  measured capabilities are in
  `remote-worker/design/decisions/ADR-0002-run-observability.md`; read it before
  changing what a line says, because several of its entries are corrections of
  the obvious-looking choice.
- **API retries are on the feed for every run; the rest of the diagnostics are
  developer-only files.** A stalled model turn used to be reported as bare
  silence. The SDK emits `system`/`api_retry` for every retryable failure and
  `from-sdk.ts` was discarding it, so `progress/diagnostics.ts` reads it into a
  `warn` line and the watchdog names it in its own. Ungated on purpose: a healthy
  run emits nothing, the `error` field is a closed enum (no prompt or credential
  can ride it into a console build log), and overload is load-dependent so a flag
  would be off during every incident. **A retry must never reach
  `watchdog.observe`** — it is the absence of progress, and resetting the idle
  clock hides the stall it explains. `debugFile`, `stderr` and
  `includePartialMessages` are the opposite call: on for every playground run,
  off in a pod unless `AEP_RUNNER_DEBUG=1`, and they write files beside
  `claude.log` rather than to the feed — nothing collects a pod's files and the
  debug log holds prompt text. Streaming frames reach neither the feed nor
  `claude.log`. ADR-0002 decisions 14–15 have the measurements, including why
  stderr is *not* where retry detail lives.
- **Fan-out runs in the foreground.** A `PreToolUse` hook
  (`lib/fanout_foreground.ts`) forces `run_in_background: false` on every
  `Agent`/`Task` call that did not already say so. Backgrounding does not add
  concurrency — several fan-out calls in one turn is what does — and it detaches
  the subagent, so the SDK forwards none of its messages, a whole component's work
  reaches the feed as an empty section, and the session can finish while its
  children are still running (it did: `result: success` with one component
  stubbed and one missing). **Background is the SDK default**, so the hook keys on
  the flag's absence, not on `true`; the omitted-flag test is the regression pin.
  Rationale inline in the module; ADR-0002 decision 13 has the measurements.
- **Authored files land in the project.** `lib/workspace_guard.ts` is a
  `PreToolUse` hook that denies `Write`/`Edit`/`NotebookEdit` outside the
  workspace, and `promptWithProjectRoot` (`lib/runner.ts`) states the absolute
  root in the prompt — the runner is the only layer that knows it, since the two
  prompt builders sit either side of a language boundary and the path is decided
  after `provisionWorkspace`. Both exist because a run inferred that the
  skills-plugin directory's parent was the project root and built a whole
  component there, green. Writes are allowed outside the project in exactly two
  trees: the temp directory, and **any dot-directory under `$HOME`** — one rule,
  because every toolchain hides its cache in one (`.ballerina`, `.npm`, `.m2`,
  `.cargo`) and this module has no business tracking which stacks the image
  ships. The earlier version listed three, which silently contradicts the next
  stack skill added. A visible directory under `$HOME` stays denied, so a sibling
  checkout is still caught. Reads are deliberately NOT gated: a skill's
  `references/` live outside the project by construction, and the agent must be
  able to read the toolchain's own installation for a library's real signature.
  Bash is not gated either; a build writes where it writes, and the pod is the
  containment boundary. The guard catches the one expensive mistake, it is not a
  sandbox.
- **`allowedTools` restricts nothing here.** `bypassPermissions` +
  `allowDangerouslySkipPermissions` allow every harness tool regardless, so
  `BASE_ALLOWED_TOOLS` documents intent while `DISALLOWED_TOOLS` is the boundary
  that holds. Keep the harness's session-management surface (schedulers, task
  channels, interactive prompts) in the deny list: a one-shot pod has no user and
  no next session, and a reachable-but-useless tool is somewhere a run will spend
  a turn. Corollary: a typo in `BASE_ALLOWED_TOOLS` cannot fail loudly — it named
  `Task` for a whole SDK generation after the tool became `Agent`.
- **`settingSources` is `["project"]`, and that is load-bearing.** The BFF
  mirrors the org's coding-relevant skills into the project clone at
  `.claude/skills/`, and the SDK only discovers them if the project source is
  admitted — its `skills:` option is an ALLOWLIST over discovered skills, not a
  loader, so a name matching nothing is dropped in silence. That shipped once:
  the run reported success while the agent compensated by grepping `SKILL.md`
  out of the tree. `AGENT_SETTING_SOURCES` is exported and
  pinned by a test so a revert to `[]` fails there instead of in a build, and
  `runClaudeQuery` warns when the `init` message's resolved list is missing
  something we asked for (`skills_preload_check.ts`). 'user' and 'local' stay
  out — a developer's `~/.claude` has no place in a container run. The MCP
  isolation that `[]` used to give for free is now explicit: `strictMcpConfig`
  keeps a project's own `.mcp.json` from declaring servers into a run.
- **`skills:` is an allowlist, and it preloads nothing.** Both halves are
  measured, not assumed. A mirrored skill absent from the array is *rejected* by
  the Skill tool, so the run lists the WHOLE mirror
  (`listMirroredSkills`) — the BFF already decided what this build may use, and
  omitting the unpinned copies would leave them as inert files on disk. And
  membership only buys a name and a description in the catalog: the body arrives
  when the model invokes the skill. So a pin, which asserts the guidance IS
  needed for this work, appends that body to the system prompt
  (`readSkillBodies`) instead of trusting the model to go looking. The comment
  claiming `skills:` "injects full bodies at startup" was wrong for as long as it
  existed, through the earlier `aep-task-skills` plugin too — an agent given a
  listed skill cannot state a codeword from its body until it calls the tool.
- Self-contained: all agent and SDK-specific wiring lives here.
- **Skills scope is stated by the caller, never read off `AEP_COMPONENT_NAME`.**
  A milestone Job carries a sentinel there (`aep-milestone`), so an
  implementation run resolves the union of `skillsPinned` across every
  `specs/design/components/*/design.json`; a validation run applies no design
  skills at all. The local harness (`local.ts`) carries its own sentinel
  (`aep-local-milestone`) for the same reason: a playground coding run works
  the whole project, same as the milestone loop, and may touch several
  components — there is no single one to name.
- **There are NO plugins, and the mirror is the only skill source.** The runner
  once loaded two — one it assembled from the library, one it materialised per
  task — and both are gone. `aep`, `aep-validation` and `playwright-cli` are
  library skills carrying `audience: [coding]`, so the BFF mirrors them into the
  project repo exactly like `go`, and a coding session reads one directory. What
  reaches a build is therefore decided in one place, by the BFF: `design`'s
  description cannot appear in a coding session's catalog because
  `audience: [design]` keeps it out of the mirror, not because the runner filters
  a library. ADR:
  `remote-worker/design/decisions/ADR-0005-the-workflow-rides-the-project-mirror.md`.
- **The always-on set is the runner's, not the design's.** `alwaysOnSkills`
  (`lib/runner.ts`) names `aep` for every run and `aep-validation` for a
  validation task; `requireWorkflowBodies` reads those bodies out of the mirror
  and appends them to the `claude_code` preset. Everything else a component needs
  is a `skillsPinned` entry someone put in a `design.json` — but no design decides
  whether a coding run follows the coding workflow. `playwright-cli` is
  deliberately NOT always-on: `aep-validation` names it, and mechanics a run may
  not reach for should cost a load, not every turn.
- **A mirror with no workflow skill is FATAL.** `requireWorkflowBodies` throws and
  both entrypoints report a failed run. Every other skill degrades — a dangling
  pin warns and the build continues — because missing guidance costs quality and
  aborting costs the whole build. The workflow is the exception: a session without
  it improvises a procedure and reports success, which is invisible from outside.
  The mirror's writes are best-effort by design (they may not fail a creation,
  publish or dispatch), so this is where that becomes visible. **Do not add an
  image fallback**: two sources drift, and the fallback would silently discard an
  org's own edit to the skill. The check sits in `runClaudeQuery`, so no new
  entrypoint can start a procedure-less session.
- **Anything a skill must invoke by absolute path reads `$AEP_SKILLS_DIR`**, now
  `<workspace>/.claude/skills`. The runner stamps it (`lib/runner.ts`) because it
  is still the only layer that knows the value. `aep-validation` runs the
  platform's report generator through it, and the component contract a lead hands
  to fan-out subagents (`contractReferencePath`) resolves the same way. A
  hardcoded path is wrong somewhere — it was, and it named `/app/plugin`.
- **`lib/workflow_skill.ts` composes ONE file**: `skills/aep/SKILL.md` for a mode.
  Production mirrors the authored trunk verbatim; local mode applies
  `skills/aep/overlays/local.md`, and `local_skill_mirror.ts` does it **while
  writing the mirror**, which is what keeps it unskippable. Every writer filters
  `overlays/` out — `loadLibrary` never seeds it, so no org repo has one for the
  BFF to copy — because the `aep` skill lets the agent read its own directory and
  an overlay beside `SKILL.md` is a second procedure it can find.
  `make workflow-skill` prints either mode without spawning a session.
- **The library arrives as a BuildKit named context**
  (`--build-context skills=<repo>/skills` → `COPY --from=skills . /app/skills`),
  the same mechanism `aep-api` uses. Add it to any new build path or the
  playground has no library to mirror: `build-runner.sh`, `release.yml`'s matrix
  row, and `local/run-local.sh` all pass it. A dispatched run does not read it —
  its skills come from the clone — but `local.ts` does.
- **One image**, `remote-worker/Dockerfile`, serves BOTH task kinds
  (`AEP_TASK_KIND=implementation` and `=validation`). It is Debian-based
  because Playwright's browsers are glibc-linked; do not reintroduce a second,
  slimmer image without moving the Helm/compose/release/`AGENT_RUNNER_IMAGE`
  consumers with it. Build + k3d-import it locally with `make build-runner`.
  Full `deployments/scripts/setup.sh` pre-builds it in the background (off the
  critical path) and imports it in `setup-aep.sh`; `PREBUILD_RUNNER=0` reverts
  to a serial build. The build is skipped when the tag exists, so use
  `FORCE=1 make build-runner` after changing the Dockerfile or `src/`.
- **The imported tag is pinned in containerd** — `build-runner.sh` calls
  `pin_node_image` (`deployments/scripts/utils.sh`) after a successful
  `k3d image import`. `aep-runner:dev` is local-only, so there is no registry to
  re-pull from, and it sits idle between dispatches: kubelet's imageGCManager
  collects least-recently-used images first (it sorts `byLastUsedAndDetected`, not
  by size) once the node's image filesystem crosses its high threshold (85%,
  freeing down to 80%), so an idle runner tag goes early and its size means one
  eviction covers much of the target. That leaves the next dispatch in
  `ImagePullBackOff` with nothing to recover from. The same helper
  covers the other local-only imports (`thunder-app-operator:local`, the patched
  RCA image). It doubles as import verification: an image in no node's containerd
  means the import silently did not land. Verify a pin from the host with
  `docker exec k3d-openchoreo-server-0 crictl inspecti aep-runner:dev` →
  `"pinned": true` (there is no host-side `crictl`). An import replaces the
  containerd record, so the pin has to live in the import path, not in a manual
  step.
