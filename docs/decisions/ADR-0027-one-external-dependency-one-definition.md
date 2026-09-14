# ADR-0027 — One external dependency, one definition, with its contract beside it

## Status

Accepted. Supersedes ADR-0010 in part (the contract a design commits to).
ADR-0003 (read-time resolution) and ADR-0023 (values are a deploy gate) stand.

## Context

An external dependency used to be defined on every component that consumed it:
`style`, `specPath`, `candidates` and `config` sat in each `design.json`, the
platform unioned the config keys by name at value collection, and "identity"
was the bare name string. A `specPath` could be a URL the platform never
fetched, so a dependency read as resolved on the strength of a link the design
agent could not even open (a 256 KiB tool cap; Stripe's document is over six
megabytes). A dependency the agent could not identify had no route forward but
one chat round trip per dependency from the Build drawer, and no way to hand
the platform a document or to build on a stated assumption.

The walkthrough that prompted this ended with a finished design whose five
external dependencies could not be built against.

## Decision

**An external dependency is defined once, in its own directory, and resolved
means its contract is on disk.**

```
specs/design/dependencies/<name>/
  dependency.json        provider, style, config keys, open suggestions, contract ref, provenance, the user's `assumed` record
  openapi.yaml           the REST slice   (or schema.graphql for GraphQL)
  sdk.json               the SDK manifest (style sdk): a package per implementation language, docs, the calls used
```

- `<name>` is the one identifier: the directory, the cell's south node, every
  component's reference, and the org registry key. A component's `design.json`
  carries `{ "kind": "external", "name": "<name>" }` and nothing else of the
  definition; the write-gates refuse the old fields with a message naming the
  file they moved to.
- The platform hydrates every reference from the directory when it reads the
  design, so downstream readers keep the flat edge they had. It writes both
  halves back. A design from before the directory existed is lifted into one
  at its next save — the migration is the ordinary save.
- A **Registered External resource** gets the same directory, stamped by the
  platform at every design save from the org record (`source: "org"`), the way
  `wiring` is derived; the agent does not edit it and the build collects no
  values for it.
- **The contract is a slice**, not the provider's whole document: the
  operations the design uses plus every schema they reference, cut by a
  deterministic platform tool (`slice_openapi_spec`) outside any model's
  context, validated as a standalone document, and recorded with provenance
  (source URL, the full document's hash, when it was read). A user-supplied
  document goes through the same tool. If the agent cannot name the operations
  it needs, it does not understand the dependency well enough to resolve it.
- **An SDK dependency** carries a manifest (`sdk.json`, a package per
  implementation language) plus the API slice beside it when the provider has
  one; without one it is flagged `sdk-only`.
- **An assumed contract** is one the agent wrote from the provider's
  documentation when no document could be found or supplied. The file says so
  (`x-aep-assumed: true`; `"assumed": true` in an `sdk.json`). It counts only
  once a user accepts it: the platform records `assumed: { by, at, note }` in
  `dependency.json` — the one field the agent's write-gate refuses to author —
  and the dependency reads `needs-acceptance` until then, resolved and flagged
  `assumed` after. Replacing an assumption with a real document is the next
  design iteration.
- **State is still derived at read time** (ADR-0003), from the directory:
  org-stamped or registry-known → `resolved`, flagged `registered`; no
  provider (open `suggestions` or not) → `unresolved / needs-input`; a style
  with no contract or manifest on disk → `unresolved / needs-contract`; an
  unaccepted assumption → `unresolved / needs-acceptance`; otherwise
  `resolved`, flagged `assumed` / `derived` / `sdk-only`. The build gate blocks on
  `unresolved` and on nothing else: an assumed or SDK-only dependency builds,
  flagged wherever it appears. A definition with a contract on disk but no
  provider named (one from before providers were named) reads its provider
  off the document's title: handing over the document was the choice.
- **The user chooses the provider; the agent never does.** Requirements
  records what the business already holds — a Registered External resource of
  the org, written as a given without a question; a service the user already
  uses or must use, written as a settled Product Decision — and leaves every
  other capability unnamed. The design turn binds and researches only what
  the PRD names. An open capability is written as the *need*: name, purpose,
  and `suggestions` (services commonly used for it, from the agent's
  knowledge, any number, no research) — no provider, no config keys, since
  the keys follow the service. The retired `candidates` (two or more
  researched fits, the `ambiguous` state) read as suggestions; the fold
  refuses them on write. All research for an open dependency happens in the
  `resolve-dependency` flow, which the user starts from the definition
  (**Select a provider**) and which asks in the conversation — the
  suggestions as options, another provider as free text, or "find one for
  me" — never writing options back into the file. A need is named
  `<capability>-service`; the system chosen for it is its provider.
- **One ladder for an interface, wherever it is searched for** — the design
  turn for a provider the PRD gives, the resolve flow after the user selected
  one, never before a provider is chosen. Find a published document and slice
  it; failing that, **derive** one from the provider's own developer
  reference when it names every operation the design calls with parameters
  and responses (`x-aep-derived: true` at the root, `x-aep-source` on every
  operation, provenance at the reference) — resolved, flagged `derived`, no
  authorization asked; failing that, ask: a link, an upload, or an
  assumption.
- **The user's answer is the authorization.** When no published interface
  exists the flow asks how to get one — a link, an upload, or the agent's
  assumption. Choosing the assumption records the user's `assumed` record on
  the definition at that moment (a typed question option the console runs
  against the acceptance endpoint, which accepts a definition with no
  interface on disk yet); the agent then writes the assumed interface and
  carries the record along — and cannot drop it: both write gates put the
  record back from the file on disk when a write of the definition leaves it
  out, so a re-emission from a snapshot taken before the authorization, or a
  model that forgot it, loses nothing. Nothing further is asked. The gate
  still refuses an agent-authored record.

**The flow.** The design turn writes each dependency's directory — researched
for a provider the PRD names, the need alone otherwise — and ends by naming
what is open; it never blocks. Each dependency's directory is a group in the
spec rail, and its definition renders as the view where the user resolves it:
**Select a provider** runs the guided `resolve-dependency` flow, whose cards
ask which provider and, if need be, how to get its interface; **Resolve** runs
the flow for a chosen provider; a URL or a dropped file goes straight into the
directory. The design turn's closing list links each open definition. Build with open dependencies lists them and offers one
button that runs the flow over all of them.

## Consequences

- A coding agent reads the committed slice, never the web, for what the
  design decided; provenance says where to look for what the slice lacks.
- Two components using one provider share one file, one contract and one set
  of config keys; the union-by-name at value collection is gone.
- The contract in the repo is what validation checks against and what the
  cell diagram's south edge means.
- The agent's schema, the Go fold gate and the save-time schema validate the
  same file; the `assumed` record is echoed by the agent but only written by
  the platform.
- `specPath` is retired. A URL was never a contract.
