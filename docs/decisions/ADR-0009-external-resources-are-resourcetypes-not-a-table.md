# ADR-0009 — External resources are org-namespaced OpenChoreo ResourceTypes, not a database table

An **external resource** is a provisioned third-party integration (a REST API
or SDK such as Stripe or OpenWeatherMap) together with its config-key schema —
which env-var keys it reads and which are secret. The org needs a **registry**
of these so the designing agent can reuse an already-registered external
(name + schema) across designs and projects instead of re-proposing it, and so
the Resources catalog (`/resources`) can list them.

The original design backed that registry with a Postgres `external_resources`
table (one row per `(org_id, name)`, upserted at design save). But OpenChoreo
already models a resource as a namespaced `ResourceType` → `Resource` →
`ResourceReleaseBinding`, and platform resources already use the cluster-scoped
`ClusterResourceType` as their catalog (ADR-0007). A parallel DB table is a
**second source of truth** for the same concept: it duplicates the schema, is
asymmetric with platform resources, and drifts from what is actually
provisioned in the cluster.

## Decision

The **org-namespaced OpenChoreo `ResourceType` IS the external-resource
registry.** There is no `external_resources` table. Each external resource's
ResourceType is **self-describing**:

| Carrier | Holds |
|---|---|
| `metadata.name` | deterministic `<name>-<shortHash(sorted key+secret)>-t<templateVersion>` — same schema ⇒ same name (stable get-or-create); a changed key/secret mints a new one (RTs are immutable) |
| `aep.wso2.com/external-name` / `aep.wso2.com/description` annotations | the logical name + description (OC has no native description field) |
| `spec.parameters` | the config-key schema (per-key description/default) |
| `spec.outputs` | secret classification — `secretKeyRef` = secret, `configMapKeyRef` = plain |

The RT is authored at **provision time** for a Project External (`EnsureResourceType`, get-or-create), and at **register** for a Registered External (ADR-0021).
Reads — the MCP `list_external_resources` / `get_external_resource_schema`
discovery tools and Resources (`/resources`) — reconstruct the definition
straight from the RTs via `openchoreo.ExternalDefinitionFromRT`. Secret
classification for provisioning + build reads the component's committed
`design.json` `config[]`, unioned across consumers with secret-wins — never the
registry. This keeps the external RT symmetric with the platform-resource
`ClusterResourceType` and leaves OpenChoreo as the single source of truth.

## Consequence — provision-gated reuse

Because a Project External's RT is authored at provision, one that has been
*designed* but *never provisioned* is not yet in the registry. A Registered
External is listed from register, before any project consumes it (ADR-0021).
Resolution correctness is identical (a design carries its own full `config[]`
regardless); only the *timing* of cross-design reuse differs, and it buys a
single source of truth with no table to keep in sync. Editing only an external's description does
not re-author its RT (the name hashes over keys, not description); the
description lives on the existing RT's annotation.

Related: ADR-0003 (resolution is read-time), ADR-0007 (resource-type behavior
keys on `aep.wso2.com/*` markers, not names), ADR-0021 (a **Registered External
resource** is authored at register, marked by consumption instructions on this
RT — not only at first provision).
