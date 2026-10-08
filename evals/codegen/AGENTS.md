# AGENTS.md — evals/codegen

Scores the coding run end to end: saved case → `play code` → `play wire` →
walker → judge → archive. What it measures and how to read it:
[README.md](README.md).

## The credential rule

ONE credential: the Claude OAuth token in `deployments/.env`
`AEP_CODING_ANTHROPIC_KEY` (`sk-ant-oat…`). Never an API key, for anything.
All of it lives in `src/credentials.ts`; do not build a child env anywhere else.

- The file is parsed with `util.parseEnv` for that one key — never loaded into
  `process.env`, never printed. Do not `cat` it.
- `play` children get `ANTHROPIC_API_KEY=""` (empty, so `play`'s own
  `loadEnvFile` cannot refill it), no `CLAUDE_CODE_OAUTH_TOKEN`, the token in
  `AEP_CODING_ANTHROPIC_KEY`. SDK sessions get the token as
  `CLAUDE_CODE_OAUTH_TOKEN` and no API key; `system/init`'s `apiKeySource` is
  checked and an API key aborts the attempt as a harness error.
- Do not import a playground module that loads `@aep/ae-design-agent`: its module scope
  merges `deployments/.env`, API key included, into this process.

## Conventions

- Every knob is in `src/config.ts`; flag → env → default.
- Every SDK session goes through `src/session.ts`: the prompt is a held-open
  stream (a string prompt closes stdin and every hook then reports cancelled),
  `settingSources: []`, structured output validated by zod.
- A case is `specs/` + `issues/` + `case.yaml` + `checklist.yaml`. Schemas are
  strict; an unknown key is an error. Checklists are hand-editable and frozen.
- Classify every failure: the code's (`hard-fail`, score 0, counted) or not
  (`harness-error`, excluded). A new failure mode picks one deliberately.
- Teardown is unconditional: every `play` child is stopped by SIGTERM, then
  SIGKILL plus the docker backstop.
- Pure logic (score, report, snapshot choice, READY parsing, schemas, the walker
  guard) is unit tested in `test/` with no model calls.
