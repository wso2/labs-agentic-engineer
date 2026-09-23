# @aep/ui-genui

Generative UI for AEP: a model describes a screen as JSON, and a host renders
it with its design system, using only components and actions this package
allows.

This package is the design-system-neutral core. A design system implements
the catalog in its own package; today that is `@aep/ui-genui-oxygen` (WSO2
Oxygen UI), which renders in whatever theme the host's
`OxygenUIThemeProvider` sets.

```
            catalog (Zod)  ──►  adapter (json-render today)  ──►  design system
            what may appear      spec format · validation ·        how it looks
            what it may do       prompt · rendering                (own package)
```

## Use

Render a spec with the host's design system:

```tsx
import { validateGenUiSpec } from "@aep/ui-genui";
import { GenUiView } from "@aep/ui-genui-oxygen";

const result = validateGenUiSpec(modelOutput);
if (result.ok) {
  <GenUiView
    spec={result.spec}
    handlers={{ openTask: ({ taskNumber }) => navigate(`/tasks/${taskNumber}`) }}
    onActionOutcome={(outcome) => console.debug(outcome)}
  />;
}
```

Produce or check specs on a server (no React pulled in):

```ts
import { genUiSystemPrompt, validateGenUiSpec } from "@aep/ui-genui/headless";

const system = genUiSystemPrompt({ customRules: ["Keep it to one card."] });
```

A spec does not name a design system, so the same model output would render
with any design system added later. The prompt is the same for every call (about 4.5k tokens for the
current catalog), so it is a good fit for prompt caching. Every component and
description you add makes it bigger.

## Forms and feedback

A form in a spec never names a URL. It binds inputs to state, and a button
sends that state as an action's params; the host's handler makes the request.

```
TextField value {"$bindState": "/customer/name"}   typing writes state
Button press → createCustomer, params {"name": {"$state": "/customer/name"}, …}
  → dispatchGenUiAction   params checked against the action's Zod schema
  → host handler          e.g. POST /api/customers through the app's client
```

The view writes every action's progress to state at `/actions/<action>`
(`GenUiActionState`): `status` (`pending` → `success` | `error`), `message`
and `fieldErrors`. A spec shows it with `$state` bindings and `visible`
conditions (see `examples/create-customer.json`):

| Outcome | `status` | `message` | `fieldErrors` |
|---|---|---|---|
| Handler returned | `success` | — | — |
| Params failed the schema (no request sent) | `error` | "Check the highlighted fields." | the schema's message per param |
| Handler threw `GenUiActionError` (e.g. the server's 400/409) | `error` | its message | its field errors |
| Handler threw anything else | `error` | a generic message; details stay off screen | — |

The messages users see for bad input live in the action's schema, so the same
words appear whichever design system renders the form. `genUiSystemPrompt()`
teaches a model this contract. A failed action also rejects inside
json-render, so a spec's own `onSuccess` runs only on success and `onError` on
failure.

## What protects the host

- `validateGenUiSpec` rejects unknown component types, props that fail the
  component's schema (a `$`-expression is accepted where a value is expected),
  and broken structure (missing root, dangling child references).
- A view re-parses each element's props at render time, so a component only
  ever receives props that match its contract. A bad element shows the design
  system's warning instead of crashing; while `loading` (streaming) it renders
  nothing.
- Every action goes through `dispatchGenUiAction`: the action must be in the
  catalog and its params must pass its schema before a host handler runs.
- Deployment URLs must be `http(s)`, so a spec cannot plant a `javascript:`
  link.

## Change the catalog

1. Add the entry to `src/catalog/components.ts` (or `actions.ts`). Words a
   platform state shows on screen go in `src/catalog/labels.ts`, so every
   design system says the same thing.
2. Implement it in every design-system package. `GenUiImplementations` fails
   to compile in each one until it has an implementation.
3. Add its sample to `examples/components.ts` (it fails to compile without
   one), and use it in a composed example under `examples/` if it changes how
   whole views look. The conformance suite renders every sample and example,
   and the demo's Components page shows the sample with its props and JSON.

## Add a design system

1. Create `packages/ui/genui-<name>`, depending on `@aep/ui-genui` only (plus
   the design system itself).
2. Implement each catalog component against `GenUiRenderProps`, and export a
   `GenUiDesignSystem` (components, the `InvalidElement` warning, and an
   optional `Root` wrapper) plus `GenUiView = createGenUiView(yourDesignSystem)`.
3. Add one test file calling `describeGenUiConformance(yourDesignSystem)` from
   `@aep/ui-genui/testing`. It checks the behaviour every design system must
   show: rendering, action routing, state bindings, the shared wording, and
   the invalid-element fallback.
4. Add a panel for it in `@aep/ui-genui-demo`.

`src/boundary.test.ts` fails if this package ever imports a design system.
The reasoning is in `design/decisions/ADR-0002-design-systems-as-packages.md`.

## Swap the renderer library

Only `src/adapter/json-render/` imports `@json-render/*`, and
`src/boundary.test.ts` fails if that changes. To move to another library
(e.g. A2UI), add a sibling adapter directory that provides the same exports
(`createGenUiView`, `validateGenUiSpec`, `genUiSystemPrompt`, `GenUiSpec`),
then point `src/adapter/index.ts` and `src/adapter/headless.ts` at it. The
catalog and every design system stay as they are. Specs that were already
saved are in the old wire format and need converting.

## Try it

```
pnpm --filter @aep/ui-genui-demo dev
```

Opens two pages, in Oxygen UI's Classic theme: **Components** shows every
catalog component with its props, its JSON and what renders; **Composed
views** renders whole specs (such as the console's Build view) and logs every
action.
