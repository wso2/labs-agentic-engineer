---
name: cell-design
description: Use when generating a design OR when ANY change alters the architecture — a component added, removed, or renamed; an edge or dependency changed; exposure changed; an external/SaaS dependency added or dropped. specs/design/design.cell moves FIRST, before design.md and the component design.json files. Covers the grammar, the AEP boundary semantics (where each dependency goes), and the write protocol that drives the live architecture diagram.
metadata:
  aep:
    kind: platform
    audience: [design]
---


# Cell design (design.cell)

`specs/design/design.cell` is the PRIMARY design source: a small project-level
text file describing the architecture as a **cell**. The console renders
it live as you write it, so it is the FIRST design artifact you emit — and the
platform derives from it: when the cell is saved, a design.json skeleton is
scaffolded for every deployable component that has none. The component ids
here MUST match the `components/<name>/design.json` names.

The cell carries structure only. Which PRD stories a component serves is
recorded in that component's `design.json` `stories` list during enrichment —
the build gate checks that every PRD story is claimed by some component, so
claim each story where it is actually served, never in the diagram.

## Cell-based architecture in AEP

The **cell is the project boundary**, drawn as an octagon. Your project's own
components live *inside* it. Anything the project talks to that lives *outside*
the project is drawn on one of the four boundaries, and which boundary encodes
what kind of thing it is:

- **north** = public / internet gateway. A component exposed to the internet
  (`design.json` `exposure: "internet"`) is reached through north:
  `north -> <component>`.
- **west** = org / intranet gateway. A component exposed only inside the org
  (`exposure: "intranet"`) is reached through west: `west -> <component>`.
- **east** = org / platform dependencies OUTSIDE the project — shared platform
  services and other projects' services. Thunder auth (a `thunder-app`
  platform-resource) and any `org-service` dependency go on **east**.
- **south** = third-party / external dependencies — SaaS and other systems
  outside the platform (`external`-kind dependencies: payment, email, …).

**Placement, keyed by the component's `design.json` dependency `kind`:**

| What it is | design.json signal | Where it goes |
|---|---|---|
| Your own component (service, web-app, worker) | a `components/<name>/` folder | INSIDE the cell — `component …` |
| Project-scoped resource (db, cache, object store) | `platform-resource` (postgres-cnpg/redis/…) | INSIDE the cell — `component <id> … database` |
| Auth (Thunder) | `platform-resource` `thunder-app` | **east** — `east <id> as "Thunder Auth" identity-server` |
| Another project's / org service | `org-service` | **east** |
| Third-party SaaS / external system | `external` | **south** |
| Exposed to the internet | `exposure: "internet"` | **north** exposure edge `north -> comp` |
| Exposed to the org only | `exposure: "intranet"` | **west** exposure edge `west -> comp` |

Rule of thumb: **if a dependency lives inside the project, it's a `component`
inside the cell; if it lives outside, it leaves the cell through a boundary**
(north/west for who calls in, east/south for what the project calls out to).

## Grammar

Line-based: one statement per line, whitespace trimmed, blank lines ignored.
Whole-line comments only — a line whose first non-space char is `#` or `//`.
Wrap multi-word values in double quotes (`"Ceramics API"`). Reserved ids you
cannot use for a component/external: `title version component as north east
south west`.

**Title** — `title <text>` (rest of line).

**Component** (inside the cell) — `component <id> [as <label>] [type]`
- The optional `type` is the LAST bare token. **There is no `:` before it.**
- Without `as`, everything after the id is the type. With `as`, one trailing
  token is the label; if there are two or more, the LAST is the type and the
  rest is the label.
- `component ceramics-api` → id only.
- `component ceramics-api service` → id + type `service`.
- `component ceramics-api as "Ceramics API" service` → id, label, type.

The cell details the WHOLE architecture — every component the PRD's stories
call for, each one fully designed.

**External** (on a boundary) — `<direction> <id> [as <label>] [type]` where
direction is `north|east|south|west`. Same label/type rule as component. The
line must NOT contain `->`.
- `east user-auth as "Thunder Auth" identity-server`
- `south payment-provider as "Payment Provider" service`

**Edges** — `A -> B [: label]`. Everything after the first `:` is the label.
The kind is inferred from the tokens:

| Form | Meaning |
|---|---|
| `webapp -> api` | internal: component → component |
| `north -> api` | exposure: internet gateway exposes `api` |
| `west -> api` | exposure: intranet gateway exposes `api` |
| `north client -> api` | inbound: external `client` on north → `api` |
| `api -> east user-auth` | outbound: `api` → external `user-auth` on east |

**Ergonomic style (preferred):** declare externals first with their boundary,
then write plain `a -> b` arrows — the compiler reclassifies an edge that
touches a declared external into the right inbound/outbound form automatically.
So after `east user-auth …` and `south payment-provider …`, you just write
`ceramics-api -> user-auth` and `ceramics-api -> payment-provider`.

**Boundary validation (hard errors):** north/west are inbound only, east/south
are outbound only. `api -> north x` and `east x -> api` are rejected.

**One cell is the norm.** Because the cell = the project, a plain list of
components + externals + edges (no wrapper) is a single implicit cell — that is
what you almost always write. Multi-cell blocks (`cell <id> { … }` with
cross-cell edges) are only for modelling several projects together; skip them
for a single project.

## Common mistakes (do NOT do these)

- ❌ `resource user-auth "…"` / `external payment "…"` — **there is no
  `resource` or `external` keyword.** Externals are a `<direction> <id>` line.
- ❌ `component api "Ceramics API"` — a bare quoted string after the id is read
  as the *type*, not a label. Use `as`: `component api as "Ceramics API"
  service`.
- ❌ `component api as "Ceramics API" : service` — no colon before the type.
- ❌ Putting Thunder/payment/email *inside* the cell as components. Auth and
  org services go **east**; third-party SaaS goes **south**.

## Worked example

```
title Handmade Ceramics Online Store

component ceramics-webapp as "Ceramics Storefront" web-application
component ceramics-api as "Ceramics API" service

east user-auth as "Thunder Auth" identity-server
south payment-provider as "Payment Provider" service
south email-provider as "Email Provider" service

north -> ceramics-webapp

ceramics-webapp -> ceramics-api
ceramics-webapp -> user-auth
ceramics-api -> user-auth
ceramics-api -> payment-provider
ceramics-api -> email-provider
```

## Write it in one addFile — the platform streams it

Write the whole `specs/design/design.cell` in a **single `addFile`**. The
platform streams the file into the live architecture diagram line by line as you
write it, so the diagram already grows one node at a time — you do NOT need
incremental `editFile`s, and you should not use them here. Emit the file in a
readable order (title, then components, then boundary externals, then edges) so
the diagram builds up sensibly as it streams.

## Changing an existing architecture

A change is ARCHITECTURAL when it adds, removes, or renames a component,
changes who calls whom (an edge), changes a component's exposure, or adds or
drops an org/external/SaaS dependency. For such a change design.cell moves
FIRST and the rest of the design follows it:

1. Update design.cell with targeted `editFile` edits — add or remove just the
   affected lines (a component/external declaration, an edge). Each applied
   edit lands in the live diagram IN PLACE, so the user sees exactly the
   requested change without the diagram tearing down and rebuilding. Only
   when the change replaces MOST of the diagram (a restructure) re-emit it
   instead: `removeFile`, then ONE `addFile` with the complete new diagram
   (the fresh add re-streams it line by line).
2. Update `specs/design/design.md` (Components, Interactions) to match.
3. Re-emit every affected `components/<name>/design.json` — same component
   ids, and every design.cell edge touching a component appears in that
   component's `dependencies` (and vice versa).

Do not narrate this in chat — just make the writes. A change that touches no
component, edge, exposure, or external dependency (copy edits, capability
wording, data-model tweaks) does NOT touch design.cell.
