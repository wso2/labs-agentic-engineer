# ADR-0002: Design systems implement the catalog in their own packages

## Context

The first implementation of the catalog was Oxygen UI, inside this package.
A second one, shadcn/ui, was wanted next, and the platform already treats the
UI design system as a per-organization choice (`skills/AGENTS.md`, "Swapping
the UI design system": Oxygen UI by default, Astryx available). The two
libraries have nothing in common underneath: Oxygen is MUI and Emotion, shadcn
is Radix primitives styled with Tailwind classes, copied into the project.

## Decision

- **One package per design system**: `@aep/ui-genui-oxygen`,
  `@aep/ui-genui-shadcn`. Each depends on `@aep/ui-genui` and its own library
  only, so a host installs one design system's dependencies and never
  another's. The core imports no design system, enforced by
  `src/boundary.test.ts`.
- **The contract is `GenUiDesignSystem`**: one implementation per catalog
  component (checked at compile time), the warning shown for an element that
  cannot render, and an optional root wrapper. `createGenUiView(designSystem)`
  turns it into a view; each package exports the result as `GenUiView`.
- **Shared behaviour is one test suite**: `describeGenUiConformance` in
  `@aep/ui-genui/testing`. Every design system runs it, so a new one is held to
  the same bar by construction. It asserts by text and role only.
- **Shared words live in the catalog**: the labels and tones for platform
  states (`src/catalog/labels.ts`) and the invalid-element message (written by
  the adapter). A design system decides only how a tone looks.
- **Implementations use each library's stock components unmodified.** Oxygen
  components take no styling overrides (colours, radii, sizes), so they follow
  the host's theme; `sx` is used only for layout. A component the library has
  no equivalent for (the agent timeline) is composed from its primitives and
  theme tokens.
  shadcn components are vendored exactly as the shadcn CLI writes them
  (`src/vendor/shadcn/`, MIT, excluded from the Apache header check).
- **shadcn ships a prebuilt, scoped stylesheet.** The package compiles its own
  Tailwind CSS: only the classes it uses, no global reset, shadcn's tokens on a
  `.genui-shadcn` root instead of `:root`. A host needs no Tailwind, and the
  sheet cannot restyle the host page.

## Consequences

- Adding a catalog component means implementing it once per design system.
  The compiler lists the missing ones, and the conformance suite covers them
  through the examples.
- A design system without a stock equivalent for a component gets the closest
  honest stand-in, documented where it is written. For example, shadcn has no
  success or warning badge colour and no code block with syntax highlighting.
- The shadcn stylesheet has to be rebuilt (`pnpm --filter
  @aep/ui-genui-shadcn build:css`) after a class changes; the demo's `dev`
  script builds it once at start.
- Tailwind still puts a handful of its own variables on `:root` (`--spacing`,
  `--font-sans`, …) because vendored classes name them directly. Theme values
  are otherwise inlined so they stay off `:root`.
