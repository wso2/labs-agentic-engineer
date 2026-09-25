# Booting the agent under evaluation

## A child process, where the spec says "in-process"

The original design (ADR-0035, decision 2 records the outcome) said to
**boot the agent in-process** with a real model and an in-memory
conversation store. `bootAgent` boots it as a locally spawned **child process**
instead, on an ephemeral port.

The contrast the spec is drawing is with evaluating a **deployed** agent, which
it rejects because every fix would then cost a rebuild and a redeploy. A local
child satisfies that choice — a retry costs seconds, and nothing is deployed —
while the alternative does not survive contact with the component contract: a
generated agent calls `listen()` at module load and owns its own process
lifecycle, so importing it into the harness's process would fight that design,
and an agent crash would take the evaluation down with it. A child also keeps
the agent's real HTTP contract in the loop, which is the thing under test.

The port is ephemeral rather than the contract's fixed 9090 because a fix loop
boots the agent again and again; a fixed port makes the second boot fail on an
address the first still holds. The port arrives as `PORT`.

## Readiness is the agent's own verdict, and it is bounded

`bootAgent` waits for `GET /healthz` to answer `200 {ok:true}` and gives up at a
bound, quoting the `missing` list and the `store` state it last saw. Two
failures that must never be confused with a bad score:

- an agent that never became ready is a **harness or wiring** failure, not an
  agent that behaved badly, so it raises rather than running scenarios;
- a boot that waited forever would turn a misconfigured agent into a stuck
  build, so the wait is bounded and a dead child is detected immediately rather
  than waited out.

The bound comes in two parts. The FIRST boot of a run is a diagnosis, not a
wait: until one boot has succeeded, an agent that does not come up is almost
certainly misconfigured, and paying the full bound for that answer on every
scenario of every round is how a ten-scenario, three-round loop spends half an
hour learning one fact. So the first boot gets a short bound
(`AGENT_EVAL_BOOT_TIMEOUT_MS`, 20 s by default), its failure is remembered, and
every later scenario in that run fails immediately with the same reason rather
than repeating the wait. Once a boot HAS succeeded, a slow one is a busy
machine and gets the longer bound.

`MEMORY_DB_*` is deliberately never set. The spec requires memory to be
exercised without Postgres, so the agent must serve the run from its own
in-memory store; if it cannot, `/healthz` says `store:"initialising"` and the
boot fails saying exactly that.

## `HOME` and `TMPDIR` are absent too

The child inherits nothing: its whole environment is what the harness decided
to give it (the stub addresses, `MODEL_*`, `PATH`), so a variable that happens
to be set on a build machine cannot make a scenario pass here and fail in the
cluster. The cost is worth naming, because the symptom does not explain itself:
a generated agent whose dependency wants `HOME` or `TMPDIR` — a cache
directory, a credential helper — will fail at startup with that library's own
message and no hint that the harness withheld the variable. If that happens,
add the variable to `agentEnv` in `provider.ts` deliberately, rather than
handing the child the whole ambient environment back.

## Where the stubs run

The tool stubs are started by the **provider**, inside the promptfoo child
process, not by the CLI. The CLI decides *what* to stub (from the agent
document's `x-aep.tools.openapi[]`) and passes it in the emitted provider
config; it cannot serve them itself, because it runs promptfoo with a
synchronous spawn that blocks its own event loop for the whole run.

Stubs and agent are started and torn down per **scenario**: it costs a process
start against work dominated by model calls, and it buys both conversation
isolation between scenarios and the guarantee that no child outlives the
scenario that needed it.

## The allow-list is served, not just recorded

`x-aep.tools.openapi[].allow` is the agent's security boundary, and the stub
world honours it: an operation the contract defines but the allow-list
withholds answers **403**, naming the operation, and is reported out of the
scenario as `metadata.toolOverReach`. This cannot widen anything — the built
agent only carries tools for allow-listed operations, and it is the built agent
that boots — but a stub answering 200 to everything in the contract would make
an over-reach invisible in the one place it is cheap to see. 403 rather than
404 because the operation is real; a 404 would read as a broken contract and
send a fix round after the wrong thing.

An `allow` entry the contract does not define is refused outright. The platform
rejects it at design-save, so reaching it here means the agent was generated
without that tool and would score badly for a reason no prompt fix can address.

## What travels where

The provider's `config` block is JSON on disk, under the build's output
directory. It carries the App Path and the tool contracts — never a credential.
The org's Anthropic key reaches the agent as `MODEL_API_KEY` through the
environment only, and reaches the judge as promptfoo's own `ANTHROPIC_API_KEY`.
One credential, two names, no file.
