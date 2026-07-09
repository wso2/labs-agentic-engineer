# ADR-0007 — Resource-type behavior keys on CRT metadata markers, never on names

ADR-0006 made end-user auth an explicit `platform-resource` dependency
(`resourceType: thunder-app`) instead of an implicit `callerIdentity` flag —
but it wired that specific behavior in by checking for the literal string
`"thunder-app"` in three places in aep-api: the auth-derivation pass at design
save, the SPA runtime-config emitter, and the runtime-config emitter's
redirect-URI patch. That directly contradicts the platform's original design
goal — "a new resource offering is a cluster install, not an app-factory
change" — for the one resource type that actually needed platform behavior
beyond generic provisioning. Any future auth flavor (or any other resource
type needing similar treatment) would have required an aep-api code change and
a release, exactly what the `platform-resource` mechanism was built to avoid.

## Decision

aep-api keys behavior on **metadata markers carried by the
`ClusterResourceType` itself** (labels and annotations under the
`aep.wso2.com/` prefix), never on `resourceType` name literals. The PE who
authors a `ClusterResourceType` declares which generic behaviors it needs by
attaching markers; aep-api's production code contains no resource-type name
literals at all (`grep -rn '"thunder-app"' services/aep-api --include='*.go'`
turns up only test fixtures using it as sample data). The `resourceType`
field itself is unchanged — it is still the concrete, architect-authored
`ClusterResourceType.metadata.name` the MCP catalog surfaces — the type-agnostic
change is entirely on the consumption side.

The marker vocabulary:

| Marker | Kind | Meaning |
|---|---|---|
| `aep.wso2.com/role: end-user-auth` | label | A `service` component that declares a dependency of this type gets `exposesAPI.auth: end-user-required` stamped automatically at design save. An explicit, conflicting `exposesAPI.auth: service-required` on such a component is rejected as a validation error, not silently overridden. |
| `aep.wso2.com/consumer-url-env-config: <key>` | annotation | Once the consuming web-app's public URL resolves, aep-api patches `<spaOrigin><consumer-url-path>` into this key on the dependency's dev `ResourceReleaseBinding` environment configs. |
| `aep.wso2.com/consumer-url-path: <path>` | annotation | Path appended to the consumer's origin for the patch above. Defaults to `/callback` when the env-config annotation is present without it. |
| `aep.wso2.com/skill: <skill-name>` | annotation | Design save ensures the named skill is present in the design's `skillsApplied` whenever a dependency of this type exists. |

Ownership is split cleanly by who authors what:

- **The PE** authors the markers, on the `ClusterResourceType` they install —
  the same act that already declares the type's parameters, outputs, and
  readiness gate. Adding a new resource type, including a new auth flavor, is
  a cluster install (a CRT manifest) plus a skill; it is never an aep-api code
  change or a platform release.
- **aep-api** reads markers generically through one shared definition
  (`services/aep-api/internal/feature/dependencies/resources/markers.go`) and
  branches only on marker values, never on a type's name. The same marker
  extraction and the same catalog fetch serve auth derivation, the
  consumer-URL patch, and skill attachment.
- **Skills** carry all per-type domain knowledge that isn't a generic platform
  mechanism — e.g. what an OIDC dependency's output keys mean and how to wire
  a login flow with them. The platform moves bytes; the skill teaches the
  agent what to do with them.
- **The reserved-name approach is retired.** Anything that previously
  special-cased `"thunder-app"` now reads a marker instead; no resource type
  name has platform-level meaning any more.

SPA runtime config is fully generic. For a `web-app`, every `platform-resource`
dependency's resolved binding outputs are emitted into `window._env_` as
`<UPPER_SNAKE(depName)>_<UPPER_SNAKE(outputName)>`, via the single shared
helper `resources.EnvVarName` — the exact convention `wiring.go` already uses
to inject the same outputs as pod env vars for services, guarded by a
cross-package test so the two paths can never drift apart. There are no
`THUNDER_*` keys and no platform-emitted URL keys: the SPA computes
`redirect_uri = window.location.origin + '/callback'` itself, taught by the
consuming skill, and the platform's only job is to get that origin registered
as a valid redirect URI on the OAuth application (via the consumer-URL-patch
annotation above).

## Consequences

- **Fail-closed save gate, coupled to OC availability.** Design save fetches
  the CRT marker catalog once, only when the design declares at least one
  `platform-resource` dependency. If that fetch fails (catalog unreachable or
  unwired), the save fails closed with a retryable `503`
  (`ErrResourceCatalogUnavailable`) rather than silently skipping derivation —
  a silent skip could leave an API that must sit behind end-user sign-in
  exposed without the gateway auth trait ever landing. This deliberately
  couples save availability to OpenChoreo's catalog being reachable; the
  alternative (silent open) was judged the worse failure mode.
- **Fail-open, retried cascade for the consumer-URL patch and output
  emission — an intentional asymmetry with the save gate.** These run on the
  deploy-event fan-out, not inside a user-facing request: an unreachable
  catalog, an unresolved SPA origin, or a not-yet-ready binding all defer the
  `env-config.js` write rather than erroring, and the next cascade (e.g. the
  operator's next reconcile, the SPA's next deploy event) retries. A design
  save is a one-shot user action that must not silently produce a wrong
  result; a cascade hook gets another chance on the next event, so deferring
  is the safer default there instead.
- **Every output of a web-app-declared `platform-resource` dependency is
  browser-visible.** Because the emission is generic — all outputs, not an
  allow-listed subset — a `ClusterResourceType` with a secret-bearing output
  (a database password, a private key, an admin token) must never be declared
  as a dependency of a `web-app` component. This is PE guidance, not a
  platform-enforced check: the type author is responsible for keeping
  secret-bearing outputs out of any type intended for web-app consumption, or
  splitting such a type into a browser-safe half and a backend-only half.
- **Readiness generalizes to "≥1 output ⇒ all outputs," which depends on an
  OpenChoreo guarantee aep-api does not itself verify.** The emitter defers
  the whole dependency (no partial keys) until the binding's `status.outputs`
  is non-empty, then emits every output present. This is only correct if
  OpenChoreo surfaces `status.outputs` atomically once the CRT's `readyWhen`
  gate is satisfied — i.e. a binding never exposes some outputs before others.
  aep-api's generic emission path takes this as a load-bearing assumption
  about OC's binding-status contract; it does not (and cannot, from outside
  OC) independently confirm atomicity for an arbitrary CRT's outputs list.
- **The generic `<DEP>_<OUTPUT>` key contract plus in-browser redirect
  computation removes the platform from the OAuth redirect-URI business
  beyond registering the origin.** The platform's only continuing
  responsibility is the consumer-URL-patch annotation (getting the SPA's
  origin onto the binding); everything downstream of that — building the
  actual `redirect_uri`, calling `/oauth2/authorize`, handling the callback —
  is skill-taught, generic browser code with no platform-specific keys to
  parse.

## Alternatives considered

- **A reserved, well-known type name (e.g. every auth-flavored type must be
  literally named `"thunder-app"` or match a documented naming convention),
  with the contract enforced only by documentation, not code.** Rejected —
  this still keys real platform behavior on a name, just moves the coupling
  from code to a convention nothing checks; a second auth-flavored type would
  either have to falsely call itself `thunder-app` or fall outside the
  contract entirely. The user rejected this in favor of true genericity: the
  marker vocabulary carries the same information as metadata that a `CRT` can
  express label-for-label without a name constraint of any kind.
- **A config knob** — e.g. an env var or platform-level config field naming
  "the" auth resource type for a cluster. Rejected — less Kubernetes-native
  than metadata carried directly on the resource the behavior actually
  applies to, and it caps a cluster at a single auth-flavored type; the marker
  approach lets any number of differently-configured auth types (or other
  role-bearing types) coexist, each self-describing.
- **Keeping the `THUNDER_*` window._env_ keys** (`THUNDER_URL`,
  `THUNDER_CLIENT_ID`, `THUNDER_SCOPES`, `THUNDER_REDIRECT_URI`,
  `THUNDER_AFTER_SIGN_IN_URL`) as a fixed, IdP-branded contract regardless of
  `resourceType`. Rejected — the keys themselves encoded a hardcoded
  assumption (every auth dependency is Thunder, every dependency is named
  the same way), which is exactly what this refactor removes; a second OIDC
  provider or a differently-named dependency would either collide with or be
  unable to use the fixed names. The generic `<DEP>_<OUTPUT>` convention has
  no such ceiling and reuses the naming scheme service-side env injection
  already relies on.

See ADR-0006 for why end-user auth is a `platform-resource` dependency at all,
and `learning/thunder-resource/PLAN-generalization.md` for the task-by-task
implementation record this ADR documents the outcome of.
