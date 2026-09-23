# ADR-0001: The generative-UI renderer sits behind an adapter

## Context

Generative-UI libraries are young and still changing: json-render (Vercel
Labs), OpenUI (Thesys), and A2UI (Google, heading for v1.0). They share one
idea, a catalog of allowed components the model builds from, but differ in
wire format and scope. AEP needs the capability now and should not be locked
into whichever library it picks first.

## Decision

- **The catalog is AEP's own**, as Zod schemas plus descriptions
  (`src/catalog/`), with no renderer types. Zod is already how AEP shares
  contracts (see `@aep/agent-stream`).
- **The components are AEP's own**, written against `GenUiRenderProps` rather
  than a library's render props. They live in one package per design system
  (ADR-0002).
- **json-render is the first adapter** (`src/adapter/json-render/`). It fits
  the stack (React 19, Zod 4, JSON validation the Go BFF can share) and has the
  broadest renderer support. It is pinned to an exact version because it is a
  Labs project.
- **Action safety lives outside the adapter** (`dispatchGenUiAction`), so it
  does not depend on which library is underneath.
- The boundary is a test, not a convention: `src/boundary.test.ts`.

## Consequences

- Swapping libraries means writing a new adapter directory, not touching the
  catalog or the components.
- The spec wire format belongs to the adapter. Specs persisted in one format
  need converting if the adapter changes.
- json-render's own catalog check does not hold props to the component
  schemas, so the adapter adds a per-element props check to
  `validateGenUiSpec`.
