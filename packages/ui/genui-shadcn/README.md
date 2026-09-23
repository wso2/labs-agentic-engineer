# @aep/ui-genui-shadcn

The `@aep/ui-genui` catalog rendered with shadcn/ui (style `radix-nova`, base
colour `neutral`, the CLI's current defaults).

```tsx
import { GenUiView } from "@aep/ui-genui-shadcn";
import "@aep/ui-genui-shadcn/styles.css"; // once, anywhere in the host

<GenUiView spec={spec} handlers={handlers} />;
```

- **No Tailwind needed in the host.** `styles.css` is prebuilt from
  `src/styles.css` and holds only the classes this package uses. Every rule is
  scoped to the `.genui-shadcn` root the view renders, with no global reset,
  so it cannot restyle the host page.
- **Dark mode**: a `dark` class on any ancestor, as in shadcn.
- **Re-theming**: override shadcn's variables (`--primary`, `--radius`, …) on
  `.genui-shadcn`.
- **Fonts**: the sheet names Geist but does not bundle it; without it the
  system sans-serif is used.

The shadcn components themselves are in `src/vendor/shadcn/`, exactly as the
shadcn CLI wrote them (MIT). `src/components/` maps the catalog onto them.
Where shadcn has no stock equivalent, the stand-in is noted in the code:
tones without a badge colour (success, warning) use the nearest stock
variant, and code snippets are not syntax-highlighted.

After changing a class, rebuild the stylesheet:

```
pnpm --filter @aep/ui-genui-shadcn build:css
```

Tests: `src/conformance.test.tsx` runs the shared suite from
`@aep/ui-genui/testing`.
