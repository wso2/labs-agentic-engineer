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
- **Chat** (`?page=chat`): a scripted agent asks with a form card (select,
  text, checkbox, textarea). Answers go back as `replyToAgent`; the agent
  checks them against the form it sent (`src/chat/formRules.ts`) and replies
  with a confirmation card, checked with `validateGenUiSpec` before it is
  posted. Every card can show the JSON the agent sent.

```
pnpm --filter @aep/ui-genui-demo dev
```

**Customers (list + form)** loads `GET /api/customers` in the host
(`src/customers.ts`) and passes it to the view as state; after a create, the
handler reloads it. **Create customer (form)** and the Customers form post to
`/api/customers`. With nothing else
running, the dev server answers with a stand-in (`dev/customers-api.ts`: GET
lists, POST answers 201, 400 with field errors, or 409 for a taken name;
"Acme" already exists). To use the
real API instead:

```
CUSTOMERS_API_URL=http://localhost:2001 pnpm --filter @aep/ui-genui-demo dev
```

The handler (`src/handlers.ts`) expects error bodies shaped
`{ message, fieldErrors }`; map the real API's shape there if it differs.
