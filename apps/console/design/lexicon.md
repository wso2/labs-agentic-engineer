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
| Cause: the platform has not applied the agent's Job yet (`NotYetApplied`, never shown) | *The platform is still preparing the agent.* / after (`not_applied`): *The platform took longer than usual to prepare it.*, then *Retry in a few minutes.* and no *The cluster reported* sentence: the cause is the platform's own. Same voice as AE Studio's *taking longer than usual* |
| Cause: image does not pull (`ImagePullBackOff`, `ErrImagePull`) | *The cluster cannot pull the agent's container image.* / after: *The cluster could not pull its container image.* |
| Cause: secret or setting missing (`CreateContainerConfigError`) | *A secret or setting the agent needs is not ready yet.* / after: *A secret or setting it needed was not ready.* |
| Cause the console has no words for | *The cluster reports the agent as waiting: `<reason>`.* / after: *The cluster reported `<reason>`.* |
| It never started, coding | **The coding agent could not start** · the cause · *Nothing ran; no pull request was opened.* · *Retry once the cluster has room.* (or *once that is fixed*) · *The cluster reported: …* |
| It never started, coding, after an earlier session of the build opened a pull request | *Nothing ran this time, so no new pull request was opened; #7, opened earlier in this build, is unchanged.* |
| It never started, validation, on the Validation card | **The validation agent could not start** · *Nothing ran; the version was not validated.* · *Validate once the cluster has room.* before any attempt has a verdict, *Revalidate …* after one: the word on the card's button (`validateLabel`) |
| It never started, validation, on the Build card | the same, with *Validate it again from its Validation card once the cluster has room.* and **Go to Validation** |
| A validation attempt, waiting / never started | **Waiting to start** / **Agent could not start** |
| A build session the cluster holds | its header reads **waiting to start**, never **writing now**; the Coding agent's log meta reads **newest first** |
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
