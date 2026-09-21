# ADR-0030 — A dependency holds a full copy of its resource, and a project's resource belongs to its project

**Status:** Accepted · 2026-09-17 · **Exposed by:** `Dependency`, `ResourceDefinition`,
`ResourceContract`, `ResourceProvenance`, `ExternalResourceDTO.scope`,
`RegisterExternalResourceRequest.provider/contract` in
`packages/contracts/api/v1/openapi.yaml`; the file shape in
`packages/agent-stream/src/contracts/dependency-design.ts` · **Amends:**
[ADR-0009](ADR-0009-external-resources-are-resourcetypes-not-a-table.md),
[ADR-0021](ADR-0021-registered-external-is-marked-on-the-resourcetype.md),
[ADR-0027](ADR-0027-one-external-dependency-one-definition.md) · **Resolves:** #755 and the
identity half of #774 · **Terms:** [CONTEXT.md](../../CONTEXT.md)

## Context

Two projects, one brief, two days apart. The first defined `currency-service`
for itself; its build authored an org-namespaced ResourceType under that bare
name. The second found the name in the org catalog, wrote `{ "source": "org" }`,
read as resolved on every surface — PRD, rail, definition, the Build gate — and
failed provisioning at 0m52s: `external resourcetype "currency-service": at least
one config key required`. Three things had gone wrong at once, and none of them
was the error message.

1. **The registry could not tell whose a record was.** A type a project's build
   left behind and a type an organization registered were the same object, told
   apart only by whether consumption instructions happened to be set. The
   catalog listed both; the design agent reused both; Register refused both
   (#774).
2. **A "copy" carried nothing.** `source: org` was a promise that the platform
   fills provider, contract and config from the org record. The record had no
   provider and no contract to fill from, and the fill was never built (#755).
   The coding agent — which reads the repo and nothing else — would have had no
   keys even if the build had passed.
3. **The org record and the project definition had different shapes.** The
   registry held name, description, keys and instructions; the definition held
   provider, style, a contract *slice* and provenance. Nothing could be copied
   in either direction without loss, so promotion (project → org) could not be
   built either.

The design that closed #755 on 14 September — copy the keys onto the stub at
the design turn, gated on consumption instructions — would not have fixed the
recorded failure: the record was not Registered, so the copy would have skipped
and the build would have refused in the same line.

## Decision

**The registry holds resources. A dependency is one project's use of a
resource, and it holds a full copy.**

1. **One resource shape at both levels.** `ResourceDefinition` — name,
   description, provider, config keys, `contract { type, path }`, consumption
   instructions, provenance — is the org registry record (stored on the
   ResourceType) and the `resource` block of a project's `dependency.json`.
   Style is not stored anywhere: it is computed from the contract's type.
2. **A project holds a full copy, with `ref` kept.** The design agent asks for a
   registered resource by writing a stub, `{ "name", "resource": { "ref", "name" } }`;
   the platform completes it at the design write (`FilesService.Apply`, the
   single `specs/` chokepoint): the record's block, the record's contract
   document copied beside the file byte for byte, and provenance naming the
   registry file and its hash. The coding agent reads one file and follows no
   link. A reference-only file with a snapshot at the version cut was
   considered and rejected: the coding agent needs the keys in the repo, and a
   snapshot is a second place for the truth to live.
3. **The contract is one whole document, never a slice and never a URL.**
   `{ type, path }`, the path relative to the level's store — the org docs repo
   for a record, the dependency directory for a copy. An internet address is
   provenance, the place a copy came from; the platform fetches a document the
   agent names by URL (https, public hosts, 5 MiB) and lands it at save. The
   coding agent cuts what its component calls at coding time. `contract.origin`
   (registry · provider · derived · assumed) replaces the file markers, and
   `contract.accepted` replaces the top-level `assumed` record.
4. **A project's resource belongs to its project.** The type a project's build
   authors carries `aep.wso2.com/scope: project` and the project's name, and
   folds the project into the type's name so it can never collide with a
   registered type. Org-level reads — the Resources page, the design agent's
   catalog, Register's uniqueness check, Delete — see `scope: org` only. A
   type from before the markers is judged by its consumption instructions (the
   ADR-0021 rule), which stays the fallback.
5. **Status asks the registry only for a copy.** `ref` set and a registered
   resource of that name exists → the copy stands and only its contract is
   checked; `ref` set and none → needs-input, "The organization has no registered
   resource with this name", the Select a provider card, the file untouched;
   the rest of the ladder as before minus the style rule. A copy whose document
   hash no longer matches the registry's is flagged `stale`, a flag and not a
   status.
6. **Scope is chosen late, and promoted from the registry.** Every designed
   dependency is project level; no question is asked at design or configure
   time. The organization's Resources page lists a project's own resources
   beside the records ("held by <project>"), and *Promote to organization*
   lives on that row — not on the project's definition view: the person who
   curates the registry decides what the organization holds, where the
   records are. Promote takes the project's block as the record, adds
   instructions and every environment's value (a value the project already
   holds is carried over vault to vault), then rewrites the project's file
   through the same renderer a fresh reuse runs, so the copy is byte-identical
   to one a design turn would have landed.

## Alternatives considered

- **Resolve the keys at read time** (PR #756). Rejected: the coding agent reads
  the repo, and a read-time fill is invisible to it.
- **Copy at the design turn, gated on the record being Registered** (the
  14 September design). Superseded: correct for a genuine registration, blind
  to the recorded failure, and it left two shapes that could not round-trip.
- **A reference in the spec, a snapshot in the version tag.** Rejected: two
  places for one truth, and a new write into the tag with no precedent.
- **Hide project types from the catalog but leave the bare-name collision.**
  Rejected: a hidden resource that still refuses a registration is a 409 the
  admin cannot explain.
- **Keep the slice.** Rejected: the design does not know which operations the
  code will call; the whole document is what the registry can share and what a
  hash can compare.

## Consequences

- The previous flat file shape still reads (lifted in memory) and is rewritten
  at its next save; `x-aep-*` file markers are read only when a contract has no
  origin.
- Register takes a provider and a contract document (URL or upload); the
  document lands in `org-resource-docs`, at most 5 MiB.
- The coding agent's skill gains a hard rule: slice the document first, never
  load it whole.
- Two things stay open: refreshing a copy when the org record's prose changes
  (#773; the hash covers the document only), and types created before the
  scope marker, which the instructions fallback hides but nobody can register
  over yet.
