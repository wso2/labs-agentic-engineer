# ADR-0020 — External dependencies are provisioned with env var names, not values; real values are a deploy gate

[ADR-0004][adr4] made the coding agent the author of every line of a component's
`workload.yaml`, and [ADR-0013][adr13] split dependency wiring into what is
DERIVED from the design and what must be RESOLVED live, stamping the derived half
— a resource's ref and its output→env-var mapping — into `design.json` at
design-save time. Both decisions rest on the same observation: most of what a
component needs to know about a dependency is a pure function of the design.

The config *values* of an `external` dependency were never brought into that
frame. They are still collected the way they were before either ADR: a drawer
opens when the developer clicks Build, and the build refuses to start until every
declared key holds a non-empty value.

That is the wrong time and the wrong place, and the code half-knows it. The
console's own comment on the collection path says the values are "dummy at build
time, real ones later". The coding agent codes against env var *names*, which
ADR-0013 already guarantees are in the design before any value is asked for. The
only consumer that needs a real URL or a real API key is a running pod, and no
pod exists until something deploys.

So the platform blocks the entire delivery pipeline — including coding work that
provably does not need the credential — on a credential nobody will read for
another twenty minutes. And it does so in a panel with no route: local component
state that cannot be linked to, cannot be handed to the colleague who actually
holds the key, and is lost on reload.

## Decision

Split the collection of an external dependency by **who needs it and when**, and
move each half to the moment its consumer exists.

**Names — authored at build time, empty.** Provisioning authors every key the
design's union schema declares, with no value: plain keys empty, keys with a
declared default seeded with that default, the secret-store path empty. The
binding renders, the config map and external secret exist, and the coding agent
receives its env vars defined. Build never blocks on a value.

**Values — collected during the run, enforced at deploy.** A first-class,
deep-linkable configuration section on the builds page collects real values while
the run proceeds. The milestone run then reaches a **deploy gate** that refuses to
deploy until every declared key holds a non-empty value and every platform
resource is provisioned. A blocked run parks in `waiting` — the state that already
means "something outside the platform is needed" — names what it is waiting for,
and resumes when the last value is saved.

Three properties make this hold.

**Every declared key must be authored, or nothing renders.** This is not
defensive. The OpenChoreo resource pipeline resolves `${environmentConfigs.KEY}`
against the binding's config map, and a key that is *absent* with no schema
default is a hard rendering failure: the binding does not render at all. A key
*present and empty* renders as an empty string. The distinction is the whole
mechanism, and it is why the authoring step enumerates the schema rather than the
supplied values.

**Empty is the discriminator, and it is the only one available.** Secret values
cannot be read back — the secret backend is write-only by design — and key names
are identical before and after real values arrive. An earlier draft of this work
invented a "pending" vault entity so the secret path itself could carry the
distinction. That was discarded once it was established that an empty
secret-store path renders an external secret with an empty remote reference,
which creates no Kubernetes secret at all. Empty means unset, uniformly, for
plain keys and for the secret path. One rule, no second concept, and no writer
and reader to drift apart.

**Readiness is derived from the design, not from the binding.** The resource
pipeline does not prune, so a key dropped from a design lingers on its binding
forever. The design is the authority on what must be set; the binding only
answers what is set. Reading it the other way round would report a stale value as
configuration.

The state is called `configured`, not `ready`. OpenChoreo's `Ready` condition on
these bindings is `readyWhen: ${true}` and is True throughout — including while
every key is empty — because an external secret is health-checked as an unknown
resource and reported healthy unconditionally. The two words name different
facts and must not be merged.

## Consequences

**The deploy gate requires the platform to own deployment.** Today components are
created with auto-deploy enabled and OpenChoreo deploys on every green build. A
gate cannot withhold something the platform never does. Disabling auto-deploy so
the platform owns the deploy act is decided in [ADR-0017][adr17]; the gate is
therefore part of the same decision — and it is not a flag flip: with auto-deploy
off the controller cuts no component release at all, so the platform must create
the release, pin the release binding, and manually re-run the four side effects
that currently ride on a green build. One of those, the API trait sync, is what
configures gateway authentication; missing it leaves a protected API passing every
request through unauthenticated. Under auto-deploy, the observer that drives it
fires on *build success* and its failures are swallowed by a best-effort fan-out.
Both become unacceptable once deploy is a distinct, gated act, so the observer
moves into the deploy activity and trait-sync failure fails it.

**Deploy becomes one act per milestone.** Previously every merged pull request
produced a build that deployed itself, so a milestone with five merges deployed
five times, mid-loop. Gating implies a single deploy at settle, on success only.
This aligns deployment with [ADR-0011][adr11]'s unit of execution, at the cost of
the deployed-app-after-every-merge feedback loop within a milestone.

**The coding agent loses live third-party integration.** It receives its env vars
defined and empty rather than real credentials, so it cannot exercise a live
service during a coding cycle. This is the intent, and it is an improvement over
the prior silent dispatch-without-secrets path, but it changes what agent runs
can do. The agent is told the condition explicitly rather than left to infer it
from empty strings.

**There is an interim window.** The collection change ships before the gate,
because the gate depends on the readiness signal the collection change
introduces. In that window auto-deploy is still on and nothing enforces values,
so an app can deploy to `development` with empty configuration. This is strictly
weaker than the behaviour being replaced, which at least guaranteed nothing ran
unconfigured. It is bounded — one environment, and only projects nobody
configured — and it is accepted deliberately rather than overlooked.

**An external secret with an empty remote reference errors continuously** in the
secret operator's status for the whole window between build and configuration.
Nothing reads it and no binding blocks on it, but it will show red on any
operator dashboard and will be mistaken for a fault.

**No wait for the secret operator is needed, and this is structural.** The
platform cannot observe whether a secret landed: its only read path for external
resources is control-plane custom resources. It does not need to. Because an
empty secret-store path creates no Kubernetes secret at all, at deploy the secret
either exists with real values or does not exist — and a pod referencing a missing
secret retries until Kubernetes finds it. The failure mode that would require a
wait, a secret existing with stale values so the pod starts happily and never
restarts, cannot arise.

**Two OpenChoreo behaviours are now load-bearing and are documented nowhere
official.** That a present-but-empty value renders, and that a binding reaches
ready with an unresolvable secret. Both were read out of the controller source at
a pinned version and recorded, with citations, in a design note under the
`projects` package. A version bump must re-verify them; if either changes, the
authoring approach changes with it.

**There is no manual deploy.** Deploy is workflow-owned, so until an endpoint
exists the only way to deploy is to run a milestone. Because a blocked run parks
rather than settling, a settled-but-undeployed milestone cannot arise — but
correcting a wrong value and redeploying without a new milestone has no path.

## Alternatives considered

**Keep collecting at build time, just move the panel to a route.** Fixes the
linkability problem and nothing else. The developer is still blocked on a
credential that nothing will read for twenty minutes, which is the substantive
complaint.

**Author a sentinel string rather than an empty value.** Would have been forced
if a present-but-empty value failed to render. It does render, so the sentinel
buys nothing and costs a magic constant that must be recognised identically by
every reader and never legitimately typed by a user.

**Stage a "pending" vault entity so the secret path discriminates.** Discarded as
described above: an extra concept, a writer and a reader that can drift, and two
extra behaviours to verify against a controller outside this repo.

**Gate by holding the release binding in `Undeploy` instead of adding a deploy
act.** Superficially cheaper — no new user-facing action. But the moment the
platform withholds the binding it owns every side effect that rode on
auto-deploy, including the gateway authentication sync. It is the same work
without admitting it, and without the explicit act that makes the state legible.

[adr4]: ADR-0004-declarative-dependency-wiring.md
[adr11]: ADR-0011-milestone-is-the-unit-of-execution.md
[adr17]: ADR-0017-the-platform-owns-deploy.md
[adr13]: ADR-0013-derived-wiring-lives-in-the-design.md
