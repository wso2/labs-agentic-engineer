# Console lexicon

The words the console says to a user. A feature draws its words from here;
introducing a user-facing term means amending this file in the same PR. The
older, fuller lexicon is the classic console's (`apps/console/design/lexicon.md`
at the `classic-console` git tag); terms are carried over here as their
features are ported.

## Naming rules

1. **A section names the class; an artifact names the document.** An artifact
   label adds information, never repeats its header outright.
2. **Filenames are never labels.** The user reads a document tree, not a repo.
3. **Plural for things that accumulate over time, singular for the one a
   project has.** Builds, Deployments, Issues, Validations — Overview, Spec.
4. **No acronyms** the user has to expand.
5. **The product is "Agentic Engineer", never "AEP".**
6. **Don't name the system's behavior** — name the user's situation. "Build
   refused" is the system describing itself; "Not ready to build yet" describes
   them.

## Spec artifacts

The names the agent uses for what it touched; `skills/console` pins this table
for console turns, and a disagreement is settled here.

| Repo | Say |
|---|---|
| `specs/requirements/prd.md` | the **Product requirements** |
| `specs/design/design.cell` | the **Architecture** |
| `specs/design/domain-model.md` | the **Domain model** |
| `specs/design/flows/<slug>.md` | the flow, by its title |
| `specs/design/security.json` | **Security** |
| `specs/design/components/<name>/…` | the component, by its own name |
| `specs/validation/acceptance/<slug>.feature` | the **Acceptance criteria**, as one set |

## Prototype

| Say | Means | Not |
|---|---|---|
| **Prototype** | the clickable mock of one web application, made by the agent | wireframe (the sketch in Design), mockup, demo |
| **Make prototype** / **Update prototype** | the action; Update once one exists | generate, build |
| **Review** | open a prototype full screen | open, view |
| **Preview** | the review mode where the prototype acts | run, play |
| **Annotate** | the review mode where clicks select instead of act | comment, mark up |
| **Request** | one change asked of the prototype: the selected elements and the text | comment, note, ticket |
| **Send all (N)** | send the queued requests to the agent in one turn | submit |
| **Reset data** | restore the prototype's mock data | clear |

## Chat

What the chat panel says above the composer and in an empty thread, by where
the user is. The Issues Page has its own agent.

| Say | Where | Means |
|---|---|---|
| **Talking about the project's issues** | the composer's line on the Issues Page | the chat there is about the project's issues, not the whole product |
| **Tell me what's broken or what you need, and I'll draft an issue.** | an empty thread on the Issues Page | invites a report; the agent drafts, and files once the user confirms |
