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

Compile-time `var _ ocauth.RequestAuthStrategy = …` / `var _ secretsprovider.Provider = …`
assertions keep seam implementations honest.

## Secrets delivery

One seam, two providers, no stub:

- **OSS / local** — `NewOSSOptions` constructs the in-process OpenBao-direct
  provider when `OPENBAO_ADDR` (and `OPENBAO_TOKEN`) are set. The provider writes
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

## Direct-OC mode (OSS)

`cmd/aep-api` calls `app.NewOSSOptions()` then `app.Run`:

- M2M `AuthProvider` when service-auth env is configured (else nil)
- `app.DirectOCStrategy{}` — always `AuthModeServiceM2M`
- `ImpersonateOrgResolver: nil` — no impersonation header
- `SecretsProvider` — OpenBao-direct when `OPENBAO_ADDR` is set (else nil)

`PLATFORM_API_SERVICE_BASE_URL` points at OpenChoreo API directly.

## Overlay module + PAS strategy

An **overlay module** is a consumer of the public `app` package: its own `main`
builds `Options` and calls `Run`. Cloud-specific auth lives there as a **PAS
strategy** — a `RequestAuthStrategy` (plus impersonation resolver when needed)
that chooses user-JWT pass-through vs M2M + `X-Impersonate-Org` from request
context. Cloud secrets delivery is the overlay's sm-api provider on the same
`SecretsProvider` slot. The public tree keeps the seam contracts and the OSS
defaults; it does not embed cloud-only policy.
