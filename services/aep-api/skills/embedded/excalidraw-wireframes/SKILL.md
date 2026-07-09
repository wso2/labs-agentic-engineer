---
name: excalidraw-wireframes
description: Use when creating or updating UI wireframes for a webapp component — sketching screens, forms, tables, or navigation flow.
metadata:
  aep:
    kind: platform
---

# Wireframes (Excalidraw DSL)

Every `web-application` component gets its wireframes as ONE DSL source file at
`specs/design/components/<name>/wireframes.dsl` — all screens in one file.
Write ONLY the `.dsl`; the platform compiles it to the `.excalidraw`
deterministically. NEVER write a `.excalidraw` file by hand.

Wireframes are **gray and structural**: they validate layout, hierarchy, and
flow — never colors, branding, or visual polish. The compiler enforces the
style; you only decide structure.

## Derive the screens from the requirements

One screen per distinct user task, per role (for an expense app: employee
claim list, claim form, manager review queue). Don't invent screens the
requirements don't imply.

## The DSL

```
screen <Name> [WxH]                  // desktop 1280x800 by default; size only for modals/exceptions
  navbar "App | Nav1 | Nav2"         // full-width top bar; pipe-separated items; no coordinates
  sidebar "Item1 | Item2 | Item3"    // left rail below the navbar; no coordinates
  <kind> "<label>" <x>,<y> [WxH]     // everything else: screen-local coords from the top-left
flow
  <ScreenA> -> <ScreenB>             // navigation edges between screen names
```

Element kinds and when to use them:

| Kind | Use for | Default size |
|---|---|---|
| `navbar` | the app's top bar (first line of every screen) | full width × 56 |
| `sidebar` | section navigation (when the app has sections) | 240 × full height |
| `heading` | page/section titles | auto |
| `text` | captions, values, helper copy | auto |
| `input` | text fields, selects, search boxes | 320×36 |
| `button` | actions; the bottom-most button gets the flow marker | 140×40 |
| `table` | data lists — label is pipe-separated column headers, e.g. `table "Risk \| Owner \| Status" 280,140 940x280` | 640×240 |
| `card` | dashboard stat tiles, summary panels | 300×160 |
| `image` | logos, charts, previews (renders a crossed box) | 240×140 |
| `rect` / `ellipse` | anything else (generic container / avatar, status dot) | 160×32 |

Layout rules:

- With a sidebar, content starts at `x ≥ 264`; below a navbar, at `y ≥ 72`.
- Full-content-width elements (tables) are ~940 wide at `x` 280.
- Stack vertically with 16–24px gaps; keep everything inside the screen size
  and never overlapping.
- Comments start with `//`. Screen names in `flow` must match `screen` names
  exactly; every screen should be reachable.

Read `references/wireframes-dsl-example.md` for a complete worked example
before writing your first wireframe.

## Updating existing wireframes

The DSL is line-oriented, so `editFile` works naturally: anchor on the
`screen <Name>` line plus the element line you're changing. Add a screen by
appending a new `screen` block and its `flow` edges.
