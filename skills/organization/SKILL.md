---
name: organization
description: The organization's settled decisions. Consult before asking the user any policy question, and before naming a provider or technology at design time.
metadata:
  aep:
    kind: org
    audience: [design]
---

Every section below is **settled** — this organization has already decided it.
Anything not below is open: interview for it normally.

- **In an interview** (start, amend): answer from the settled section and move
  on, recording it as a plain Product Decision in the PRD — no special tag. The
  user can override it in chat like any other decision, and the override wins.
- **At design time**: a settled section pins its provider or technology
  outright. A settled capability gets no candidates list.

## Authentication & identity

Every web app signs its users in via SSO through Thunder, the platform IDP.
Thunder is available as a dependency.

## Technology stack

- Web apps: TypeScript + React, single-page app.
- Services and APIs: Ballerina.

## UI design system

`astryx-design-system`

That is the name of a skill in this library, and it is the single authority for
this organization's web-app UI — components, layout, styling, theming, and the
verification step a web-app build owes it. **This section is a pointer, not a
specification:** what the design system requires is stated in that skill and is
never restated here, so the two can never disagree.

To adopt a different design system, change the name above and make sure a skill
by that name exists (see "Swapping the UI design system" in `skills/AGENTS.md`).
Those are the only edits — nothing else in the library names a design system.
Leave this section empty to run with no design system at all; web-app builds
then carry only the stack skills.
