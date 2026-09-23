# @aep/ui-genui-demo

Dev page, not shipped. Renders with `@aep/ui-genui-oxygen` in Oxygen's Classic
theme (the one the Oxygen UI Storybook shows).

- **Components** (`?page=components`): every catalog component on its own:
  what it is for, its props (generated from the catalog schema), the JSON a
  model writes for it, and what that JSON renders. The catalog's actions and
  their params follow. The samples are `componentExamples` in
  `@aep/ui-genui/examples`.
- **Composed views** (`?page=views`): whole specs such as the console's Build
  view, rendered as one UI. Pick an example or paste model output; every
  action is logged.

```
pnpm --filter @aep/ui-genui-demo dev
```

**Create customer (form)** posts to `/api/customers`. With nothing else
running, the dev server answers with a stand-in (`dev/customers-api.ts`: 201,
400 with field errors, 409 for a taken name; "Acme" already exists). To use the
real API instead:

```
CUSTOMERS_API_URL=http://localhost:2001 pnpm --filter @aep/ui-genui-demo dev
```

The handler (`src/handlers.ts`) expects error bodies shaped
`{ message, fieldErrors }`; map the real API's shape there if it differs.
