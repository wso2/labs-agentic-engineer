# ADR-0041 — Platform-resource facts are derived by one function, with a narrow resource-type port

**Status:** Accepted (2026-10-05)
**Related:** [playground ADR-0003](../../playground/design/decisions/ADR-0003-code-run-derives-wiring-with-aep-api.md)
(`code` runs this derivation) ·
[ADR-0027](ADR-0027-one-external-dependency-one-definition.md)
(legacy carriers)

## Context

Before POST /build cuts a tag, aep-api derives a design's platform-resource
facts (`exposesAPI.auth` and every dependency's `wiring`) and commits them into
each `design.json`. The playground never builds, so it needs the same facts
without a cluster or a commit. A second implementation would drift from the
first, and the design reader (the coding agent) cannot tell a drifted design
from a correct one.

The derivation reads the installed resource types. Those belong to the
dependencies domain, and the spec domain names no other domain's entity, even
in a port.

## Decision

1. **`spec.DerivePlatformResourceFacts` is the one implementation.** It is a
   pure function over the assembled design and a resource-type map: it checks
   membership, derives auth, then wiring, and returns the files whose derived
   state changed, rendered. POST /build's `persistPlatformResourceDerivation`
   commits that list; `cmd/design-derive` writes it to a directory. Neither
   re-implements a rule, an order or a render.
2. **Refusal policy.** An unknown resource type (`ErrUnknownResourceType`) or an
   explicit end-user-auth conflict (`ErrEndUserAuthConflict`) refuses the save,
   with nothing mutated and nothing committed, like the unresolved-dependency
   proceed gate. Wiring never rejects: an underivable dependency gets absent
   wiring, which the coding agent reports, rather than a design the platform
   refuses to save.
3. **Spec sees resource types only through a port.** Spec's
   `resourceTypeCatalog` port returns `spec.CRTType`, spec's own vocabulary for
   the markers and outputs it reads. `internal/app/crtcatalog` projects the
   dependencies `ResourceTypeCatalog` onto it, so spec names no dependencies
   entity.
4. **`crtcatalog` is its own package**, not an adapter inside `internal/app`, so
   `cmd/design-derive` projects a catalog through the code the composition root
   wires without importing the service graph.
5. **`openchoreo.ResourceTypeDir` is the cluster-less source.** It reads the
   same ClusterResourceType CRs from manifest files
   (`deployments/single-cluster/resource-types/*/resourcetype.yaml`) into the
   OC client's own struct, behind the `dependencies.ClusterResourceTypeSource`
   interface the live client also satisfies.

## Consequences

- A rule change lands once and reaches both callers. `cmd/design-derive`'s
  oracle test checks the CLI's output byte for byte against design files the
  platform committed.
- A derivation re-run on unchanged input returns nothing, so a re-save commits
  nothing and the CLI is idempotent.
- `ResourceTypeDir` treats an empty directory as an error: an empty catalog
  means "disabled" to the derivation and would skip the membership check.
- The repo manifests are now a second source of resource types. A type
  installed on a cluster but missing from them is unknown to the CLI.
