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
- Seeds test users **`mark`** and **`emily`** (password `admin`), plus the
  default **`admin`/`admin`**

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
| `bootstrap/60-aep-console.yaml` | also seeds the CORS `server_config` for `:8090` |
| `bootstrap/60-aep-console.yaml` | the console OAuth app + test users |
