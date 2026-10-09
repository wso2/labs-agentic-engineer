# Composition seam — `app.Run(Options)`

How aep-api starts: one importable composition **seam**, two process entries
(OSS vs **overlay module**), and nil-able `Options` as the only deployment
behaviour differences.

## Shape

```
cmd/aep-api (OSS)          overlay module main
        \                     /
         \                   /
          v                 v
     app.Run(Options)     ← public github.com/wso2/aep/aep-api/app
              |               (+ ocauth / secretsprovider contracts)
              v
     config.Load → Resolve → Assemble(Seam) → HTTP + watchers + shutdown
```

`Run` owns config load → resolve → assemble → degradation logs → HTTP serve
(existing timeouts) → background watchers under `async.Go` → signal shutdown.
Callers do not open the DB or wire the domain graph themselves. Seam types
(`AuthProvider`, `RequestAuthStrategy`, `AuthMode`) and context helpers live in
public `github.com/wso2/aep/aep-api/ocauth`; the secrets delivery port lives in
public `github.com/wso2/aep/aep-api/secretsprovider` so an overlay module never
imports `internal/`.

## `Options` (the seam)

Every field documents its nil meaning. Nil is a **feature off-switch**: disable
cleanly, never panic, never silently swap credential class or secret path.

| Field | Role |
|---|---|
| `AuthProvider` | Bearer for `AuthModeServiceM2M` OC calls (`Token` / `Invalidate`) |
| `RequestAuthStrategy` | Pure per-request credential-class decision (`Decide(ctx) AuthMode`) |
| `ImpersonateOrgResolver` | Sets `X-Impersonate-Org` on M2M calls when non-nil |
| `ImpersonateOrgResolverBuilder` | Late-bound resolver after `Resolve` opens the DB; ignored if the resolver is already set |
| `SecretsProvider` | Write-only secrets delivery (`secretsprovider.Provider`). Nil = delivery off |
| `ResourceLabels` | Labels stamped on every OpenChoreo resource AEP writes (validated at boot). Nil = none |

Compile-time `var _ ocauth.RequestAuthStrategy = …` / `var _ secretsprovider.Provider = …`
assertions keep seam implementations honest.

## Secrets delivery

One seam, two providers, no stub:

- **OSS / local** — `NewOSSOptions` constructs the in-process OpenBao-direct
  provider when `OPENBAO_ADDR` is set. It logs in by Kubernetes auth
  (`OPENBAO_AUTH_*`, role `aep-api`; no static token) through one lazy session
  per process, which `NewOSSOptions` also hands to `Run` in an unexported
  `Options` field so the environment Thunder binding reader shares it; an
  overlay cannot set it and gets no reader. The provider writes
  KV; the high-level client authors `SecretReference` CRs via OpenChoreo when
  `ManagesSecretReferences()` is false. Those CRs go in the Workload's
  control-plane namespace (not the vault `wc-…` path segment). Disconnect
  also best-effort deletes a leftover CR of the same name from `wc-…`
  (pre-fix authoring); NotFound is ignored.
- **Overlay / cloud** — overlay `main` injects its own provider (sm-api HTTP
  client) through `Options.SecretsProvider`. That client lives outside the
  public module; OSS CI does not exercise it. Coverage is the overlay's unit
  tests plus cloud dev — accepted trade-off (the public tree never had sm-api
  tests either).

One provider per process, chosen at construction. No fallback chain.

## OpenChoreo transport

`internal/clients/openchoreo` consumes the injected `RequestAuthStrategy`. Nil
strategy = **direct-OC mode** off-switch (`AuthModeServiceM2M`, never
pass-through). Same-class M2M cache invalidate + retry on 401 is preserved;
strategies must not retry with a different credential class.

`Options.ResourceLabels` rides the same client config: every create and update
(generated and hand-rolled clients alike, `resource_labels.go`) sets them on
the object's metadata, keeping every other label and overriding a same-key
one. It exists for a platform in front of OpenChoreo. wso2cloud's platform API
stamps `cloud.wso2.com/product-name` on a user-token write but not on an
impersonated one, which is how every background write (webhooks, sweeps, the
run supervisor) goes out, and its build workflow will not render a WorkflowRun
without it. The overlay sets `cloud.wso2.com/product-name: app-factory`, the
product wso2cloud maps both AEP clients to (its `PRODUCT_CLIENT_ID_MAP`).
Agent Manager solves the same gap the same way.

## Direct-OC mode (OSS)

`cmd/aep-api` calls `app.NewOSSOptions()` then `app.Run`:

- M2M `AuthProvider` when service-auth env is configured (else nil)
- `app.DirectOCStrategy{}` — always `AuthModeServiceM2M`
- `ImpersonateOrgResolver: nil` — no impersonation header
- `SecretsProvider` — OpenBao-direct when `OPENBAO_ADDR` is set (else nil)
- `ResourceLabels: nil` — direct OpenChoreo reads no platform labels

`PLATFORM_API_SERVICE_BASE_URL` points at OpenChoreo API directly.

## Overlay module + PAS strategy

An **overlay module** is a consumer of the public `app` package: its own `main`
builds `Options` and calls `Run`. Cloud-specific auth lives there as a **PAS
strategy** — a `RequestAuthStrategy` (plus impersonation resolver when needed)
that chooses user-JWT pass-through vs M2M + `X-Impersonate-Org` from request
context. Cloud secrets delivery is the overlay's sm-api provider on the same
`SecretsProvider` slot. The public tree keeps the seam contracts and the OSS
defaults; it does not embed cloud-only policy.
