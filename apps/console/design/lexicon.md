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
| **Comment** (mode) | the review mode where a click starts a comment instead of acting. `annotate` is only the code's and the protocol's name for it, never said to the user | Annotate, mark up |
| **Comment** | one change asked of the prototype: the selected elements (or the whole screen) and the text. "Request" is only the contract's and the code's name for it, never said to the user | request, note, ticket |
| **draft** (comment) | a comment started and closed without Add: kept where it was written, shown as a hollow pin, reopened with its text; never counted or sent | unsaved comment |
| **Whole screen** | what a comment on no element is on, in its bubble and the list | page, full page |
| **Send to agent** | send the queued comments to the agent in one turn (the dock's last button) | submit, Send all |
| **Comment on this screen** | the comment list's action that starts a comment on the whole screen, not an element (the keyboard's way; the pointer's is a click on empty space, which puts the comment at that spot) | Comment on screen, page comment |
| **Revising…** | the Send to agent button while the agent works on the sent comments; read out to a screen reader as `Agent is revising… (N comments)` | a separate status label, a header chip, processing |
| **Updated · N addressed** | the toast once the revision lands in the open review: `Updated · N comments addressed`, N being the comments sent | Done, Reloaded |
| **What changed** | the toast's action: the chat, at the turn that revised the prototype | Details, View diff |
| **Retry** | send again the comments a revision that did not land gave back | Resend |
| **Written on an earlier version** | a queued comment written before the revision now showing landed | stale, outdated |
| **Reset data** | restore the prototype's mock data | clear |
| **Dock** | the review's one floating bar below the prototype, with every control | toolbar, send bar |

## Chat

What the chat panel says above the composer and in an empty thread, by where
the user is. The panel holds one thread, the page's (ADR-0003): the project's
main chat; on the Issues Page its own agent's chat, a branch of the main chat;
on an open issue's card, that issue's chat.

| Say | Where | Means |
|---|---|---|
| **<org> › <Project> › Issues › #N** | the chat's breadcrumb, on top of the panel | the thread the panel holds: **<org> › <Project>** the main chat, **… › Issues** the Issues chat, **… › Issues › #N** an issue's chat. Every crumb before the last goes there: the org to its Dashboard, the project to its overview and main chat, Issues to the Issues page and its chat |
| **Threads** | the list button in the chat's header | the project's chats: **<Project> · main chat**, **Issues** once it holds something, and **Issues › #N** for each issue's chat that holds something (opened in this tab), each with its message count; Issues reads **open** while the user is on the Issues page, **summarised** once the main chat has its From Issues note; an issue's reads **open** while it is in the panel. Each goes to its page, whose thread the panel then holds: the main chat to the overview, Issues to the Issues page, Issues › #N to the issue's card |
| **Talking about the project's issues** | the composer's line in the Issues chat | the chat there is about the project's issues, not the whole product |
| **Tell me what's broken or what you need, and I'll draft an issue.** | an empty Issues chat | invites a report; the agent drafts, and files once the user confirms |
| **Describe what's broken, or what you need…** | the Issues chat composer's placeholder (the main chat's stays "Tell the agent what to change…") | the box takes a report, not an edit instruction |
| **Create Issue** | a button in the Issues Page's header | starts an issue in the Issues chat: opens the chat panel, puts `/issue ` in its composer and focuses it; nothing is sent until you send |
| **This belongs in Issues. I'll open it and draft the issue, in its own chat.** | the main chat, when the agent hands a request on to Issues (the turn ends there) | the request is a problem report or an issue to file; the Issues chat drafts it, not this one |
| **I'll pass on: “…”** | under that announcement, before New Issue or Stay here is chosen | the request as it will go to Issues, as plain text, its first 200 characters; it goes as an `/issue` report, so the Issues agent drafts it and asks before filing |
| **New Issue** | a button under that announcement | opens the Issues page, whose chat takes the panel, and sends the request to it as `/issue <request>` (into its composer instead while a turn runs there) |
| **Stay here** | a button under that announcement | keeps the conversation where it is; nothing is sent to Issues |
| **Continued in Issues · Open** | the announcement once New Issue was chosen, or once the request is a message on the Issues thread | the request went on to Issues; Open goes to the Issues page and its chat |
| **Stayed here instead of opening Issues** | the announcement once Stay here was chosen | the user kept the request in this chat |
| **From Issues · N messages · …** | a note in the main chat, after the user leaves the Issues page having talked in its chat (opening an issue's card is not leaving) | sums up the visit: the number of messages said there since arriving (a later visit adds to it, replacing the note while it is the chat's last line) and the agent's last line, on one line of at most 140 characters; nothing is posted for a visit that said nothing |
| **Reopen** | a button under that note | goes back to the Issues page and its chat |
| **Talking about issue #N** | the composer's line in an issue's chat | the chat there is about that one issue, with its own agent |
| **Ask me about this issue, or tell me what to do with it.** | an empty issue chat | invites a question, or a change: a comment, an edit, closing it, handing it to the coding agent; each is asked about first |
| **Ask about this issue, or what to do with it…** | an issue chat composer's placeholder | the box takes a question or a change to the issue |
| **Post this comment?** · **Post it** / **Apply this edit?** · **Apply it** / **Close this issue?** · **Close it** / **Reopen this issue?** · **Reopen it** / **Hand this to the coding agent?** · **Hand it over** — **Not now** | the issue agent's confirmation, on the Questions card; the go-ahead option's description is the exact change (the comment, "Title: …" / "Body: …", the reason, "Component: …") | nothing changes on the issue until that option is chosen; Not now changes nothing |
| **Continue on #N · Open** | the Issues chat, under the reply that filed issue #N | the issue now has its own chat; Open goes to the issue's card, whose chat the panel then holds |
| **This issue is closed.** | an Issue card, for a closed issue | a closed issue has no chat of its own; closing an issue removes its chat |
| **Issue #N was closed; its chat was removed.** | a line in the main chat, when the issue whose card is open turns out closed (its agent closed it, or it was closed on GitHub) | the issue's chat went away with it, and the main chat took the panel; the line is the console's, never sent to the agent |
| **This issue is closed. Its chat was removed.** | an issue chat or its Questions card, when the server refuses its thread as closed | as above, where the chat itself was asked for |

## Issue card

What an Issue card says about handing its issue to the coding agent.

| Say | Where | Means |
|---|---|---|
| **Hand to the coding agent** | a button on an open issue's card, until the coding agent has taken it on (armed, in a version's milestone, not halted — a halted issue offers it again); a closed issue has none, nor has one the platform works another way (the version's validation task, a dispatch gate, planned work, a configuration-only incident) | opens the picker below; nothing is handed over yet |
| **Which component is it about?** | the picker's question, over the design's components (one is chosen already when the design has only one) | the coding agent works in the component the person picks |
| **Hand it over** · **Cancel** | the picker's buttons | Hand it over hands the issue to the coding agent with that component; Cancel puts the picker away |
| **Handed to the coding agent.** | the card, once the platform has the issue and until the issue list reads it taken on | the hand-off went through |
| **The coding agent works it in version** *v2***.** | the card, for an issue the coding agent has taken on; the version (the ledger's entry for the issue's milestone) links to its Build card, where the coding agent's log is | the build of that version is working it |
| **The coding agent is working on it.** | as above, when the ledger has no entry for the issue's milestone | the same, with no version to point to |
| **Deploy a version first: the coding agent works in a deployed version's milestone.** | the picker, when the project has no deployed version (the platform's words, shown as they are, as is any other refusal) | there is nothing to hand it to yet |
| **This issue is closed.** | the picker, when the issue was closed meanwhile (the platform's words) | nothing was handed over |
| **The coding agent doesn't take on this kind of issue.** | the picker, when the platform will not adopt the issue's kind (the platform's words; the card offers no button where it can tell) | nothing was handed over |
| **Couldn't hand it to the coding agent. Try again.** | the picker, for any other failure | nothing was handed over |
| **The design has no components yet, so there is nothing to hand it to.** / **The design's components could not be read.** | the picker, in place of the components (a project with no design yet has none) | nothing can be picked |

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
