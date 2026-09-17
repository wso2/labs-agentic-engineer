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
  outright. A settled capability gets no suggestions list — it is a given,
  not a choice left to the user.

## Authentication & identity

Every web app signs its users in via SSO through Thunder, the platform IDP.
Thunder is available as a dependency.

## Security & compliance

`security-design` owns how a permission catalog is derived and what
`specs/design/security.json` may say. **This section is a pointer, not a
specification:** it holds only the calls that skill deliberately leaves to the
organization, because they are about the org's own shared directory rather than
about one project. A filled line is settled and is never asked at design time;
an empty one leaves the call to `security-design`.

| The call | This organization's answer |
|---|---|
| May a design introduce a **new org group**? | Yes, but only where `list_groups` shows no existing group is already those people. |
| **Groups to prefer**, by name. | *(none — the directory is the list)* |
| **Groups a role may never be assigned to.** | `Administrators`. It administers the platform, not any generated app. |
| Is **self-service enrolment** permitted? | Only where the PRD describes people who sign themselves up. |

Changing how a design derives groups, roles and scopes starts here: this is the
org-editable surface, and `security-design` is platform-owned and read-only in
the console. A change it cannot express is a change to that skill.

## Technology stack

- Web apps: TypeScript + React, single-page app.
- Services and APIs: Ballerina.

## UI design system

`oxygen-ui-design-system`

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
