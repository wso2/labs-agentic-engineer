---
name: modernize-extract
description: Use in a LEGACY APPLICATION REPOSITORY, outside AEP, to extract a requirements bundle (PRD plus domain model, business rules, integrations) for import into a new AEP project. Never applies to a turn running inside an AEP project.
disable-model-invocation: true
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Modernize extract

Extract a **requirements-only** bundle from a legacy application so it can be
imported into a new AEP project. The skill runs on the developer's machine,
inside the legacy repo — never on the platform.

## Hard stop — refuse AEP projects

If `specs/requirements/` or `specs/design/` exists in the working tree, this is
an AEP project. **Stop immediately.** Tell the user to run `/start` (or
`/amend`) inside AEP instead. Do not write anything.

## Prohibitions (refusals, not preferences)

- No `design.cell`, no component or service list, no directory tree of the
  legacy app.
- No framework or language names in `prd.md` (product altitude only — see
  `prd-contract`).
- No source files copied into the bundle. Verbatim fenced blocks are allowed
  **only** for lookup tables, rate tables, and formulas that cannot be
  losslessly prosified, and only inside `business-rules.md`, labelled as
  reference values — not design constraints.
- Do not invent architecture. The bundle is requirements; `/design` in AEP
  owns structure.

## Bundle contract

Write flat markdown under `.aep/requirements/`. Load
`references/bundle-contract.md` for the exact file set, section shapes, and
size budget. Flat matters: nested paths are invisible to AEP's requirements
save gate.

## Phases

### 1. Survey (silent)

Read-only walk of the legacy tree. Capture facts — do not ask yet:

- Entry points (CLIs, HTTP servers, schedulers, message consumers)
- Routes / handlers / screens and what each does for whom
- Persisted models, migrations, schemas
- Auth wiring (who signs in, roles, IdP if any)
- Outbound calls (HTTP, queues, email, payment, …) by capability
- Config knobs and feature flags that change product behaviour
- Obvious dead or unused paths

Prefer deterministic tools (`find`, `rg`, language-aware search). Do not
rewrite or "clean up" the legacy tree.

### 2. Run it

Best effort: bring the app up (`docker compose up`, `make run`, the package
manager start script, whatever the README claims) and exercise the main
flows you found. Note what actually works versus what is documented but
broken. Secrets and seed data stay on this machine — that is why extraction
is external.

If the app cannot start, record why in Open Questions and continue from
code + the interview.

### 3. Grill

Interview the person who knows the product. Use the grilling mechanics
(`ask_question` / `ask_questions` — or the client's equivalent structured
question tool). **One form per topic**, not a single mega-form:

1. What the product is *for* (problem + solution in stakeholder language)
2. Actors (who uses it, product altitude)
3. Load-bearing vs accidental behaviour (what must ship vs what is dead /
   accidental)
4. Journeys that become numbered user stories
5. Product decisions (sign-in approach, notifications, integrations by
   capability)
6. Out of scope for the AEP rebuild
7. Open questions nobody can answer yet

Ask only what changes the bundle. Skip valve: if the user says "just
generate" / "skip", stop asking, fill remaining decisions with your
recommended answer, and tag each `*assumed*` where it lands.

### 4. Write

Write `.aep/requirements/` exactly as `references/bundle-contract.md`
specifies:

- `prd.md` — required; `prd-contract` section order and story numbering
- `domain-model.md` — entities, fields, relations, invariants
- `business-rules.md` — load-bearing rules with `path:line` provenance
- `integrations.md` — external systems by capability

Target ~8 KB per document. Prefer cutting depth over padding.

### 5. Pack

Print the exact commands and the console path:

```bash
cd .aep && tar czf requirements-bundle.tar.gz -C requirements .
# Upload requirements-bundle.tar.gz in the AEP console:
# Project → Spec → Requirements → Import requirements
```

Close with a one-paragraph summary of decisions (calling out every
`*assumed*`) and remind the user: after import, run **Generate design** in
AEP — do not try to design from this skill.
