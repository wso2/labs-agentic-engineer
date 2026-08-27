# ADR-0021 — A Registered External is marked on its ResourceType, not in process memory

Register writes org-held values into OpenBao and also into a process-local value
plane so list and Build preflight can treat the name as already configured. That
plane dies when aep-api restarts. The catalog ResourceType and the vault record
survive. Treating empty process memory as “not Registered” made Build ask for
GITHUB_TOKEN again on a name the org had already registered.

## Decision

**Consumption instructions on the ResourceType mark a Registered External
resource.** Register always writes them (`aep.wso2.com/consumption-instructions`).
Project External provision authors the RT with them empty. Terms:
[CONTEXT.md](../../CONTEXT.md).

The process-local org value plane is a cache. After restart, list and preflight
reconstitute configured cells from the RT and reconstruct the org-catalog vault
path (`OrgCatalogVaultKey`) — they do not re-write secrets.

A new project that needs an already-registered API reuses that exact name.
Architecture prefers the catalog; preflight emits no `external-config` collect
for those names. A Project External remains when nothing in the catalog fits, or
the user asks to reconsider — the fold does not reject a new name.

## Alternatives considered

**A dedicated `kind` / boolean on the ResourceType.** A second carrier beside
consumption instructions, which Register already requires. One field would then
disagree with the other.

**Persist the value plane.** A second store for “this name is org-held” next to
the RT that already is the registry (ADR-0009).

**Empty cells mean collect again.** The restart bug: durable secrets, amnesiac
identity.

## Consequences

- A Project External must not grow fake consumption instructions — that would
  make it Registered.
- `HasOrgEnvCells` names the preflight question (are org values already held?),
  not the RT scan that answers it after restart.
- Rebuild of aep-api still needs the request JWT’s `ouId` to reconstruct the
  vault path; a missing claim cannot invent one.
