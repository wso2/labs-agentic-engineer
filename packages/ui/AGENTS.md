# AGENTS.md — packages/ui

Shared React components — **one package per component**, never a single bundle.
`packages/ui/<component>` → `@aep/ui-<component>`.

**Status:** nothing here yet. Components land here as `@aep/ui-*` packages:
explorer, md-editor, excalidraw-editor, cell-diagram-view, openapi-view,
project-status, excalidraw-dsl.

## Conventions

- One component (or one tightly-related set) per package, independently versioned.
- One public entry point: `src/index.ts` — plus a re-export subpath where a
  non-React consumer needs part of the package, since the index pulls in React.
- Build with the Oxygen UI design system; see the `oxygen-ui` skill.
