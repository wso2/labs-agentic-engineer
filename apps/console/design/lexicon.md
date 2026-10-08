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
the user is. The panel always holds the project's main chat; the Issues Page
has its own agent, whose chat is a branch stacked on the main chat, and so does
each open issue, on its card.

| Say | Where | Means |
|---|---|---|
| **Work on Issues here · Start** | a row above the main chat's composer on the Issues Page, until its chat is started | Start opens the Issues chat as a sheet over the main chat, its composer focused |
| **↑ Main chat** | the strip across the top of the Issues sheet, with the main chat's last line after it | goes back to the main chat: the sheet folds down to a link; nothing is lost |
| **<Project> └ Issues** | the path under that strip (the project named as the chat's breadcrumb names it) | the Issues chat is a branch of this project's main chat |
| **↳ Issues · its own chat · Open ↑** | the end of the main thread while the Issues chat is minimised | Open brings the sheet back up, with whatever was typed in it |
| **Threads** | the list button in the chat's header | the project's chats: **<Project> · main chat**, **Issues** once it holds something, and **Issues › #N** for each issue's chat that holds something (opened in this tab), each with its message count; Issues reads **open** while started, **summarised** once the main chat has its From Issues note; an issue's reads **open** while its sheet is up. Issues goes to the Issues page and opens its chat; Issues › #N goes to the issue's card and brings its chat up; the main chat brings it to the front |
| **Talking about the project's issues** | the composer's line in the Issues chat | the chat there is about the project's issues, not the whole product |
| **Tell me what's broken or what you need, and I'll draft an issue.** | an empty Issues chat | invites a report; the agent drafts, and files once the user confirms |
| **Describe what's broken, or what you need…** | the Issues chat composer's placeholder (the main chat's stays "Tell the agent what to change…") | the box takes a report, not an edit instruction |
| **Create Issue** | a button in the Issues Page's header | starts an issue in the Issues chat: opens the chat over the main one, puts `/issue ` in its composer and focuses it; nothing is sent until you send |
| **This belongs in Issues. I'll open it and draft the issue, in its own chat on top of this one.** | the main chat, when the agent hands a request on to Issues (the turn ends there) | the request is a problem report or an issue to file; the Issues chat drafts it, not this one |
| **I'll pass on: “…”** | under that announcement, before New Issue or Stay here is chosen | the request as it will go to Issues, as plain text, its first 200 characters; it goes as an `/issue` report, so the Issues agent drafts it and asks before filing |
| **New Issue** | a button under that announcement | opens the Issues page with its chat over the main one, and sends the request to it as `/issue <request>` (into its composer instead while a turn runs there) |
| **Stay here** | a button under that announcement | keeps the conversation where it is; nothing is sent to Issues |
| **Continued in Issues · Open** | the announcement once New Issue was chosen, or once the request is a message on the Issues thread | the request went on to Issues; Open goes to the Issues page and opens its chat |
| **Stayed here instead of opening Issues** | the announcement once Stay here was chosen | the user kept the request in this chat |
| **From Issues · N messages · …** | a note in the main chat, after the user leaves the Issues page having talked in its chat (minimising the Issues chat, or opening an issue's card, is not leaving) | sums up the visit: the number of messages said there since arriving (a later visit adds to it, replacing the note while it is the chat's last line) and the agent's last line, on one line of at most 140 characters; nothing is posted for a visit that said nothing |
| **Reopen** | a button under that note | goes back to the Issues page and opens its chat |
| **<Project> └ Issues › #N** | the path under the strip of an issue's sheet, on the issue's card | the issue's own chat, a branch of this project's main chat; it is up on arrival at an open issue's card |
| **↳ Issues › #N · its own chat · Open ↑** | the end of the main thread while an issue's chat is minimised on its card | Open brings the issue's sheet back up |
| **Talking about issue #N** | the composer's line in an issue's chat | the chat there is about that one issue, with its own agent |
| **Ask me about this issue, or tell me what to do with it.** | an empty issue chat | invites a question, or a change: a comment, an edit, closing it, handing it to the coding agent; each is asked about first |
| **Ask about this issue, or what to do with it…** | an issue chat composer's placeholder | the box takes a question or a change to the issue |
| **Post this comment?** · **Post it** / **Apply this edit?** · **Apply it** / **Close this issue?** · **Close it** / **Reopen this issue?** · **Reopen it** / **Hand this to the coding agent?** · **Hand it over** — **Not now** | the issue agent's confirmation, on the Questions card; the go-ahead option's description is the exact change (the comment, "Title: …" / "Body: …", the reason, "Component: …") | nothing changes on the issue until that option is chosen; Not now changes nothing |
| **Continue on #N · Open** | the Issues chat, under the reply that filed issue #N | the issue now has its own chat; Open goes to the issue's card with that chat up |
| **This issue is closed.** | an Issue card, for a closed issue | a closed issue has no chat of its own; closing an issue removes its chat |
| **Issue #N was closed; its chat was removed.** | a line in the main chat, when the issue whose card is open turns out closed (its agent closed it, or it was closed on GitHub) | the issue's sheet went away with it; the line is the console's, never sent to the agent |
| **This issue is closed. Its chat was removed.** | an issue chat or its Questions card, when the server refuses its thread as closed | as above, where the chat itself was asked for |

## Issue card

What an Issue card says about handing its issue to the coding agent.

| Say | Where | Means |
|---|---|---|
| **Hand to the coding agent** | a button on an open issue's card, until the coding agent has taken it on (a closed issue has none) | opens the picker below; nothing is handed over yet |
| **Which component is it about?** | the picker's question, over the design's components (one is chosen already when the design has only one) | the coding agent works in the component the person picks |
| **Hand it over** · **Cancel** | the picker's buttons | Hand it over hands the issue to the coding agent with that component; Cancel puts the picker away |
| **Handed to the coding agent.** | the card, once the platform has the issue | the coding agent's log takes over the card |
| **Deploy a version first: the coding agent works in a deployed version's milestone.** | the picker, when the project has no deployed version (the platform's words, shown as they are, as is any other refusal) | there is nothing to hand it to yet |
| **Couldn't hand it to the coding agent. Try again.** | the picker, for any other failure | nothing was handed over |
| **The design has no components yet, so there is nothing to hand it to.** / **The design's components could not be read.** | the picker, in place of the components | nothing can be picked |

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
