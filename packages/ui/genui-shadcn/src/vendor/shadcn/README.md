# Vendored shadcn/ui

Written by the shadcn CLI, never by hand. Style `radix-nova`, base colour
`neutral` (see `components.json` at the package root). The files keep shadcn's
MIT license (`LICENSE.md`), which is why `make license-check` skips `vendor/`.

To update or add a component, from the package root:

```
npx shadcn@latest add <component> --overwrite
```

Then re-run this package's tests: the conformance suite catches a component
whose API changed under us.
