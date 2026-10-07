# Running in a build pod

This package is a workspace package of the platform monorepo, but the place it
actually earns its keep is a build pod — the `remote-worker` container where the
coding agent generates an ai-agent component and, before opening the PR,
evaluates what it built. A build pod holds no monorepo. Two things follow.

## The harness ships in the runner image

`runners/remote-worker/Dockerfile` installs this package at
`/opt/aep/agent-eval` (`$AEP_AGENT_EVAL_HOME`) and puts a one-line wrapper on
`PATH` as `agent-eval`. It reaches the image through a BuildKit **named build
context** (`--build-context agent-eval=packages/agent-eval`), the same mechanism
the skill library and the `bal library` tool already use, and every builder of
that image passes it: `release.yml`'s matrix row,
`deployments/scripts/build-runner.sh`, and `runners/remote-worker/local/run-local.sh`.
`runners/remote-worker/src/agent_eval_packaging.test.ts` pins all three — a
context passed by one builder and not another produces an image that carries the
harness locally and not in the cloud, which is the failure this step is least
able to notice on its own.

It runs from SOURCE under `tsx`, the interpreter that image's ENTRYPOINT already
uses, rather than being compiled to `dist/`. Compiling in the image would need
the monorepo's `tsconfig.base.json`, which is outside every build context that
image has; duplicating those compiler options into the Dockerfile would be a
copy that can silently disagree with the one the tests compile against.

Dependencies install with `npm ci` from `package-lock.json` — a second lockfile
beside the workspace's `pnpm-lock.yaml`, exactly as `runners/remote-worker`
carries one. **Bump a dependency and both have to move**
(`npm install --package-lock-only` here). The loud failure is `npm ci` refusing
an out-of-sync lockfile during an image build; the quiet one is a pod grading
agents against a promptfoo the tests never saw, and a score nobody can reproduce.

promptfoo declares every model-provider integration it can drive as an
`optionalDependency`; installed by default they made the layer **~2.5 GB**, of
which the harness uses only the two judges it can grade with — the Anthropic
Messages provider (on `@anthropic-ai/sdk`) and the OpenAI Chat Completions
provider (on `fetch`), both regular dependencies. The image therefore installs with `--omit=optional` (**~0.3 GB**),
then adds back the one optional the harness cannot start without: libsql's
native binding for the image platform, which promptfoo's SQLite layer loads at
startup, pinned to the version the lockfile carries. The layer still sits
BEFORE the runner's own sources in the Dockerfile so a runner source edit does
not re-run it.

## The connection arrives under different names

In a pod the platform mounts the org's model connection as five variables: the
key as `AEP_EVAL_MODEL_API_KEY` (a secret), and `AEP_EVAL_MODEL_FORMAT`
(`anthropic` | `openai-compatible`), `AEP_EVAL_MODEL_BASE_URL` (ending in its
version segment, `…/v1`), `AEP_EVAL_MODEL_NAME` and
`AEP_EVAL_MODEL_AUTH_SCHEME` (`x-api-key` | `bearer`) beside it. All five come
together or not at all. The key is not mounted as `ANTHROPIC_API_KEY`: that name
already belongs to Claude Code, whose authentication precedence ranks it above
`CLAUDE_CODE_OAUTH_TOKEN` — so a platform that mounted the evaluation key there
would move the coding session of every OAuth-billing organization onto it in
silence (`docs/decisions/ADR-0016`).

`resolveConnection` (`src/connection.ts`) reads `AEP_EVAL_MODEL_*` first, and a
part of it that is absent defaults to Anthropic's own API
(`https://api.anthropic.com/v1`, `claude-sonnet-5-5`, `x-api-key`). Failing that,
it falls back to `ANTHROPIC_API_KEY` — on Anthropic's API only, whatever else is
set, so a key under Anthropic's name never follows another host. It never treats
`CLAUDE_CODE_OAUTH_TOKEN` as a fallback: that is the platform's own coding
budget, and it authenticates none of the API calls the judge makes.

`buildChildEnv` hands what comes back to the promptfoo child under the names
each consumer reads:

| Consumer | Receives |
|---|---|
| the agent under test | `MODEL_API_KEY`, `MODEL_ENDPOINT`, `MODEL_NAME`, `MODEL_API_FORMAT`, `MODEL_API_AUTH_SCHEME` — what a deployed ai-agent is given, less the governed proxy's `MODEL_API_KEY_HEADER` |
| the judge, `anthropic` | provider `anthropic:messages:<model>` with `apiBaseUrl` = the base URL minus `/v1` (`@anthropic-ai/sdk` posts to `/v1/messages`); key as `ANTHROPIC_API_KEY`; on `bearer`, `ANTHROPIC_CUSTOM_HEADERS` carries `Authorization: Bearer <key>` and the provider entry removes `x-api-key` per request |
| the judge, `openai-compatible` | provider `openai:chat:<model>` (Chat Completions, which every such host serves) with `apiBaseUrl` = the base URL; key as `OPENAI_API_KEY` |

`AGENT_EVAL_GRADER` names another judge and wins over the connection. The
simulated user is rule-based (`sim-user.ts`), so it has no model to follow.

**One judge the connection cannot name:** an Anthropic-format model whose name
holds a `:` (`gpt-oss:20b` on Ollama's `/v1/messages`). promptfoo's `anthropic:`
factory reads the model as the id's third `:`-separated field and drops the
rest, and its config has no field that names the model instead. The run reports
that as a run failure naming `AGENT_EVAL_GRADER`, rather than grade on a
truncated model name.

### The fallback is not unconditional, and `AEP_EVAL_KEY_MANAGED` is why

The fallback exists for a developer's machine, where `ANTHROPIC_API_KEY` simply
is "the key". **On a pod it is the organization's CODING credential** — possibly
an override configured so that coding, and nothing else, bills it. Falling back
to it there would grade agents on a ring-fenced budget.

The two look identical from inside the process, so the platform says which is
which: every dispatch sets `AEP_EVAL_KEY_MANAGED=1`, whether or not it had an
evaluation key to mount. Its presence means *the platform owns this pod's
evaluation credential* — so when `AEP_EVAL_MODEL_API_KEY` is absent beside it,
this run has no evaluation key, and the harness says so rather than reaching for
the one next to it. Nothing outside a dispatch sets it, so a local or playground
run keeps the fallback it needs.

`||` rather than `??` at each step, because ESO can materialize an **empty**
secret and an empty key is no key, not a key that fails to authenticate. The
difference decides whether the report reads "never became ready" or the judge is
pointed at an endpoint with a blank credential.

## The key never lands in the output

`report.md`, `out.json` and `promptfooconfig.json` sit in the build's output
directory, and the first two travel in the PR. The config never holds the key
(it reaches both consumers through the environment only). `report.md` and
`out.json` hold third-party text — promptfoo's stderr, the agent's transcripts,
the judge's reasons — so the CLI scrubs every key it was handed
(`AEP_EVAL_MODEL_API_KEY`, `ANTHROPIC_API_KEY`) from both BY VALUE, since the
connection's key has no fixed shape, and anything shaped like an Anthropic key
besides.

With no key at all the agent boots without a `MODEL_API_KEY`, answers 503 on its
own `/healthz`, and the report says it never became ready. The run still exits 0.
Evaluation reports; it never fails a build.
