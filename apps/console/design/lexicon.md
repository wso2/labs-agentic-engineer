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

## The agent's questions

| Say | Means | Not |
|---|---|---|
| **Questions** | the card where every question the agent is waiting on is answered, in one list | form, quiz, Review (a prototype term) |
| **Questions for you** | the card's heading over that list | Quick questions |
| **The agent has N questions · Answer them →** / **The agent has a question · Answer it →** | the chat's pointer to the Questions card while questions are open | Open questions, Respond |
| **The agent is asking questions…** / **Still asking…** | the batch is still arriving; what has arrived can already be answered | loading |
| **Send answers** / **Send answer** | send every answer, or the one, to the agent as the next message | submit, Continue |
| **N of M answered** | how far the user is through the list | progress, completed |
| **Not answered** | a question still owed an answer, flagged when Send was pressed with gaps | skipped, missing |
| **No questions waiting** | the Questions card opened by hand when the agent is not waiting on anyone | empty |
