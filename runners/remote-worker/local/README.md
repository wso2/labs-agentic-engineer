# Local one-shot harness

Run the coding-agent runner on your machine without the platform (no k3d,
no BFF, no Argo). The runner executes in its real Docker image; the only
substitution is `token-stub.mjs`, a loopback HTTP server that plays the
platform's `POST /internal/v1/tasks/{taskId}/credentials/refresh` endpoint
and hands back your GitHub PAT.

## What a run does

1. Starts the token stub on `127.0.0.1:8377` (host side).
2. Builds the runner image from `../Dockerfile` (cached after the first time).
3. Runs the one-shot container: clones `AEP_REPO_URL`'s default branch,
   reads the `aep` skill from the clone's mirror, and lets the agent work `AEP_PROMPT` —
   branch, commit, push, PR, exactly as a cluster run would.

The run reads every skill from the `.claude/skills/` mirror the BFF wrote
into the clone — there is no skills fetch and no plugin. The repo-root
`skills/` library is bind-mounted read-only over `/app/skills` all the
same, so a skill edit is live on the next run without a rebuild; the
playground's local mode mirrors from it, and the mount is harmless here.

## Usage

```bash
colima start                        # or however you run the docker daemon
cd runners/remote-worker/local
cp env.local.example .env.local     # fill in the four required values
./run-local.sh
```

The runner streams progress NDJSON to stdout. After exit, the cloned
workspace (including `.logs/claude.log`, the full SDK transcript) is kept
under `workspace/<org>/<project>/<taskId>/` for inspection.

## Validation tasks

The same image serves validation runs (it bakes Playwright + chromium),
so only the dispatch env differs. Point the harness at an alternate env
file:

```bash
cp env.local.example .env.local          # secrets + repo, as usual
# create .env.validation: sources .env.local, sets
#   AEP_TASK_KIND=validation
#   AEP_PROMPT="This is a validation task. Work on this GitHub validation issue: <url> ..."
./run-local.sh .env.validation
```

The prompt must say "validation task" and point at an issue labelled
`aep` + `validation` whose body follows the validation issue contract
(criteria file path, Deployed endpoints table, test layout, report
requirements) — see `scripts/create-validation-issue.mjs` at the repo
root for the interim issue generator. The `aep-validation` skill drives
the rest.

## Testing a custom task

- **Real flow:** create a GitHub issue in the test repo whose body is the
  task spec, and set `AEP_PROMPT` to point at its URL — the `aep` skill
  drives issue-comment/branch/PR conventions from there.
- **Skill iteration:** edit `<repo>/skills/aep/SKILL.md` (or any skill the
  runner loads); the `/app/skills` mount picks it up on the next run.
  (In-cluster, per-task skills come from the BFF snapshot instead — that
  path needs the full `deployments/` setup.)

## Safety notes

- The agent runs with `bypassPermissions` — that is why the harness only
  runs it containerized, never bare on the host.
- Scope `GITHUB_PAT` to a single throwaway test repo (fine-grained PAT).
- The stub only releases the PAT to callers presenting the per-run
  access token (a fresh random value each run, minted by the stub's
  `/oauth2/token` and required as `Authorization: Bearer` on refresh).
  `PUBLISHER_CLIENT_ID` / `PUBLISHER_CLIENT_SECRET` / `PUBLISHER_TOKEN_URL`
  are set automatically so oneshot can mint that token the same way a
  cluster Job does. Still keep `STUB_BIND=127.0.0.1` (default); on a Linux
  host `host-gateway` cannot reach loopback, so you would need
  `STUB_BIND=0.0.0.0` — the bearer check is what keeps that tolerable.
- `.env.local` and `workspace/` are gitignored; `local/` is dockerignored
  so secrets never enter the image build context.
