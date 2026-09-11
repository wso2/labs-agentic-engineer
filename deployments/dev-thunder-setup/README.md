# dev-thunder-setup

A standalone [Thunder](https://thunderid.dev/) identity provider for
developing the console's SSO flow (issue #91) without the k3d cluster.

> **Version note.** This harness still runs the Thunder 0.34 line and its
> shell-script bootstrap format. The cluster moved to ThunderID 1.0.0 with a
> declarative bootstrap (see `../single-cluster/thunder-resources/`), so the two
> no longer share a seed format. It remains useful for console SSO work — the
> OAuth surface the console talks to is unchanged — but it is no longer a
> faithful mirror of what the cluster runs.

- Plain HTTP on **http://localhost:8097** (the console dev server owns 8090)
- Seeds **`aep-console-client`** — public PKCE, `authorization_code` +
  `refresh_token`, redirect `http://localhost:8090/callback`, `ou*` claims
  on both tokens — mirroring the cluster seed, which now lives in
  `../single-cluster/thunder-resources/87-aep-console-app.yaml`
- `aep-console-client` restricts login to the **`AEUser`** type (not
  Thunder's built-in `Person`) — so its own `ADMIN_USERNAME`/`ADMIN_PASSWORD`
  superadmin (see `docker-compose.yaml`) cannot sign in to the console
- Seeds test users **`mark`** and **`emily`** (password `admin`), plus the
  default admin login **`aeadmin`/`admin`** — all three type `AEUser`
- Seeds the **`ae`** resource server, its 8 AE permission actions, and the
  **`ae-admin`**/**`ae-developer`** groups + roles — mirroring
  `services/aep-api/internal/authz/role_permissions_catalog.go`, so a real
  Thunder-issued token can be tested against aep-api's permission gate.
  **`aeadmin`** is seeded as a member of **`ae-admin`** (all 8 permissions);
  `mark`/`emily` hold no AE role by default

## Run

```bash
cd deployments/dev-thunder-setup
docker compose up -d
```

First start runs a one-shot setup job (applies `bootstrap/*.yaml`), then the
server. State persists in named volumes; `docker compose down -v` resets.

## Use with the console

```bash
cd apps/console
VITE_API_MODE=mock VITE_AUTH_MODE=thunder pnpm dev
```

Real OIDC login against this Thunder, MSW still serving the APIs — the
login machinery in isolation (issue #91 dev topology). OIDC discovery:
`http://localhost:8097/.well-known/openid-configuration`.

## Files

| File | Purpose |
|---|---|
| `docker-compose.yaml` | db-init → setup (bootstrap) → server, image pinned |
| `deployment.yaml` | Thunder config: `http_only`, public URL `:8097` |
| `bootstrap/60-aep-console.yaml` | the console OAuth app + test users, and the CORS `server_config` for `:8090` |
| `bootstrap/61-ae-roles.yaml` | the `ae` resource server + actions + `ae-admin`/`ae-developer` groups + roles |
