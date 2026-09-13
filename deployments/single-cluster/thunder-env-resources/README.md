# AEP's bootstrap bundle for an environment Thunder

Declarative ThunderID resource documents that give **AEP** its own identity on a
per-environment Thunder — the second identity tier, one Thunder per (org,
environment). `scripts/setup-environment-thunder.sh` renders them and mounts them
into that instance's bootstrap ConfigMap.

The sibling directory `thunder-resources/` is the same idea one tier up: AEP's
bundle for the shared platform IdP. Same publisher contract, different instance.

## The two tiers, and who owns what

| | platform IdP (T1) | environment Thunder (T2) |
|---|---|---|
| how many | one per cluster | one per (org, environment) |
| who logs in | humans, into the platform console | agent and workload identities, plus a version's roles and test users signing in to the generated app (ADR-0022) |
| installed by | `scripts/setup-thunder.sh` | `scripts/setup-environment-thunder.sh`, or Agent Manager's `add-environment-thunder.sh` |
| AEP's bundle | `thunder-resources/` | this directory |

Neither tier belongs to a product. Both are platform infrastructure that AEP and
Agent Manager **publish into**. A T2 may therefore already exist when AEP first
reaches it — Agent Manager created it — and AEP binds to that instance rather
than provisioning a second one.

## The publisher contract

Stated in full in `deployments/design/two-tier-thunder.md`. It bites harder at
this tier because the instance is more often someone else's: every object this
directory declares is `aep-`prefixed and its files start at 80 (after
ThunderID's shipped `01-` defaults and Agent Manager's 10-13), nothing here is a
`server_config` — the singletons every T2 needs are rendered by
`setup-environment-thunder.sh` from Agent Manager's own `render_*` functions, so
the two products cannot produce different versions of them — and every
`aep-system-client` mint sends `resource=<issuer>/mcp`, which the binding record
publishes as `systemResourceIdentifier`.

One consequence is worth spelling out, because it differs from the bundle one
tier up. AEP's role here (`84-aep-system-role.yaml`) is a new `aep-`prefixed
role rather than an assignment appended to ThunderID's built-in Administrator
role, which is the shortcut the platform-IdP bundle takes: a document naming an
object another publisher declared replaces it wholesale. A T2 shared by both
products therefore carries two small roles, one per publisher, each removable
without touching the other.

## Templating

The files are `envsubst`-templated with an explicit variable list, the same way
`setup-thunder.sh` templates `thunder-resources/`:

| variable | is |
|---|---|
| `${AEP_SYSTEM_CLIENT_SECRET}` | the per-environment secret for `aep-system-client`, generated once and then reused from the Secret |
| `${ORG_NAME}`, `${ENV_NAME}` | the environment this instance belongs to (descriptions only) |

Anything else that looks like a shell variable is left alone, because the list is
explicit. The secret is single-quoted YAML in the document, whose only escape
rule is a doubled quote, and the generated value is hex — so there is nothing to
escape in practice.

## Format

Each file is one document with a `resource_type` header, shaped like Agent
Manager's own — see `add-environment-thunder.sh`'s `render_*` functions for the
reference set.

`ouId: "01900000-0000-7000-8000-000000000001"` is ThunderID's built-in `default`
organization unit, fixed on every install. Applications must use `ouId` and not
`ouHandle`: the importer resolves the handle for roles, groups and users but NOT
for applications, and an application bootstrapped with `ouHandle` ends up with no
OU at all.

`attributes: ["ouId", "ouHandle"]` under `token.accessToken.clientConfig` is
separately required: ThunderID only embeds those as token claims when the client
opts in.

`resourceServerId: "01900000-0000-7000-8000-000000000020"` is ThunderID's own
System resource server, fixed on every install. Only its *identifier* is per
instance, and that is the platform layer's document, not one of these.

## Operating notes

**The bootstrap importer only runs on install.** ThunderID's setup Job is a
`helm.sh/hook: pre-install` hook, so editing a file here and re-running Helm
changes the mounted ConfigMap and nothing else — it looks like the edit landed
and it did not. `setup-environment-thunder.sh` re-runs the importer as a plain
Job when a mint proves `aep-system-client` is missing. To force a re-import after
editing a document, delete the Secret holding the client's credential
(`<release>-aep-system-client` in the T2 namespace) and re-run the script: the
mint then fails, and the bundle is published again with a fresh secret.

**The importer re-runs ThunderID's shipped defaults too.** Bootstrap pattern 2
mounts these files *alongside* `/opt/thunderid/bootstrap`'s built-ins, which
include a System resource server whose identifier is hardcoded to
`https://localhost:8090/mcp`. Any re-import bundle must therefore also carry the
identifier fix, or the next mint fails with `invalid_target`. That is why
`setup-environment-thunder.sh` renders the platform documents into every bundle
it builds, not only the install-time one.

**Where the credential ends up.** Not in this directory and not in Git: the
secret is generated per environment and written to the Secret in the T2
namespace, mirrored into `thunder-app-operator-system`, and stored in OpenBao at
`secret/aep/thunder/<org>/<env>`. `setup-environment-thunder.sh`'s header
documents which consumer reads which copy.
