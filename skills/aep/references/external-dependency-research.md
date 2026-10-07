# Researching an `external` dependency

Read this file before you write the client of a `kind: external` dependency.
All the data that you need is in `specs/design/dependencies/<name>/`. Read its
`dependency.json` first. Do not follow links from it. The names of the config
keys are fixed, and their values are injected at runtime.

## What the design already decided

| In `dependency.json` | Meaning for you |
|---|---|
| `resource.provider` | the system that the design selected (for example "Stripe") |
| `resource.ref` | if set, this is the ORGANIZATION's registered resource, copied here with its keys, instructions and document. The values of the organization are injected under the same keys |
| `resource.consumptionInstructions` | how the organization wants the resource used. Obey it, and read it before the docs of the provider |
| `resource.contract.type` | `openapi` or `graphql`: write a client for the document in this directory. `sdk`: use a vendor library; `sdk.json` names the package for YOUR language and the calls that the design uses |
| `resource.contract.path` | the document in this directory. It is the WHOLE document, not a slice. If it and the docs do not agree, obey the document |
| `resource.contract.origin` | where the document came from (see below) |
| `provenance` | the source and the hash of the document. It is a record: do not fetch it |
| `resource.config[]` | the env-var keys that the component reads |
| `resource.description` | what the system is |

The values of `resource.contract.origin`:

- `registry` or `provider`: a published document. Trust it.
- `derived`: the design agent wrote the document from the developer reference
  of the provider. The `x-aep-source` of each operation names the page. Read
  that page when a detail is not clear.
- `assumed` (with `accepted`): the user accepted a document that has no
  published source.

For `derived` and `assumed`: build to the document as written, keep the
integration behind one adapter, and say in your report that the interface is
not from a published document.

If `resource.provider` is not set, or the contract document is not on disk,
the design has an open question. Report it, and do not implement the
dependency.

## The procedure

1. Slice the document first — never read it whole. It can be megabytes. Find
   the operations that this component calls (from the issue and the flows).
   Read only these operations and their schemas.
2. Read the description of the dependency. When `resource.ref` is set, read
   the instructions of the organization.
3. Use the docs of the provider only for what the document does not cover. If
   an operation is not in the document, it is a design gap: say so in your
   report. Do not fetch a different document.

These are design decisions, not yours: a different provider, a new name for a
`config` key, a change to its `secret` flag, or a value for a key.
