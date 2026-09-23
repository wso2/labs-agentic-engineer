# Design notes — `@aep/prototype-model`

The one definition of a web application's `prototype.json`: what a valid
prototype is, how it is read, and how it is written back. Why the prototype
exists, where it sits in the stage order (an optional review step after Design,
beside the wireframes) and why Build does not ask for it is repo
[ADR-0034](../../../docs/decisions/ADR-0034-a-web-application-is-reviewed-as-a-prototype-before-build.md);
how the console presents it is console
[ADR-0033](../../../apps/console/design/decisions/ADR-0033-preview-and-annotate-are-one-prototype-view.md).

## Who reads it

| consumer | uses |
|---|---|
| the agent write gate (`@aep/agent-stream`'s `checkPrototype`) | `parsePrototypeModel` with the directory's component, so a write the model refuses never reaches the turn ledger |
| the console (`features/prototype`) | `parsePrototypeModel` on the file read through `read-file`, the types for the renderer and reducer, the artifact-path helpers for the rail |
| the Go save gate (`services/aep-api/internal/platform/prototypespec`) | the generated JSON Schema, vendored, plus a Go twin of the reference pass |
| the `prototype` skill's test (`services/agents`) | the node and action kinds, and the gate, against the skill's registry table and worked example |

## What it owns

- **`model.ts`** — the version 1 types: a closed registry of node kinds, seven
  actions that change view state only, screens with optional overlays, shared
  navigation, roles, display states and flows. No markup, style, script or
  custom-component node exists to be generated.
- **`schema.ts`** — the Zod schema, every object strict.
- **`parse.ts`** — `stablePrototypeJson`, the deterministic serializer (parse,
  serialize, parse, serialize is byte-equal, which is what lets a feedback
  rewrite be diffed and keeps the mock fixtures canonical), and
  `parsePrototypeModel`, the one entry point, in three stages that stop at the
  first failure: the version check (an unsupported
  `schemaVersion` is one `UNSUPPORTED_VERSION` issue, not a schema dump), the
  structural pass, then the reference pass.
- **`references.ts`** — the rules JSON Schema cannot express. Every ID goes into
  one namespace first; any `DUPLICATE_ID` ends validation, because a reference
  into an ambiguous namespace has no single target. Then every reference must
  name an entry of the right kind, on the screen the action runs on for
  screen-local targets (tabs, steppers, tables, overlays): otherwise
  `UNKNOWN_REFERENCE`. With a component given, `component` must equal it
  (`PROTOTYPE_COMPONENT_MISMATCH`).
- **`issues.ts`** — the codes and the JSON-path spelling (`screens[0].content[2].id`).
  Both are a contract with the Go gate: `test/validation-cases.json` is the one
  case table both sides assert.
- **`artifact-path.ts`** — `specs/design/components/<component>/prototype.json`,
  and its inverse. Everything that places or recognizes a prototype goes through
  it.
- **`fixtures/`** — two realistic enterprise prototypes (expense approval,
  integration monitor) that between them use every node kind and every action.
  They are the package's acceptance fixtures and the console's test data.

## The published schema

`pnpm --filter @aep/prototype-model gen` renders the Zod schema as JSON Schema
(draft 2020-12) into `packages/contracts/schemas/prototype-model.schema.json`;
`test/json-schema.test.ts` fails when the checked-in file is stale. The Go
package vendors a copy behind its own anti-drift test, the same arrangement as
`security-design.schema.json`.

## Changing the model

The version is part of the contract. A new node kind or action is a change to
the model, the schema artifact, the Go vendor copy and reference twin, the
console's registry (a mapped type, so a missing renderer is a compile error)
and the `prototype` skill's registry table (a test pins it). A change that an
existing v1 file could fail is a new `schemaVersion`, and a reader that meets
an unknown version renders nothing rather than guessing.
