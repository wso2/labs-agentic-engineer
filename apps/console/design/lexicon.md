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
| **Describe what's broken, or what you need…** | the composer's placeholder on the Issues Page (elsewhere it stays "Tell the agent what to change…") | the box takes a report, not an edit instruction |
| **Create Issue** | a button in the Issues Page's header | starts an issue in the Issues chat: puts `/issue ` in the composer and focuses it; nothing is sent until you send |
| **This belongs in Issues. I'll open it and draft the issue, in its own chat on top of this one.** | the main chat, when the agent hands a request on to Issues (the turn ends there) | the request is a problem report or an issue to file; the Issues chat drafts it, not this one |
| **New Issue** | a button under that announcement | opens the Issues page and sends the user's request to its chat (into its composer instead while a turn runs there) |
| **Stay here** | a button under that announcement | keeps the conversation where it is; nothing is sent to Issues |
| **Continued in Issues · Open** | the announcement once New Issue was chosen, or once the request is a message on the Issues thread | the request went on to Issues; Open goes to the Issues page |
| **Stayed here instead of opening Issues** | the announcement once Stay here was chosen | the user kept the request in this chat |

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
