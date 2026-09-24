# Composed ThunderID bootstrap documents

ThunderID imports its bootstrap folder in filename order and upserts each
document. Four of the settings involved exist **once for the whole server**, and
declaring one replaces its entire value rather than adding to it. Two products
publishing into one IdP therefore cannot both hold them: the last filename read
wins, and nothing chooses which that is.

The documents here are that choice, made deliberately. Each carries both
products' needs and is numbered `99-` so it is imported last whatever either
side numbers its own.

## Assembling the bundle

The bundle is the folder `setup-env-for-aectl.sh` builds (`bootstrap.configMap`
on the ThunderID release), plus Agent Manager's documents, minus the ones these
replace.

| From | Action |
|---|---|
| `95-cors.yaml` (ours) | drop — replaced by `99-composed-cors.yaml` |
| `71-fix-system-resource-server-identifier.yaml` (ours) | drop — replaced by `99-composed-system-resource-server.yaml` |
| `71-amp-cors-config.yaml` | drop — merged into `99-composed-cors.yaml` |
| `73-amp-csp-config.yaml` | drop — carried by `99-composed-csp.yaml` |
| `70-fix-thunder-system-rs-identifier.yaml` | drop — merged into `99-composed-system-resource-server.yaml` |
| `69-amp-default-resource-server-config.yaml` | **keep as-is** — see below |
| everything else Agent Manager ships | keep as-is — users, groups, roles and applications are additive, not singletons |

Agent Manager's documents come out of the rendered chart, which installs no
Thunder of its own:

```
helm template amp oci://ghcr.io/wso2/wso2-amp-thunder-extension \
  --version 1.0.0-rc2 --set thunder.enabled=false
```

## `defaultResourceServer` is not composed

Agent Manager sets it to `amp-resource-server` so its own callers can request
scopes without an OAuth `resource` indicator. AEP publishes no competing
document, and its two clients — `aectl` and `aep-api` — always send the
indicator explicitly, so neither reads the default. Nothing to merge: Agent
Manager's document is kept unchanged.

## When Agent Manager's chart version moves

These values were composed against `wso2-amp-thunder-extension` 1.0.0-rc2.
Re-render the chart and re-compose: the CSP value and the System resource
server's resources tree are Agent Manager's verbatim, and the CORS list is a
union. The identifier in `99-composed-system-resource-server.yaml` is this
deployment's own and must keep matching `thunder.public_url`
(`skaffold/defaults.yaml`) — it is what `aectl` and `aep-api` send as the OAuth
`resource` indicator.
