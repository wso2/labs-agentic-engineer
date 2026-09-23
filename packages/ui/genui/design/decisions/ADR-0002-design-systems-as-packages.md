# ADR-0002: Design systems implement the catalog in their own packages

## Context

The first implementation of the catalog was Oxygen UI, inside this package.
The platform already treats the UI design system as a per-organization choice
(`skills/AGENTS.md`, "Swapping the UI design system": Oxygen UI by default,
Astryx available), so generated UIs may need a second design system later.
Design systems share nothing underneath, and a host should not install one it
does not use.

## Decision

- **One package per design system.** `@aep/ui-genui-oxygen` is the only one
  today. It depends on `@aep/ui-genui` and Oxygen UI; the core imports no
  design system, enforced by `src/boundary.test.ts`.
- **The contract is `GenUiDesignSystem`**: one implementation per catalog
  component (checked at compile time), the warning shown for an element that
  cannot render, and an optional root wrapper. `createGenUiView(designSystem)`
  turns it into a view; the package exports the result as `GenUiView`.
- **Shared behaviour is one test suite**: `describeGenUiConformance` in
  `@aep/ui-genui/testing`. A design-system package runs it from one test file,
  so any future one is held to the same bar. It asserts by text and role only.
- **Shared words live in the catalog**: the labels and tones for platform
  states (`src/catalog/labels.ts`) and the invalid-element message (written by
  the adapter). A design system decides only how a tone looks.
- **Implementations use the library's stock components unmodified.** Oxygen
  components take no styling overrides (colours, radii, sizes), so they follow
  the host's theme; `sx` is used only for layout. A component the library has
  no equivalent for (the agent timeline) is composed from its primitives and
  theme tokens.

## Consequences

- Adding a catalog component means implementing it in every design-system
  package. The compiler lists the missing ones, and the conformance suite
  covers them through the examples.
- Adding a design system is a new package that implements the contract and
  runs the conformance suite; the core does not change.
- The look is the host's theme: the same spec shows 4px buttons under Oxygen's
  Classic theme and 20px pills under Acrylic Orange (the console's theme).
