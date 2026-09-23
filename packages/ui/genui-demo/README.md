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
