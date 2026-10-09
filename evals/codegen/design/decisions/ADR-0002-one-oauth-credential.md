# ADR-0002: One OAuth credential for every session

**Status:** Accepted (2026-10-05)

## Context

A sweep is a comparison. If the billing path can differ between two sweeps (an
exported key in one shell, a keychain login in another), the runs were not made
the same way. Claude Code ranks `ANTHROPIC_API_KEY` above every other
credential, so a key that only exists in the environment wins without notice.
`deployments/.env` holds both the platform's API key and the coding OAuth token.

## Decision

**On demand, never in CI.** A sweep spends the subscription and runs for hours; `make eval-codegen` is the only entry point.

**One credential: the OAuth token, never an API key.** Every Claude session uses
the subscription OAuth token from `deployments/.env`'s `AEP_CODING_ANTHROPIC_KEY`
(`sk-ant-oat…`, the playground's coding-credential name from repo ADR-0016). This
covers the coding run, the planner, the walker and the judge. A value without the
OAuth prefix is refused before anything is spent. All of this lives in
`src/credentials.ts`; no other module builds a child env.

**Envs are built by removing competitors, not by adding the token.**

- Every env drops the surrounding Claude Code session's variables (`CLAUDECODE`,
  `CLAUDE_CODE_*`).
- SDK sessions (planner, walker, judge) delete `ANTHROPIC_API_KEY` and
  `ANTHROPIC_AUTH_TOKEN` and get the token as `CLAUDE_CODE_OAUTH_TOKEN`.
- `play` children get `ANTHROPIC_API_KEY` and the `AEP_MODEL_*` connection
  variables set to the empty string, not deleted. `play` calls
  `process.loadEnvFile`, which fills only absent variables, so a deleted key
  would come back from the file. The token goes in `AEP_CODING_ANTHROPIC_KEY`.

**The file is parsed, not loaded.** `util.parseEnv` reads the one key.
`process.loadEnvFile` would put the file's API key into this process's
environment. For the same reason the harness does not import playground modules
that load `@aep/ae-design-agent`, whose module scope merges the file into `process.env`.

**The token is never logged, archived or put on argv.** Error messages say what
the value is not, never what it is. Provenance records paths and ids, never env
values. The argv of every child carries paths and flags only, since `ps` reads
argv.

**The result is checked, not only the intent.** Each SDK session reads
`apiKeySource` from `system/init` (`session.ts`); a value that means an API key
aborts the attempt as a harness error.

**Exception: `opencode` configs.** For an `opencode` entry in `configs.yaml`, the
model connection is the credential. Its key is read from `deployments/.env` (then
the shell) by the variable name the entry gives. The OAuth token is blanked
instead, because the playground refuses OpenCode on an OAuth token. The harness's
own agents still use the OAuth token.

## Consequences

- A sweep cannot run without a valid OAuth token, even when an API key is
  available.
- Two sweeps of the same Claude config always bill the same way.
- An `AEP_MODEL_*` variable exported in the developer's shell cannot move a
  Claude config onto another connection.
- A new child process must get its env from `credentials.ts`.
