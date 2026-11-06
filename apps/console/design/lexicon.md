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
| **Prototype** | the clickable mock of one web application, made by Design | wireframe, mockup, demo |
| **Review** | open a prototype full screen | open, view |
| **Preview** | the review mode where the prototype acts | run, play |
| **Comment** (mode) | the review mode where a click starts a comment instead of acting. `annotate` is only the code's and the protocol's name for it, never said to the user | Annotate, mark up |
| **Comment** | one change asked of the prototype: the selected elements (or the whole screen) and the text. "Request" is only the contract's and the code's name for it, never said to the user | request, note, ticket |
| **draft** (comment) | a comment started and closed without Add: kept where it was written, shown as a hollow pin, reopened with its text; never counted or sent | unsaved comment |
| **Whole screen** | what a comment on no element is on, in its bubble and the list | page, full page |
| **Comment on this screen** | the comment list's action that starts a comment on the whole screen, not an element (the keyboard's way; the pointer's is a click on empty space, which puts the comment at that spot) | Comment on screen, page comment |
| **N pending · sent with Address comments on the Overview** | where the queued comments go, in the dock | Send to agent, submit |
| **The agent is revising the prototype…** | the dock's status while a design run may rewrite it | Revising…, processing |
| **Updated** | the toast once a changed revision lands in the open review | Done, Reloaded |
| **What changed** | the toast's action: the chat, at the turn that revised the prototype | Details, View diff |
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
| **This belongs in <Area>.** (today **Issues**) | the main chat, when the agent hands a request on to an area (the turn ends there) | the request is that area's work (for Issues: a problem report or an issue to file); the area's chat does it, not this one |
| **I'll pass on: “…”** | under that announcement, before Open <Area> or Stay here is chosen | the request as it will go to the area, as plain text, its first 200 characters; it goes as the area's command (`/issue` for Issues), so the Issues agent drafts it and asks before filing |
| **Open <Area>** (today **Open Issues**) | a button under that announcement | opens the area's page, whose chat takes the panel, and sends the request to it as its command, `/issue <request>` for Issues (into its composer instead while a turn runs there) |
| **Stay here** | a button under that announcement | keeps the conversation where it is; nothing is sent to the area |
| **Continued in <Area> · Open** | the announcement once Open <Area> was chosen, or once the request is a message on the area's thread | the request went on to the area; Open goes to its page and its chat |
| **Stayed here instead of opening <Area>** | the announcement once Stay here was chosen | the user kept the request in this chat |
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

## The design review

| Say | Means | Not |
|---|---|---|
| **Overview** | the Design card's first item: what to review, for whom, and the one step forward | Summary, Dashboard |
| **For business review** / **For technical review** | the Overview's two lists, by who can judge each file | Product, Technical (as a filter) |
| **Checked** | the reviewer has gone through the file (GitHub's Viewed); not "it's perfect" | Approved, Reviewed, Done |
| **Changed since you checked it** | the file changed after it was ticked, so it shows unticked | stale, outdated |
| **Pending** | a comment added and not yet sent; the Comments item counts them as "N pending" | draft (that is a started, unadded comment in the prototype review), unsent |
| **Address comments (N)** | send every pending (and reopened) comment to the agent in one turn | Send, Submit review |
| **✓ Addressed in revision N** | the agent's one-line answer on a comment it acted on | Resolved, Done |
| **Reopen** | put an addressed comment back in the next batch | Reply, Unresolve |
| **N assumed** | the Technical decisions row's count of decisions the agent made for the user that nobody has kept or changed yet | unconfirmed, open |
| **Keep** | confirm an assumed decision as written: its "assumed" comes off, its words stay | Accept, Approve, Confirm |
| **Change…** | ask the agent for a different decision: a comment on the decision, with the decision quoted | Edit, Remove, Reject |
| **by you** | the brief mark on a line the user just kept | Saved, Done |
| **Write what you want instead under the quote.** | the hint under a Change… comment's box; the quote above it can't be edited | — |
| **v2 is building — open it on Builds** | the Overview's note while a build runs and Build is held; the link opens that build | Build disabled, Busy |
| **Wait for the agent to finish** | the note while a turn runs in the project's chat and Build is held, so a build does not freeze a half-written design; the picker's Start build says the same | Agent busy, Build locked |
| **Not ready to build yet — Payroll export: waiting on Xero** | the note while every designed feature is held: each one with the build picker's reasons | Build refused, Blocked |
| **Nothing new to build: what is designed is built and unchanged** | the note when every designed feature is built and has not changed since | Up to date, Nothing to do |

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

## AE Studio

The organization's design workspace (the design agent, the Room). Named to
the user only when it is upgrading, starting, restarting or unavailable; never as a pod,
Resource or container.

| Situation | Says |
|---|---|
| First visit while it upgrades | the whole console waits: **Upgrading AE Studio** · *The console opens as soon as it is ready.* (no duration: a first install can take a quarter of an hour) |
| Starting behind the console, never ready this session (hold capped, after Try again, after a failed first read) | a banner above the page: **AE Studio is starting…**; the console stays usable |
| Restarting after a settings change (ready seen this session) | a banner above the page: **AE Studio is restarting…**; the console stays usable |
| Not ready within the bound (`failed`, reason `timeout`) | full page: **Starting AE Studio is taking longer than usual.** · *This often means the cluster is short on room. It keeps trying by itself; if this lasts, contact your platform administrator.* · **Open Settings**; re-read every 30 s, so it turns ready by itself. "Short on room" is said only here: the platform cannot see the cause |
| Failed to start (`failed`, reason `error` or none) | full page: **AE Studio couldn't start** · **Try again** · **Open Settings**; Settings stays reachable |
| Onboarding's skills step | waits inline: **Getting AE Studio ready…**; on a `timeout`, the same taking-longer copy as a warning, with **Retry** and **Continue anyway** |
| The spec or the builds, while AE Studio restarts (`ae_studio_unavailable`) | in place of the content, never a hold: **AE Studio is restarting — retrying…** |
| The spec or the builds, GitHub not connected (`github_not_connected`) | **Connect GitHub to continue**, with **Connect GitHub** → Settings, GitHub |
| The spec or the builds, AE Studio misconfigured (`ae_studio_misconfigured`) | **AE Studio is misconfigured — contact your administrator**, no retry |
| A chat message while AE Studio restarts | *AE Studio is restarting. Try again in a moment.* |

## Starting a project, refused

| The platform says | The create form says |
|---|---|
| GitHub not connected (`github_not_connected`) | a warning: **Connect GitHub to continue**, with **Connect GitHub** → Settings, GitHub |
| AE Studio restarting (`ae_studio_unavailable`) | **AE Studio is restarting — try again**, with **Try again** |
| AE Studio misconfigured (`ae_studio_misconfigured`) | an error: **AE Studio is misconfigured — contact your administrator**, no retry |
| An earlier delete of the project still finishing (`project_delete_pending`) | **An earlier delete of this project is still finishing. Try again in a minute.**, no button |

## The spec's saves

| Situation | Says |
|---|---|
| The Room's last save landed with warnings | **Saved with warnings**, dismissible, listing each file and the platform's message for it, as given |

The title says the save happened, so a warning is not read as lost work. The
next save's warnings replace these, and a save without any clears them.

## A spec file opened read-only

| Situation | Says |
|---|---|
| The file holds content the editor would lose if it bound to it (a mark the schema does not allow, a node it does not know) | above the file, a warning: **Part of this file can't be edited here safely, so it is read-only for now. Ask the agent to rewrite it to edit it again.** |

It names what the user can do, not the schema. The file is shown as far as it
can be read, and opens for editing by itself once the content is whole again.

## A build session's log

A session's log is kept for a few days, not forever. When it is not on screen
the session says why, in place of its lines; the run's outcome and its
explanation are unaffected.

| The log is | Says | Offers |
|---|---|---|
| Being written, or kept | nothing: the lines are the content | |
| No longer kept | **This run's log is no longer kept (logs are kept for a few days)** | nothing: it is not coming back |
| Not loadable right now | **Couldn't load this run's log right now** | **Try again** |

No retention number and no expiry date in copy: "a few days" is the promise,
and the exact window is the platform's to change.

## A component build's log

Under a version's Build logs, each component build the version's merge started,
and, opened, its log. A build is kept only for a while after it finishes, then
it is gone with its log; the version's own outcome is unaffected.

| The build or its log is | Says | Offers |
|---|---|---|
| Being written | its lines, then **…tailing** | |
| Finished, nothing written | **This build wrote nothing to its log** | |
| No longer kept (one build) | **This build's log is no longer kept** | nothing: it is not coming back |
| No longer kept (the merge's builds) | **These builds are no longer kept** | nothing |
| Not loadable right now | **Couldn't load this build's log right now** | **Try again** |

No retention number in copy, for the same reason as a session's log.

## A build session's crew

Inside a session's log, the agents that worked it: who is doing what now
(**Crew**), and where the time went (**Timeline**, offered from the second
agent on). `@aep/progress-view` owns the state words and the runtime's own
sentences; the Build card's crew components own the rest.

| Where | Says |
|---|---|
| The session's line over the crew | **N agents · N running · last event 12s ago**, or **N agents · all settled** once nothing runs |
| The connection gone quiet (three missed ticks) | warning · *No word from the platform for 45s: the connection may have dropped.* |
| An agent's state | **working** · **waiting** · **stalled** · **completed** · **failed** · **cancelled** |
| A row's age, while it runs | **quiet 1m** |
| An agent the runtime detached | **background**, beside its name |
| The picked agent's own task list | **Plan · 2 of 5 done**, each entry with its status (**pending** · **in progress** · **completed**) |
| What the picked agent started | **Subagents · 4**, each with its state or, settled, its totals |
| Its detached commands still going | **Running in the background · 2**, each **running · 3m** |
| Its steps | **Steps**: oldest first, the newest at the bottom; **Show 340 earlier steps** above the newest 200 |
| Steps arrived while the reader read back | **Latest · 3 new** (just **Latest** when none arrived), which returns to the newest |
| An agent with no steps | *No steps from this agent yet.* |
| The Timeline's key | **solid · working** · **faded · waiting on another agent** |
| A Timeline with nothing timed | *Nothing has been timed yet: the crew view has the live picture.* |

**Subagents**, not "dispatched" or "spawned": the reader's word for an agent
another agent started.

## A task's log

One task's own log, under its row on the Build card and on its Issue card.

| The log | Says |
|---|---|
| Opening | *Attaching to the task's log…* |
| Open, nothing yet | *Attached. Nothing logged for this task yet.* |
| Reopening | *Connection lost, reconnecting…* |
| Ended empty, coding work (`development`, `bug`, `conflict`) | *No log of its own: the coding agent's log has this task's work.* |
| Ended empty, validation | *No log of its own: the version's validation has this task's work.* |
| Ended empty, a provision gate | *No log: the platform resolves this gate itself.* |
| Ended empty, any other kind | *Nothing logged for this task.* |
| A run of it that wrote no lines | one line of its kind and status as the platform states them, with its reason: *provision completed: …* |

## An agent that has not started

A coding or validation agent can wait for the cluster before its first line:
no room (CPU, memory or a scheduling rule), an image that does not pull, a
secret not there yet, or, before any of that, the platform still applying its
Job. The platform gives it a startup grace from when the Job exists (a longer
cap while it does not), then closes the cycle and fails the run
`agent-start-failed`. The Build card and the
Validation card say both halves; `features/builds/model/agentStart.ts` owns
the words.

| Situation | Says |
|---|---|
| Waiting, the cluster has no room (`Unschedulable`) | warning · **Waiting for room in the cluster to start the agent** · *The cluster has no room for the agent right now (CPU, memory or a scheduling rule). If it has not started by 14:05, this run fails.* |
| Waiting, any other cause | warning · **Waiting to start the agent** · the cause · the same deadline sentence |
| Cause: the platform has not applied the agent's Job yet (`NotYetApplied`, never shown) | Waiting, as a neutral in-progress status with no warning and no deadline line (it is the normal path on Cloud): *Preparing the agent.* *The platform is still preparing the agent.* / after (`not_applied`): *The platform did not start it within 30 minutes.*, then *Retry starts a new attempt.* and no *The cluster reported* sentence: the cause is the platform's own. Same voice as AE Studio's *taking longer than usual* |
| Cause: image does not pull (`ImagePullBackOff`, `ErrImagePull`) | *The cluster cannot pull the agent's container image.* / after: *The cluster could not pull its container image.* |
| Cause: secret or setting missing (`CreateContainerConfigError`) | *A secret or setting the agent needs is not ready yet.* / after: *A secret or setting it needed was not ready.* |
| Cause the console has no words for | *The cluster reports the agent as waiting: `<reason>`.* / after: *The cluster reported `<reason>`.* |
| It never started, coding | **The coding agent could not start** · the cause · *Nothing ran; no pull request was opened.* · *Retry once the cluster has room.* (or *once that is fixed*) · *The cluster reported: …* |
| It never started, coding, after an earlier session of the build opened a pull request | *Nothing ran this time, so no new pull request was opened; #7, opened earlier in this build, is unchanged.* |
| It never started, validation, on the Validation card | **The validation agent could not start** · *Nothing ran; the version was not validated.* · *Validate once the cluster has room.* before any attempt has a verdict, *Revalidate …* after one: the word on the card's button (`validateLabel`) |
| It never started, validation, on the Build card | the same, with *Validate it again from its Validation card once the cluster has room.* and **Go to Validation** |
| A validation attempt, waiting / never started | **Waiting to start** / **Agent could not start** |
| A build session the cluster holds | its header reads **waiting to start**, never **writing now** |
| The short label | **Agent could not start** |

The deadline is the platform's, never a guess, and the time gains its date
only when it falls on another day. "Could not start", never "stopped" or
"died": nothing ran, so there was no stop to describe. The cluster's own words
end with exactly one period.

## Secrets in Settings

| Situation | Says |
|---|---|
| A stored key (API key, subscription token) | exactly **Set ••••••••**; no key: **Not set**. Never a prefix, a last four or any other part of the key: the server returns none. **Replace** swaps in the key input |
| Key refused by the secret store (`secret_store_write_failed`) | the server's message on the key field |
| Saved, but a follow-up step failed (`agent_manager_not_updated`) | beside **Test connection**; the key is saved |
| A Claude subscription whose token was never recorded (`tokenMissing`) | shown set (**Set ••••••••**, **Replace**, **Remove**) with a warning: *The Claude subscription token was never saved, so coding uses the API key. Replace the token to bill your Claude plan, or remove the subscription.* Never hidden: the org chose its plan and is billed API credits until it acts |
| A Claude subscription that is not active (its `status`; the server never flags it `tokenMissing`) | shown set (**Set ••••••••**, **Replace**, **Remove**) with a warning in place of *Coding bills your Claude plan…*: the server's `validationError`, or *This subscription is `<status>`: replace its token or remove it.* when it has none. Dispatch refuses the row, so the card names neither the plan nor the API key |
