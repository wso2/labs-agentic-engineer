# Console lexicon — the words the product says to a user

The console's user-facing vocabulary. Decided in
[#523](https://github.com/wso2/labs-agentic-engineer/issues/523).

**This is not `CONTEXT.md`.** The repo keeps two vocabularies and they are allowed to differ:

| | audience | governs |
|---|---|---|
| `CONTEXT.md`, `docs/glossary.md` | engineers | what terms *mean* in the domain — *spec bundle*, *milestone run*, *stage aggregate*, *committed truth* |
| **this file** | the product's users | what the console *says* — the words a newcomer reads |

The domain model already concedes the split: `CONTEXT.md`'s **Milestone run** entry warns
*"build (the console's 'build' is the click that starts a run, not the run)"*. This file is
where that difference gets written down instead of rediscovered.

**The rule, same as `design-system.md`:** a feature draws its words from here. Introducing a
user-facing term means amending this file in the same PR. A term absent from this file is not
yet a product word.

## Naming rules

1. **A section names the class; an artifact names the document.** An artifact label adds
   information, never repeats its header outright — `VALIDATION › Validation` fails this;
   `REQUIREMENTS › Product requirements` and `VALIDATION › Validation criteria` do not.
2. **Filenames are never labels.** The user reads a document tree, not a repo.
3. **Plural for things that accumulate over time, singular for the one a project has.**
   Builds, Deployments, Issues, Validations — Overview, Spec.
4. **No acronyms** the user has to expand.
5. **The product is "Agentic Engineer", never "AEP".** The acronym is an internal
   convenience; the app brands itself Agentic Engineer in its header, page title and
   onboarding, so every user-facing string does too.
6. **Don't name the system's behavior** — name the user's situation. "Build refused" is the
   system describing itself; "Not ready to build yet" describes them.

## The spec workspace

`Spec` stays as the umbrella — the route, the nav item, the workspace title. It is the right
concept for *the agreed description of what we're building*.

| Section | Artifacts | Repo |
|---|---|---|
| `REQUIREMENTS` | **Product requirements** | `specs/requirements/prd.md` |
| `DESIGN` (not `DESIGNS` — one design, several files) | **Architecture** · **Domain model** · **Security** as rows, then the groups: **Flows**, then one per component | `specs/design/` |
| `VALIDATION` | **Validation criteria** | `specs/validation/validation-criteria.json` |

**Security** is one rail entry, one page:

| artifact | what it holds | repo path |
|---|---|---|
| **Security** | which Roles this project uses, what each may do within this project, its Test users, and the Thunder application client | `specs/design/security.json` |

**Flows** is one collapsible group — one row per key flow, named by its
slug — and each **component** is another, headed by the component's own name:

| artifact | what it holds | repo path |
|---|---|---|
| a flow row | one key flow: a PRD actor's journey across the architecture, as a sequence diagram | `specs/design/flows/<slug>.md` |
| **Design** | the component's authored design record — type, language, the stories it serves, dependencies, pinned skills | `specs/design/components/<name>/design.json` |
| **API** | the component's OpenAPI contract | `specs/design/components/<name>/openapi.yaml` |
| **Wireframe** | the component's screens | `specs/design/components/<name>/wireframes.dsl` |

A label under a header adds the artifact, never the subject the header already
names — *Design*, not *Design overview* (the retired root document's name).

Agents, the runner's validation cycle and aep-api consume those repo paths
internally. This table *is* the mapping — keep it, so nobody later "fixes" the
inconsistency in the wrong direction. It holds only while the user never sees a
path, which requires the agent to stop quoting them
([#530](https://github.com/wso2/labs-agentic-engineer/issues/530)).

Placeholder for an artifact class with nothing in it: **"Not created yet"** — shipped in
[#575](https://github.com/wso2/labs-agentic-engineer/issues/575). Active wording is
reserved for when an agent is genuinely working — the old *"Being derived…"* claimed work that
was not happening.

**"Genuinely working" is `spec.agent`, not "the spec has no version".** The old signal was the flat
`specStatus`, which the platform only ever sets to *draft* or *approved* — so every unversioned project
on screen was described as being worked on, and the one moment work really is in flight, the kickoff,
had no files at all and read as idle. It is now read from the turn record
([#562](https://github.com/wso2/labs-agentic-engineer/issues/562)).

A user who opens the spec **before the interview has asked anything** — reachable from the moment the
project exists, since the kickoff fires at creation — meets *"Agent is working on the requirements
document"*, not a file picker over an empty list. It names what is happening rather than asking the user
to choose from nothing.

## Starting a project

| | |
|---|---|
| Heading | **What do you want to build?** |
| Subtitle | *Describe it in your own words — rough is fine, or upload a product requirements document.* |
| Idea echoed on the name step | **Prompt:** *&lt;the idea&gt;* — **one line, ellipsized** |
| Repository field helper | *Agentic Engineer creates this repository in your organization. Your specs and source code live here, and it stays yours.* |
| Name already taken | *That repository name already exists in `<org>` — pick another.* |
| While creating | **Creating your project…** |

The subtitle answers the only question a blank box raises — *how much detail?* — and stops
there. Narrating the rest of the journey up front is overwhelming, and the journey explains
itself as it happens. The upload clause depends on document upload shipping; drop it until then.

**The echoed idea is labelled `Prompt:`, and kept to one line.** The label says what the platform
*does* with what the user wrote — it is the agent's brief, not a description filed against the
project — which a bare quoted sentence never conveyed. It is a transparency device, the same role
the cropped idea plays beside `/start`, not a display of the document: the textarea has no length
limit, so unclamped this is the only element on the page that can grow without bound and push
**Create project** off the fold. The full text stays on the `title` attribute, and **Back** returns
to the textarea holding it.

**"Creates this repository"** is deliberate on the field helper and deliberate *not* on the wait
label. Under the Repository name field the repo is the subject and the user needs to know one is
being made in their organization — the previous copy said it "holds" the specs, implying it
already existed. During the wait the user's object is the project, not its storage.

**One honest label, not a phase sequence.** `POST /projects` is a single call with no
phase signal in the contract, so changing text mid-wait would be fabricated progress. The
second phase has a better home: after [#522](https://github.com/wso2/labs-agentic-engineer/issues/522)
the user lands on the overview with the spec card reading *Writing requirements*, which is the
same information shown where it is actually true.

**Not disclosed:** the org's shared skills repo, provisioned on first project creation. It is
org-level, async and best-effort, and on a hosted deployment the platform team owns it — the
developer cannot act on it, so telling them is noise.

### Example prompts

Three, one click each. They are the fastest answer to "what does enough detail look like", so
they carry the persona: internal enterprise work, not consumer apps.

- **Expense approval** — *Employees submit expense claims, managers approve them, and finance exports approved claims to payroll.*
- **Employee onboarding** — *Track each new hire's onboarding tasks across IT, HR and facilities, with reminders for overdue items.*
- **Triage agent** — *A support triage agent that reads incoming tickets, classifies them by urgency, and drafts replies for a human to approve.*

The third is deliberate: Agentic Engineer builds agents too, and the examples are where that
gets advertised.

**Card titles are two short words**, so each fits one line in a third-width card. A wrapped title
starts its card's body text at a different height from its neighbours', and the row reads ragged.
The label may be shorter than the prompt it stands for — *Triage agent* fronts *a support triage
agent that reads incoming tickets…* — since the prompt carries the detail.

## Starting a build

| | |
|---|---|
| Title | **Build v1** |
| Body | *This freezes your requirements and design as v1 and hands them to the coding agents. You can keep editing afterwards — it won't change what's being built.* |
| Scope list | **What gets built:** … |
| Confirm | **Start build** |
| Header button | **Build** |
| Its tooltip | *Freeze this design as v1 and start building* |

**Version** stays — an ordinary word, and the platform genuinely versions the spec.

Deliberately absent: **cut** (release-engineering argot), **git tag** (storage mechanism;
raises "do I need to know git?" at the worst moment), **milestone** (platform bookkeeping that
changes nothing about this decision — it stays discoverable on the Builds page), **stories in
scope** ("in scope" is the jargon; the list is right).

## Builds

Decided in [#609](https://github.com/wso2/labs-agentic-engineer/issues/609)
(ADR-0021). The Builds page is a list now, so its status cells are read side by
side — which is exactly why each **names the situation, not the state machine**
(naming rule 6). The qualifier after the middot is the fact the bare state does
not carry: who is acting, why it failed.

### A version's status, on the Builds ledger

| Situation | Says |
|---|---|
| The coding agent is working it | **`Running · Coding agent`** |
| The run ended badly, and the platform said why | **`Failed · <reason>`** |
| The run ended badly and left no reason | **`Failed`** |
| Built, and it is the version running in development | **`Deployed to development`** |
| Built, and its rollout is under way | **`Deploying to development`** |
| Built, and its rollout failed | **`Deploy failed`** |
| Built, everything else | **`Built`** |

**`Built`, never *Completed*.** "Completed" describes the run; the row is about
the version.

**There is no queued status, and that is a gap rather than a choice.** The
design drew `Queued · next` for a version waiting its turn, and the platform
genuinely does run one build at a time — but `BuildSummary.status` has no member
for it (`started` · `in_progress` · `completed` · `failed`), and a version that
has not started has no row in the ledger read at all. So a waiting version is
simply absent until its run begins. Giving it words needs a `queued` member on
that enum; until then the page says nothing rather than inventing a state.

**Only the deployed version may be described by where it reached.** The platform
records one deployed version per project, so every other completed version says
`Built` — saying more would be a guess.

**The Milestone cell reads `Milestone #3`.** The platform records a number, not a
title. This is the "stays discoverable on the Builds page" the build-confirm
dialog's copy promises.

### Deployments

An environment's card and its ledger row say the same word about it
(ADR-0027). Development's is the deploy aggregate's — the same fact the Builds
ledger reads as *Deployed to development* — so the two ledgers never disagree
about the version running in dev; production, which the aggregate never names,
is read off its bindings.

| Situation | Says |
|---|---|
| Every binding settled and serving | **`Deployed`** |
| A binding still converging | **`Deploying`** |
| A binding failed | **`Deploy failed`** |
| Every binding intentionally undeployed | **`Undeployed`** |
| Nothing bound | **`Nothing deployed`** |

**The Validation cell is development's alone** — the check runs against the
dev deployment, so production reads `—`. It says the counts once the join has
them (`24 / 24 passed`), the shared verdict word otherwise (*validating*,
*awaiting fix*, *validated\**), and **`Not run`** before anything has been asked.

**A row is what the environment runs now.** The platform keeps no deployment
record, so there is no *Superseded* row and no Duration cell — saying either
would be a guess. The card's age (*2h ago*) is the newest binding's stamp.

**Test users are counted on the card and listed in a dialog.** The card says
**`N accounts, one per role`** beside **View test users**, because the list
grows with the design and the card must not. The dialog is titled **Test
users** and says what they are — *Disposable accounts the platform created for
this project's roles, so agents can sign in to the running app and check what
each role can do. They are not real people.* Its columns are Username ·
Password · Role · Cold start, and a password is **`**********`** until the eye
beside it is pressed. The accounts are the project's own, one per role its
security design declares; the Thunder Console link below the count is for
**real** accounts, which is a different thing and says so.

### A task's row, on a build

| Situation | Says |
|---|---|
| Its pull request merged | **`Merged`** |
| Waiting on a dependency someone must configure | **`Blocked`** + **`Configure in Resources`** |
| The agent is on it now | **`In progress`** + elapsed time |
| Its pull request is open and nothing has merged it | **`PR sent`** |
| Nothing has run yet | **`Pending`** |

**One pull request, several rows.** A build session dispatches ONE pull request
that claims a SET of issues, so every issue in that set says the same thing —
the row is reporting the pull request's state, not a state of its own. That is
why three rows can all read `PR sent` at once.

`PR sent` is the one place the console keeps an acronym (naming rule 4). It sits
inches from the `#9` chip the row already shows and from the Coding agent log
that names pull requests by number; the reader here is the engineer whose issue
it is, and *`Pull request sent`* is a chip twice the width saying nothing more.

Counts read **`11 in this build · 5 done · 2 need your attention`**. *Need your
attention* folds blocked and PR-sent together deliberately: both are waiting
on the reader, which is what makes them one number.

**A row's title is text, not a link.** The per-task detail page is not a
destination this surface sends anyone to — the row already carries what that
page led with (state, the agent's newest note, elapsed time), and the `#4` chip
goes to the issue itself, on GitHub. A link to a view nobody uses is a dead end
that looks like a destination.

**Rows read ascending by issue number** — the order the milestone was planned
in. The gates the platform files first come first, and the work that depends on
them follows. `list-tasks` promises no order, so GitHub's newest-first default
was showing through and the list read backwards.

The row's second line is the issue's **newest comment by any author**, flattened
to its first non-empty line. Not "the agent's latest note": the platform's own
machine comments are already excluded server-side, and what remains — the coding
agent's progress notes and whatever a human wrote — cannot be told apart by
author, because the platform comments through the org's own credential and the
runner is handed that same credential. A task with no comment shows no second
line at all: eleven rows each reading "No updates yet" is noise, not
information.

**The ledger has no Tasks column.** A version's task counts cannot be attributed
from a read that spans versions, and asking per row would be one GitHub-backed
request each. The breakdown is on the build page, where the read is already
scoped to one version.

### External resources, on a build

Decided in [ADR-0023](../../../docs/decisions/ADR-0023-external-dependency-values-are-a-deploy-gate.md).
The values an external dependency needs are no longer asked for in front of the
Build button; they are supplied on a version's build page, as a section sitting
directly under Tasks.

The PAGE is version-scoped; the CONFIGURATION is not. It belongs to the project
and the environment, so every version's page shows the same answer, and a
dependency configured on one releases whichever run is parked on it. Copy here
must not imply otherwise — "this version's credentials" would promise a
per-version answer the platform does not have.

| | |
|---|---|
| Section title | **External resources** |
| Its chip, all supplied | **`3 of 3 configured`** |
| Its chip, some outstanding | **`2 of 3 need configuration`** |
| Body, all supplied | *Every external dependency has its development configuration.* |
| Body, some outstanding | *The agent builds while you supply these. The version is not deployed until every one of them has its development configuration.* |
| A row that is configured | **`Configured`** + **Edit configuration** |
| A row that is not | **`Needs configuration`** + **Configure now** |
| A row's second line, no description | *2 settings outstanding* / *2 settings stored* |
| After a save | *Configuration saved — the deployment no longer waits on this one.* |

**`Needs configuration`, never *Unconfigured* or *Not ready*.** It names what the
reader must do, not the state machine's word for the row. And *configured* is
deliberately not *ready*: OpenChoreo reports these bindings `Ready` while every
key is still empty, so the two words name different facts and must not merge.

**One noun: *configuration*, never *values* or *credentials*.** The section used
to say "values", which is the wire's word for what a binding holds, and the
drawer before it said "credentials", which is only true of the subset that are
secrets — a webhook URL is neither. *Configuration* covers every key the dialog
collects and is the word the buttons already used. The individual keys are
**settings** when they have to be counted.

**The row's status is plain toned text, not a second pill.** The section header
already carries a chip; a chip on every row competes with the button it is meant
to lead the eye to. The outstanding row takes the section's one filled button
(**Configure now**), a configured row the quiet outlined one.

### A version parked at the deploy gate

A run held at the deploy gate is **unbounded, and only a person can end it**, so
`waiting` on its own reads as a hang. The build page says so in three places,
and they must agree.

| | |
|---|---|
| The page's status pill | **`Waiting for configuration`** |
| The summary card's notice, naming what it waits on | **`Waiting for configuration: stripe, sendgrid`** |
| The same notice when the run named nothing | **`Waiting for external configuration`** |
| Its body | *Everything built. This version is not deployed until every external resource holds its development configuration — add it under External resources below and the run resumes and deploys on its own, with nothing to restart.* |
| Its button | **Add configuration** |
| The card's rollout line | ***v2** is built and waiting for its external configuration.* |

**"with nothing to restart" is the load-bearing half.** Without it the reader
goes looking for a Build or Retry button that would start a second run.

**The ledger row says `Waiting for configuration` too, and it is the same words
as the pill.** `BuildSummary` carries the waiting reason, which is what separates a
parked version from a running one — both are `in_progress` — and it costs the
ledger nothing: the run row it is built from already holds it. The row also goes
QUIET, no tint and no pulse: those mean "the moving thing", and a park is the
opposite. The dependency NAMES stay on the build page, where the run read that
carries them is already being made and there is room to list them.

### The build page's log sections

Tasks, External resources, Coding agent log and Build logs are peers on one
page, so they label and empty themselves the same way. Two things had drifted:
one section's placeholder was a bare left-aligned paragraph beside another's
centred `EmptyState`, and the agent log carried a status pill where Tasks
carried plain text.

| | |
|---|---|
| Header note, Tasks | *7 in this build · 7 done* |
| Header note, agent log, streaming | *Streaming* + `AgentPulse` |
| Header note, agent log, run over | *Run finished successfully* / *Run failed* / *Run cancelled* / *Run blocked* |
| The same, state unknown or absent | *Run ended — `<state>`* / *Run finished* |
| Any section with nothing to show | a centred `EmptyState compact` |
| The agent log's stream health | *Attaching to the run feed…* / *Connection lost — reconnecting…* |

**A header note is secondary caption text, never a pill.** The page header
already carries the version's toned status chip; a second toned pill a few
inches below competes with it for the same glance.

**`Run finished successfully`, never `run settled — succeeded`.** *Settled* is
the stream contract's word for the transition, not something a person watching a
build says, and the state used to be pasted on raw in its lower-case wire
spelling. It draws on the same vocabulary as the run status chip — *succeeded*
becomes *finished successfully*, never *settled* — so a reader meets one word
for one outcome, phrased for the slot it sits in.

**How the run ENDED belongs in the header; how the STREAM is doing belongs in
the body.** They answer different questions and come from different reads — the
ending from the run list, which the streaming note beside it already reads, so
the two halves of one label cannot contradict each other. Stream health is
right-aligned under the log, away from where its content starts.

**A run that is neither streaming nor terminal is labelled by neither.** A run
parked at the deploy gate has not ended, and *Run finished* over its log would
contradict the summary card telling the reader it is waiting on them.

## The project overview

### Where project status lives

**One chip, in the global header beside the project switcher** — plus one on each card in the
projects listing. **Not** on the overview page header, the Issues page or the Deployments page.

The chip was never redundant, it was standing in the wrong place. Beside the project name on the
overview it restated the stage cards six inches below it — `specChip`'s own comment admits this
("the same three states the spec stage card renders"), and `deliveryChip` only picks the loudest
of `build.status` / `deploy.status` by priority, synthesising nothing. Meanwhile the **projects
listing carried no status at all**, which is the one place a one-line summary genuinely earns its
slot: many projects, no cards on screen.

In the global header it is visible from every page in the project, always in the same position.
Accepted trade: on the overview the chip and the cards are both on screen. Consistency of
location beats zero redundancy.

Its words come from the state table below — no chip-specific vocabulary:

| condition | chip |
|---|---|
| no spec yet | **Writing requirements** |
| spec, no version | **not built yet** |
| spec versioned, edited since | **`v1 · edited`** |
| spec versioned, clean | **`v1`** |

### What the chat shows while an agent works

**Your own message appears the moment you send it**, not when the platform accepts it. Dispatch
resolves a repository, a workspace ref, an API key, two git heads and two snapshot extracts before it
answers, and a message that has not appeared yet is indistinguishable from one that was dropped.

**A turn you did not send still shows who started it and what they said.** That covers the kickoff —
which no browser sent — and a teammate's turn, which used to render under a blank space and be
attributed to *you*. The turn itself carries the line
([#562](https://github.com/wso2/labs-agentic-engineer/issues/562)); the transcript store cannot,
because it only records a turn once the turn has finished.

### Track grammar

The three stages are **one track**, not three cards: one bar, three legs, each carrying a step
numeral and separated by an arrowhead at the seam. Three cards with three borders read as three
features; they are one version moving through three gates.

Every leg says the same things in the same slots, so the pattern is learned once:

- **which stage** — a step numeral (`01`/`02`/`03`) and the name. The numerals are not decoration:
  you cannot build before you publish or deploy before you build, so order is information here
- **where it stands** — one line, always the user's situation, never the system's dependency
- **version** — only when one exists. No em-dash placeholder; blank says "not yet" better
- **where it goes** — the whole leg is a link to that stage's section

| state | line | version | lit |
|---|---|---|---|
| not reached | *Nothing built yet* | — | no |
| the platform is working | *Building* | yes | accent, pulsing |
| waiting on **you** | *Draft changes, not published* | yes | amber, still |
| settled | *Built* | yes | no |
| failed | *Build failed* | yes | error, still |

**Nothing on the track is a button.** Every way of *starting* work lives on the page that owns it,
which is where the context to choose it lives too — the spec view offers *generate designs* and
*start building*, and a `Publish` button on the overview would be the renamed-caption mistake below
wearing a different verb. This supersedes the earlier rule that a stage carried a CTA "when the flow
stopped there" ([#522](https://github.com/wso2/labs-agentic-engineer/issues/522)): the card that rule
was written for was not clickable, and a button inside a link is a broken target.

**Lit means unsettled, and the colour says who is holding it.** Accent with a pulse is the platform
working; amber and still is the platform waiting on the user. A page that animates while it waits
for you to type is lying about who is busy. A settled leg is quiet — if every visit glows, the glow
stops meaning anything.

**More than one leg can be lit.** Amending the spec while the platform builds what you last
published is a legitimate state, not a race, and any rule that forces a single "current" stage has
to pick one and will pick wrong.

**Validation is a phase of deploying, not a fourth leg.** It only runs once the components are up,
so it rides the deploy line (*Live in dev · validating*). A fourth gate would be empty in most
states, and empty gates teach the reader to ignore the bar. A red verdict fails the leg and **keeps
its version chip** — that version really is what is running in dev.

#### The summary line

One sentence under the track, or nothing. It earns its slot **only** by saying something no single
leg can — relating two legs to each other:

| situation | sentence |
|---|---|
| amending a spec while the last published version builds | *Building v1 — your draft changes are not in it. Publish the spec to build them.* |
| a newer version building over one still serving dev | *Building v2. v1 stays live in dev until it deploys.* |

Everything else has **no summary**. A sentence that paraphrases the leg above it is the same
"says it twice" problem the status chip was moved for.

#### The spec leg: no button, one line

**The leg is a link to the spec view, in every state — and it was a button before it was a link.**

It used to be three buttons — *Generate spec*, then *Open spec*, then *Continue spec* — and it walked
all three during a single kickoff **with no input from the user at all**, because each state was
inferred from a signal that moved on its own. A control that renames itself while you are reading it
cannot be learned, and the destination never actually varied. So the caption stopped varying too.

**The line is the only part that moves, and it always says something.** It used to blank itself
mid-kickoff: a turn that ends *on* a question has written no requirements, so the spec still looks
absent and the agent looks idle, and the card fell through to its cold-start wording.

| the project's situation | line |
|---|---|
| the agent is writing the first requirements | *The agent is writing your requirements.* |
| an agent is working on a spec that already exists | *The agent is working on your spec.* |
| the agent asked and is waiting | *The agent has questions for you.* |
| the kickoff died with nothing written | *The agent couldn't start — open the spec to try again.* |
| nothing ever started | *Nothing written yet.* |
| otherwise | the spec's own status |

The **version** is a separate slot and survives underneath all of these, so an amendment interview on
`v2` still reads as `v2`.

**Nothing on this leg starts anything.** It is a destination in every state.

**An empty spec workspace shows one thing: a centred spinner over *"Agent is working on the
requirements document"***, the same shape the architecture pane uses while a design turn runs. It is
keyed on the **file list**, not on the agent's status — `spec.agent` returns to `""` in every gap
between turns, and each time it did the user was handed *"Select a file to view its content"* over a
workspace with no files in it to select. A failure has its own banner, so an empty workspace without
one means the work is pending or in flight either way.

**An empty spec workspace offers nothing either.** *"The workspace looks empty"* is true for a while
before the agent's first write lands — so a button gated on it appears **during the kickoff**, which is
precisely the moment the user must not be invited to restart. The only state carrying a way out is the
one that can be *known* rather than inferred: a turn that started and then died.

| | |
|---|---|
| Title | **The agent couldn't write your requirements** |
| Body | *Nothing was lost — anything already written stays browsable.* |
| Action | **Retry** |

The previous copy ended *"Ask the agent to continue from where it stopped in the chat panel"*, which
[#530](https://github.com/wso2/labs-agentic-engineer/issues/530) forbids: a command the UI can offer as
a control is offered, not described.

**A project that never got a turn at all** — a dispatch that never reached the turn guard (no Anthropic
key, an unreachable skills repo), or an abandoned document upload the create held the kickoff for — is
its own state, not an absence. It shows the same **Retry**, over *"Nothing written yet."*

That distinction is load-bearing. *Nothing has run* and *between turns* look identical in git, and need
opposite treatment: one needs a way to begin, the other is mid-interview and must not be offered a
restart that would supersede it. Collapsed, the first showed a spinner for work that was never coming,
with nothing to click.

**Everything else is the chat's job.** The card says where the project stands in one line; the panel
carries the conversation, the questions and the agent's work.

**Ghost cards stay clickable.** Their destination teaches what the section is for
([#533](https://github.com/wso2/labs-agentic-engineer/issues/533)), so the click is a lesson, not
a dead end.

| was | is |
|---|---|
| `waiting on spec` | **Nothing built yet** |
| `nothing deployed` | **Nothing deployed yet** |

`waiting on spec` named the system's dependency; naming rule 6 requires the user's situation.

### Repository preparation is loading, not status

Cloning is async and the user cannot influence it, so a progress label is noise with extra words.
It folds into the **overview's own loading state** and is never labelled.

Only failure surfaces, and not as a chip: an **alert** reading *"Unable to clone the
repository"*, carrying `repoErrorMessage` and a **Retry**. A failed repo means nothing in the
project can work — no spec commits, no build runs — which is more than a pill can carry, and
today's `Repository error` chip discards the message the contract already provides.

## State

| Situation | Says |
|---|---|
| Spec versioned, unchanged since | **`v1`** |
| Spec changed since its version | **`v1 · edited`** |
| No version yet | **`not built yet`** |
| Agent writing requirements | **Writing requirements** |
| Agent deriving design | **Designing…** |
| Collab server unreachable | **offline** |
| Build gate not satisfied | **Not ready to build yet** |

**published**, **draft** and the `v1+` diff suffix are retired — all three imply a
review-and-release model AEP does not have, and `+` is a convention the user was never taught.
`solo session` becomes `offline`: shorter, and it does not read like a focus feature.

## Questions

The word had two referents. It now has one.

- **Open questions** — recorded gaps in the PRD: numbered entries under `## Open Questions`.
  Keeps its name; it is accurate and standard. Defined in `CONTEXT.md`. They **block nothing** —
  not design, not Build ([#539](https://github.com/wso2/labs-agentic-engineer/issues/539)).
- **Questions for you** — the agent's live request for input. Renamed away from the collision:
  the chat bubble says **"The agent needs your input (5)"**, the form is headed **"Questions
  for you"**. Nothing parses these, so they were the cheap side to move.
- The form's exit is **"Use recommended answers"**, not *"Skip questions"*. It skips nothing — it
  makes the agent decide, and those decisions land in the document. Safe to click now that each
  one lands flagged and one click from being revisited
  ([#532](https://github.com/wso2/labs-agentic-engineer/issues/532)), so the label can be plain
  rather than cautionary.

**defer** had been a gate tooltip's advice with no affordance behind it. The gate is gone
([#539](https://github.com/wso2/labs-agentic-engineer/issues/539)) and deferring is now an outcome
of the `/settle` conversation — see **Two kinds of unsettled**.

## The spec view's artifact rail

**The rail is the flow.** It is already ordered — Requirements, Design, Validation — already where
the user reads, and now carries state at both levels plus **Build as its terminal step**. One
surface answers *what exists*, *what is happening* and *what comes next*, so the journey needs no
step bar and no second progress indicator competing with the overview's cards. Decided in
[#527](https://github.com/wso2/labs-agentic-engineer/issues/527);
[drawn here](https://claude.ai/code/artifact/fe3fc0c0-6ecd-49ed-9f75-ed65c2220cb1).

**A reference between spec documents is a link, and it opens in place.** The PRD names its feature
docs — depth lives in `features/<slug>.md` and the body stays lean — so the document is full of
pointers to files sitting two rows away in the same rail. The shared schema parses a markdown link
as an EXTERNAL one, which is wrong twice: a plain click inside the editor only places a caret, and
a click that did follow the href would leave the console for a path it does not serve. A reference
that resolves to a file the project HAS is styled as a link and selects that file; one naming a
document nobody has written yet stays plain text, because a control that selects nothing is worse
than prose. The link's text is the feature's **name** — the path is the href, never the label.

**The PRD leads Requirements.** Everything else in that group elaborates it — a feature file is
depth on a story the PRD defines — so the document the whole flow is written against cannot sit
below its own footnotes. Path order alone puts `features/…` above `prd.md`, which
[#579](https://github.com/wso2/labs-agentic-engineer/issues/579) made routine by giving `/expand` a
lens on every story; the list pins the PRD instead, and everything behind it keeps path order.

### Section state

| state | shown as |
|---|---|
| ready | green tick |
| active | **pulse**, section name in primary |
| outdated | amber warning, section name amber |
| not started | dim, no ornament |

**Outdated is the load-bearing one.** Edit requirements after a design exists and Design and
Validation go *outdated* — the same rail that reported progress reports staleness, with no second
mechanism to learn.

**An amber section carries a COUNTED alert chip beside its title**, and clicking it opens a dialog
listing what there is to resolve, each with the way to resolve it. Hovering shows the most
significant one, so a peek costs no click.

Rows beneath the title were tried first and cost the rail up to three extra lines before the user
reached the documents, in a column 280px wide; one chip costs one slot however many problems there
are. It is a chip rather than a bare tooltip because it is discoverable at rest — the hover is a
shortcut on top of an affordance, not a replacement for one. And it carries the **count**, because
three assumptions and one otherwise look identical and *how much* is what a glance is for.

| section | what the dialog lists | its fix |
|---|---|---|
| Requirements | *N questions only you can answer* | **Open the document**, where the settle controls already are |
| Requirements | *N decisions marked assumed* | **Open the document** |
| Design · Validation | *The requirements have changed since* | **Update the design** |

**Ordered by how badly it hurts to ignore**, which is what makes the hover's pick meaningful rather
than arbitrary: a design behind its requirements blocks the build and ships the wrong software if
forced; an open question is a hole only the user can fill, so nothing else can resolve it; an
assumption already *has* an answer standing, which the user may or may not disagree with.

**Build refuses the same way.** Its unmet conditions are the same kind of thing — a list the user can
act on — so they read identically rather than one being a strip under the header and the other a
sidebar. Acting from either dialog closes it and goes, so it is never dismissed twice.

Assumptions and open questions are counted **apart** — one is a judgment the agent made and you may
overturn, the other a hole only you can fill — and neither GATES anything. Designing against
assumptions is deliberate: the requirements arrive early, full of them, and are refined in place. The
rail reports; Design stays clickable throughout.

**But Generate design warns first when they stand.** The rail says the same thing at rest; the click
is the moment it becomes consequential, because the design is derived from those guesses and
overturning one afterwards means deriving again. So the dialog names what is unsettled and offers
both ways: *Review them first* returns to the requirements document, where the controls already live
on the flagged lines, and *Generate anyway* goes on.

**In the user's words** ([#666](https://github.com/wso2/labs-agentic-engineer/issues/666)): the
dialog is titled *Some decisions are still yours*, and its one paragraph says what the agent did
(*made some decisions on your behalf — marked assumed in the document*), what happens next (*the
design will be built on the requirements as they stand*) and what being wrong costs (*the design has
to be generated again*). *Settled*, *derived*, *judgment* and *to challenge* were retired from it:
they are how this file talks about the document, not how a user reads it. The rows say *N decisions
marked assumed* — pointing at the pill they will find on the line — and *N questions only you can
answer*, which is the fact that makes an open question different.

This is where the dialog's two moods separate, and the distinction is the point:

| | says | way past |
|---|---|---|
| **refusal** — Build | the platform will not do this yet | none; Close |
| **warning** — Generate design | this will cost you if you are wrong | the primary action |

A warning that cannot be dismissed is a gate wearing a warning's clothes, and gating a design run on
settled requirements was tried and removed. So the way past is the *primary* button, not a buried
link — the user is being informed, not asked for permission.

**Active follows the WORK in flight, not which sections happen to be empty.** The running turn's
flow says what it is for — settling an assumption is requirements work, a design run is design work
— and that maps straight onto the section that will change. Guessing from emptiness was wrong in
both directions: settling an assumption lit Design, because Design was the first empty section; and
the moment a design run wrote its first file the pulse jumped to Validation, while the rest of the
design was still being written.

Work the rail cannot place — a plain chat turn, an org's own skill — pulses **nothing**. An agent is
working, but a pulse on the wrong section is worse than a still rail. One exception, by elimination
rather than by guess: while the project holds nothing at all, an unplaceable turn pulses
Requirements — nothing downstream can be written before that document exists, and the turn carrying
a member's interview answers (plain prose, no flow) is the very one that writes it (#629). The
moment anything exists, the silence above resumes.

**"In flight" starts at the submit, not at the server.** `spec.agent` cannot see a turn before its
row exists, and submitted interview answers take the dispatch round-trip — seconds — to become one;
in that gap every signal above read idle and the empty workspace offered Retry against the very
interview it could not see ([#635](https://github.com/wso2/labs-agentic-engineer/issues/635)). The
browser that submitted holds the missing evidence — a seeded message waiting, a dispatch awaiting
its turn id, a stream being folded — and that chain counts as agent work until the status catches
up. It is claim-scoped, not timed — a refused send releases its claim and Retry surfaces at once —
with one backstop: a seeded message whose consumer never opens (the one stage with no failure path
of its own) lapses from the signal after a generous TTL, so an outage cannot pin a working state
that hides Retry. A
teammate's browser holds no claim for a send made elsewhere and waits on the status, as it always
did.

**The counts read the LIVE document, not the committed one.** Deleting an `*assumed*` flag clears
the alert as you delete it; the committed copy is a collab flush behind, and on the agent's own
edits that lag was long enough to look broken.

**How staleness is known.** Every commit is a permanent snapshot, and every agent turn records the
commit it read the project at — so the requirements *as the last design run saw them* are still
there to compare against. Nothing is stored, so nothing can fall out of sync, and the question is
answerable for projects that predate the check. Coarse on purpose: it reports that the requirements
moved, never which components are affected. Over-marking costs one re-derivation the agent mostly
no-ops through; under-marking ships a design the user has already changed their mind about.

### Artifact state

Built as [#576](https://github.com/wso2/labs-agentic-engineer/issues/576): the skill declares what
it is about to write (`declare_plan`, ADR-0025 — fire-and-forget tool-call-as-UI), and the rail
renders the declaration.

| state | shown as |
|---|---|
| planned | **ghost row** — dim, and disabled: a control that selects nothing is worse than prose |
| writing / modifying | pulse, name in primary |
| done | a plain row — the file is simply there |
| error | warning mark, name in error |

**Planned means about to be written now.** The plan is turn-scoped, never project-scoped —
pre-creating design placeholders during a requirements turn would recreate the `Being derived…`
defect this file already removed.

**The plan arrives in stages, and the console takes the union.** The design agent writes the cell
first; only then does the component set exist, and the per-component files join the list. Repeated
declarations accumulate — first-seen order kept, restated paths ignored, no removal — the one rule
robust to an agent restating its whole plan. A count (*2 of 6*) beside the section title is what
answers "how long do I wait", and it is honest precisely because it grows.

**Every status is derived from the stream, never self-reported**: writing when the file's mutation
starts, done when it completes, error only for the entry being written when the turn died. A clean
turn's plan **dissolves** — the files are the record. A dead turn's **wreckage persists** — the
done ticks, the one error, the remaining ghosts — surfaced through the section's attention chip
(*The design run didn't finish* → **Update the design**) until the next declaring turn replaces it.
Recovery is the `/design` delta pass, never a per-file retry.

**The chat records each declaration as an activity step** — *Planned 3 documents*, then *Planned 4
more documents* as the plan grows. The rail is the plan's real rendering; the chat row keeps the
activity record complete. `start` declares nothing: a single-file turn's count answers nothing,
and a skill that declares nothing leaves the rail exactly as it was.

**The editor follows the write** (ADR-0026): each artifact is selected as its write starts, in
whatever renderer it already has. The first manual selection is a declaration of reading intent
and ends the following for the rest of the turn — the rail's pulse on the writing entry stays the
one-click way back in. This supersedes the cell's burst navigation, which yanked back even over a
manual selection.

### The pulse

Work in progress is the app's existing `agentChatWorkingPulse` — an 8px `primary.main` dot,
opacity .3→1, scale .85→1, 1.2s ease-in-out, from `WorkingIndicator.tsx`. Not a spinner, and not a
second animation: "working" looks the same everywhere it appears.

### Build stays in the header

Build was drawn at the foot of the rail as its terminal step. It stays where it is: a call to
action at the bottom of a scrolling list is not where a user looks for it, and the header button is
visible from every part of the workspace rather than only once you have reached the end of a list.

**An outdated design still blocks it**, as one more entry in the refusal Build shows on click —
*"the requirements have changed since this design was written — update the design before building"*.
The refusal is a **dialog**, headed *"Not ready to build yet"*, the same one the rail's alert chip
opens. The button stays live and the same click re-checks, exactly as it does for a missing
dependency. No disabled control and no tooltip.

The refusal is enforced by the **build gate on the server**, not by the console: a block the client
draws is not a block. The rail's amber Design row carries the same repair for anyone who meets it
there first.

**An outdated design blocks Build.** Building it would implement something the user has already
changed their mind about. This is the one gate that survives the map's general "progress with
unknowns is fine" rule, because the design is not *unknown* — it is known to be wrong.

**"An agent is working" is no longer stated as a gate.** The rail shows live per-artifact state,
larger and more usefully than a disabled button with a hover explanation could.

**Reading the design is optional.** A user may go through every artifact before building, or click
Build without opening one — both are correct use
([#529](https://github.com/wso2/labs-agentic-engineer/issues/529)). Nothing is acknowledged,
certified or "passed". The only two things that ever hold Build back are the design being
**incomplete** (undeclared dependencies) or **wrong** (outdated) — never whether a human looked at
it. So no confirm step, no review checklist, no approve-then-build two-step: ADR-0007 stands, and
Build remains the approval.

### Recovery is `/design`, not a per-file retry

An errored artifact and a stale design resolve the same way: re-run design as a **delta pass**,
which sees what is missing or behind and updates it, grilling first when a change is significant.
Not a scoped per-file regeneration — artifacts are derived from each other, and the cell fixes
the component set everything else hangs off.

### Three surfaces, three jobs

| surface | job |
|---|---|
| chat | **narration** — why, and what the agent is thinking |
| artifact rail | **structure** — what, and how far |
| editor | **the artifact** — content, streaming |

**The chat does not echo the rail.** Progress prose — *"drawing the
architecture"*, *"now the component designs"* — was tried during #576 and
removed: watching the artifacts appear as they are written already tells the
user what is happening, so a line announcing each step restates the rail in
words and reads as filler. The chat speaks when it has something the other two
surfaces cannot carry — a decision, a question, a failure — and is otherwise
free to stay quiet while it works.

The chat panel is the spine and **never collapses itself**; only the user closes it. But it stops
pointing at a form that already owns the screen, and its composer stays live during a form — the
agent is waiting on the user, not working, and the user may want to talk instead of fill.

## The criteria pane

The read-only pane behind the Validation section's document. A reader meets it
cold: the rail carries no explanation, a design turn mints the file as its last
step with no announcement, and the only sentence in the product that said what
criteria were for lived in the **Validations** empty state, on another page most
readers never reach.

| | |
|---|---|
| Description, under the heading | *Each criterion represents one thing your software must do, based on your requirements. After every deployment they are checked against the running software, and the results appear under Validations. To change one, ask the agent.* |
| Checked by a test | **`AUTO`**, tooltip *Validated automatically by the agent.* |
| Checked by a person | **`MANUAL`**, tooltip *Requires manual validation.* |

**`AUTO`, never `E2E`.** The stored value stays `e2e` — the validation runner,
the report generator and the per-criterion spec path all key on it — so the badge
carries a **display name** instead, the same split the run-state chips already
draw. `E2E` was never a copy decision: it was agent-authored JSON rendered
verbatim, which is how an unexpanded acronym reached the screen past naming rule
4. The rule now has a place to bite, because the word is finally a string
somebody wrote.

**Every badge earns a tooltip, and only the two real methods get one.** A third
value exists in older documents; it renders bare rather than being given an
invented explanation.

**"Ask the agent", not an edit control.** There is no way to edit a criterion
here, by design: they are written from the requirements alone and never from the
design, so they judge the work rather than describe it. The chat panel is on
screen throughout, so this points at something the reader can use rather than
narrating a procedure (rule 3).

**The description belongs to the spec view, not to Validations.** Both surfaces
render the same pane, and Validations suppresses it — a reader there came for run
results, so a sentence promising that results appear under Validations is
redundant on the page holding them.

**Unsettled: `deployment` or `build`.** This description says criteria are checked
*after every deployment*; the **Validations** empty state says *"After a build,
your software is checked against the validation criteria in your spec"*. Both name
the same event. *Deployment* is the more accurate word, since validation runs
against the deployed system and needs its resolved endpoints, but that empty state
was out of scope when this pane was written. Whoever settles it changes both and
deletes this note.

### What a criterion is doing, while a run is under way

A validation cycle holds an agent for up to two hours. Before this, every row in
the pane read **`Pending`** for all of it, and a repeat attempt was worse — it
showed the *previous* attempt's verdict, so the criteria a repair was actively
re-working sat on **`Failed`** while it fixed them.

The row now says what is happening to it. In the order a reader meets them:

| | |
|---|---|
| Nothing has happened to it yet | **`Pending`** |
| The run has decided how it will check this one | **`Planned`** |
| Driving the running software to learn how to check it | **`Exploring…`** |
| Writing its check | **`Authoring…`** |
| Its check is running | **`Running…`** |
| It worked, then broke — being repaired | **`Healing…`** |
| Settled | **`Passed`** / **`Failed`**, unchanged |

Above the rows, one line, and only while they have nothing to say:

| | |
|---|---|
| Nothing picked up yet | *Setting up the test harness…* |
| All settled, no results published | *Writing the validation report…* |

**The trailing `…` means in flight.** Every word that can still change carries
one; the two settled words do not. It is the only signal separating "this is
happening now" from "this is the answer", and both sets sit in the same column.

**Only `Healing…` is coloured.** It is the one live word that changes what a
reader thinks is happening — something that worked has stopped working. Colouring
ordinary progress would spend attention on the common case and leave none for
this one.

**Live words are local; settled words are the report's.** `Passed` and `Failed`
come from the report file the run commits, and the live words never enter that
vocabulary — a report can only describe work in the past tense, so there is no
report word for *Authoring…*. The reverse holds too: when the feed says a
criterion passed, it renders as **`Passed`**, because that is the same fact
whichever surface delivered it.

**Rule 6, deliberately bent.** *Exploring*, *Authoring*, *Healing* name the
system's behaviour, which rule 6 forbids. They stay because here the system's
behaviour **is** the user's situation: the reader has handed work to an agent and
is watching it, and "what is it doing right now" is the whole question. The rule
protects against a reader being told about machinery they did not ask about —
not against answering the one thing they came to find out.

**Unsettled: *test harness* and *validation report*.** Both run-wide lines name
internal artifacts, which rule 6 has a better claim over — a reader does not have
a harness, they have criteria waiting to be checked. They are the two windows
where nothing else moves, so something had to be said; whoever finds better words
changes them here first.

## What a change invalidates

**Numbered decisions, precise where the link was recorded, coarse where it was not.**

### Product Decisions get numbers

They are unnumbered prose bullets today, so nothing can cite them — which matters because
**assumptions *are* Product Decisions** (`prd-contract`: *"skip-valve entries carry the `*assumed*`
tag"*), and [#532](https://github.com/wso2/labs-agentic-engineer/issues/532) made those flagged,
clickable and expected to be reversed routinely. The most-reversed thing was the untraceable one.

They are numbered **in the PRD, exactly where stories already live** — no new file, no new store.
The number is the join key, permanent and append-only, same rule as story numbers. `design.json`
gains a sibling field:

```ts
stories:   z.array(z.number().int().positive()).optional(),
decisions: z.array(z.number().int().positive()).optional(),   // new
```

Validation criteria trace to **stories** — behavior — and need no decision citations.

### What goes outdated

| what changed | marked outdated |
|---|---|
| a **story** | exactly the components and criteria citing it |
| a **cited decision** | exactly the components citing it |
| an **uncited decision**, the problem statement, out-of-scope, anything else | the whole **Design** section |

**Never a guess.** An uncited decision is ambiguous — genuinely design-irrelevant, or the agent
did not record the link — so it falls back to coarse rather than inferring from wording.

### Why coarse is the safe fallback

The two failures are not symmetric. **Over-marking** costs one delta pass: the user clicks *Update
the design*, `/design` finds what is still consistent and leaves it alone. **Under-marking** leaves
a stale design unflagged, so Build is not blocked and coding agents implement something the user
already rejected.

This is also why stories can be precise and decisions cannot always be: the build gate **enforces**
story coverage, so those citations are complete by construction. No equivalent enforcement is
possible for decisions — plenty have no design consequence at all — so their citations are
best-effort, and best-effort links may only ever *add* precision, never withhold a warning.

### Display and detection are separate

The rail's **outdated** section state is the display
([#527](https://github.com/wso2/labs-agentic-engineer/issues/527)); this is what feeds it. The
`/design` delta pass is what acts on it. **`dirty` is unaffected** — whole-spec and boolean, it
answers *"has anything moved since we built?"*, which is a different question.

## Notifications

**One bell, for both.** The global `NotificationBell` already sits in the header on every page and
already carries the SRE agent's RCA reports. It also carries **the agent asking for input** —
design-time questions, dependency declarations, anything blocked on the user. Two bells would be
worse than one mixed list, and this is the only surface visible from outside the spec view.

**Outstanding, not unread.** Alerts track unread client-side, which is right for a report you have
seen. It is wrong here: glancing at a request does not answer it. A request for input is
**outstanding until the thing is done** — an unanswered question form, an undeclared dependency —
which the platform can derive. It clears when the user acts, never when they look.

**Typed and actionable.** *"expense-api needs a SendGrid key"* and *"production is returning 500s"*
must not read alike, and a request must take the user to the thing. Alerts are read-only today;
these are not.

**The bell and the Alerts page are deliberately different scopes.** The bell is the notification
center — incidents *and* requests. The **Alerts** page stays the SRE agent's incident list across
projects. Recorded so the divergence reads as a decision rather than drift.

**The status chip does not carry this.** Two words cannot say *which* question, cannot carry an
action, and would displace the state the chip exists to show. It stays a state display
([#544](https://github.com/wso2/labs-agentic-engineer/issues/544)); this is an alert's job.

**In-context markers still stand** and are not replaced by the bell: the artifact rail's section
state, Build's reason at the foot of the rail, flagged lines in the PRD. The bell is for when the
user is *not* looking at any of them.

**Nothing auto-navigates.** The notification is how the user finds out; clicking it is how they go
([#522](https://github.com/wso2/labs-agentic-engineer/issues/522)).

**That holds for a question arriving, too.** The chat says one has — *"The agent has N questions ·
Answer them →"* — and the **click** is what opens the form. It used to move the user the moment an
unanswered question appeared, which was tolerable when reaching a question meant having asked for
one; since the kickoff fires at project creation every project produces one in its first minute, so
it threw every new user off the page they had just landed on, before they had read a word of what the
agent was doing.

## Empty states

**An empty state teaches *what*, offers the action, and does not narrate the *how*.**

The distinction is between prose and affordance. *"Publish your spec and click Build in the spec
view to…"* restates a sequence that lives elsewhere, in words that go stale — which is why these
strings became the console's largest concentration of retired vocabulary. A **button** is a
destination, not a description: it survives a flow change, and it is the only thing standing
between a user on an empty page and the rest of the product.

The artifact rail is the flow ([#527](https://github.com/wso2/labs-agentic-engineer/issues/527)),
but **it only exists on the spec view** — a user on Builds cannot see it. So an empty state may not
duplicate the sequence, and must not strand the user either.

**A false CTA is worse than none.** Offer one only where the user can genuinely act; four of these
five surfaces fill themselves.

| surface | says | action |
|---|---|---|
| Builds | **No builds yet.** A build hands your design to coding agents, which write your components and open pull requests. | **Go to the spec** |
| Deployments | **Nothing deployed yet.** Your components run here once they are built — each environment shows what is live and where to reach it. | — |
| Validations *(never validated)* | **Nothing validated yet.** After a build, your software is checked against the **validation criteria** in your spec; results appear here. | — |
| Validations *(version skipped)* | **This version was not validated** — it has no validation criteria, or it was an incident run, which gets no validation cycle. | — |
| Components *(overview)* | **No components yet.** Components are the services and apps your design is made of — they appear as agents build them. | — |
| Architecture *(overview)* | **No architecture yet.** Once the agent designs your app, its components and the connections between them are drawn here. | — |
| Chat | **Hi! I'm your Agent.** This is where we talk through what you're building. Ask about a decision, change what's in scope, or take up anything I marked as assumed. | the composer, plus three suggestions |

**Chat's empty state is the one a user reaches last, not first.** Since
[#522](https://github.com/wso2/labs-agentic-engineer/issues/522) fires the kickoff at project
creation, the first transcript is never empty; this appears only after **New conversation**, on a
project that already has a spec. The old string — *"Ask me to edit this project's spec — I join the
shared workspace and you can watch the files change live"* — invited the user to start something
that had already started, and narrated the *how* (a shared workspace, files changing) that nothing
on screen calls by those names. Its suggestions offered to draft requirements that exist. Both now
open a conversation **about** the spec.

**The document and its rows carry different names, on purpose.** The **Validation criteria** are
the document; one row inside it is an **acceptance criterion**, which is what its `AC-` id says and
what the term means everywhere else. Naming the document after one of its rows — the earlier
*Acceptance criteria* label — cost the link between the criteria and the runs against them, and
took a sentence of empty-state copy to restore. They share a root again, so nothing has to.

**Validations has two empty states, and only one narrates.** The page is version-scoped, so a
version that was skipped — no criteria, or an incident run — is a different fact from a project
that has never validated. The *version skipped* sentence explains **why** the page is empty
without saying how to fill it, so it conforms as written
([#577](https://github.com/wso2/labs-agentic-engineer/issues/577)).

**Retired from these strings**: *published* / *publish the plan* / *the published design* (there is
no publish step — Build is the act), *plan* (not a term in this file), *AEP*.

**Not an empty state:** *"Issues is on its way"* — a feature that does not exist yet is a different
thing from a surface with nothing in it, and reads differently on purpose.

Ghost card lines on the overview (*Nothing built yet*, *Nothing deployed yet*) are the same voice at
card size — see **The project overview**.

The shared `EmptyState` primitive already carries the shape — including the `action` prop this
rule depends on (icon, title, description, optional action, `compact`, `bordered`). Nothing new is
needed structurally.

## Two kinds of unsettled

The PRD carries both, they look similar, and they are not the same thing. Decided in
[#532](https://github.com/wso2/labs-agentic-engineer/issues/532).

| | **Assumption** | **Open question** |
|---|---|---|
| what it is | a judgment the agent made | a hole nobody has filled |
| why | the agent *could* decide, so it did | a fact **only the user holds** — a URL, a package, which vendor you have a contract with |
| in the document | flagged `*assumed*`, **doing real work** | listed under `## Open Questions` |
| clicking it | challenges a decision that already has an answer | answers it for the first time |
| blocks design? | no | no |

**Neither blocks anything.** Not design, not Build
([#539](https://github.com/wso2/labs-agentic-engineer/issues/539)). What replaces the old
design gate is the agent simply **asking** at the moment a question matters — the interview cap is
gone, so an unknown that genuinely stops the work becomes a question rather than a refusal. An
unanswered question that truly prevents implementation surfaces as a **dependency**, and those do
block Build.

**`deferred` survives, with a better job than it had.** Its only function used to be releasing the
design gate. With the cap removed the agent may raise the same question round after round, so
deferral is now the user's way to say *"I know — stop asking."* It is an outcome of the `/settle`
conversation, never a separate control.

The test is **what kind of thing the answer is**. A judgment the agent can make is assumed and
flagged; a fact only the user holds can never be invented, because an invented API URL does not
fail at review, it fails at build.

An assumption says *"I decided this, correct me."* An open question says *"nobody has decided
this yet."* Both are one click from a grilling session, which is what makes assuming a
**deferral with a handle** rather than a loss.

## How the agent talks

The agent's prose is product surface — the user reads it in the console — so this file governs it,
same as any label. But the agent runs in more than one place, and **the right vocabulary belongs to
the surface, not to the skill**: in an agentic coding tool the user is standing in the repo, so
`design.cell` is exactly the right word. In the console it is not.

**A console skill carries the difference** — `skills/console/`. Every caller names the **surface**
its turn will be read on, so the BFF sends `surface: "console"` and a local playground run sends
nothing; the shared flow skills (`start`, `design`, `amend`, …) stay identical in both. It is
**standing policy inlined into every console turn's system prompt** under `# Narration policy`, not
a catalog skill loaded on demand, following the `organization` skill's precedent: *"An agent that
has to remember to load it asks questions the org already answered."* An agent that has to remember
its narration rules will forget one and quote a path.

It **outranks per-flow narration.** The system prompt used to say *"A LOADED skill may define the
narration for its own flow"* flat; it now says *"unless a standing narration policy in this prompt
overrides it"*, and the policy block is composed last, so it is the system prompt's final word.
Three skills had used that freedom to mandate the very output this removes — and since their bodies
are inlined into the *turn* rather than the standing instructions, the override is repeated there
too, in the same message as the text it overrides.

### The rules it carries

1. **Name things the way the UI names them.** *Architecture*, not `design.cell`. *Validation
   criteria*, not `specs/validation/validation-criteria.json`. The mapping is the table under
   [The spec workspace](#the-spec-workspace) — this file is its source, and the console skill is
   how it reaches the agent.
2. **Never quote a repo path** to the user.
3. **Never tell the user to type a command the UI offers as a control.** The walkthrough's *"Next
   step: review the PRD, then run `/design`"* was said while a **Generate design** button sat on
   screen.
4. **Describe the work, not the file operations.** What the user got, not which files moved.

### What changes in the shared skills

The agent was obeying instructions, not improvising — so the instructions are what changed. A path
pointer is genuinely useful in a local run, where the user can open it, so these mandates were not
deleted from the trunk; they simply stopped being unconditional, and the console skill overrides
them:

| skill | the mandate |
|---|---|
| `design` | *"Close with three parts and nothing more: … a one-line pointer to `specs/design/`"* — kept, overridden on the console |
| `architecture` | *"…and a one-line pointer to `specs/design/`"* — kept, overridden on the console |
| `start` | *"review `specs/requirements/prd.md`, then run `/design`"* — **removed**: it named a command the console offers as a button, which is wrong on every surface once the button exists |

## Commands

What the user types or the UI fires on their behalf. These appear in the transcript, so they are
product surface and this file governs them — unlike **skill** names, which are engineer-facing,
route by catalog description, and should never be what a user reads. `amend` did not need
renaming; it needed to stop being visible.

| the user's intent | command | where it is offered |
|---|---|---|
| start from an idea | `/start with <idea>` | fired at project creation ([#522](https://github.com/wso2/labs-agentic-engineer/issues/522)); the idea rides along, cropped, so the user can see the agent is working from **their** words rather than a bare command ([#528](https://github.com/wso2/labs-agentic-engineer/issues/528)) |
| add a feature | `/feature <idea>` | code lens on the story list — opens the aim box to collect the idea, and sends the command plus their words |
| add an actor | `/actor <who>` | code lens on Actors — same collecting box |
| go deeper on a feature | `/expand <story>` | code lens on the story, which carries itself as the subject |
| answer an open question | `/settle <the point>` | code lens on the question |
| take up the open questions | `/settle` over the section | code lens on **Open Questions** |
| talk a line through | *Discuss* — no command; opens the aim box on the line, Enter sends Discuss | code lens on any bullet |

**An `*assumed*` run offers verdicts, not a command**
([#652](https://github.com/wso2/labs-agentic-engineer/issues/652)). An assumption is a decision
the agent already made, and the user's response to it is a judgement, not a request — so the line
carries **Agree · Discuss**. *Agree* is a **direct edit**: it strips the flag and keeps the decision —
no agent turn, no model, one undo — and it stays live while an agent holds the turn, which is exactly
when a reviewer is reading flagged lines. *Discuss* opens the aim box on the line. Two, deliberately:
a line with four controls on it stops reading as a line. *Remove* and *Reopen* were built and cut for
that reason — dropping or reopening a decision is a sentence away in Discuss, and the editor deletes a
bullet as well as any control could. The word is **Agree**, not *Accept* — *Accept* is what the
agent-suggestion review bar says, and this is a different act. `/settle` on a flagged line is retired;
the marker leaving the document is itself the signal the agent reads.

**An add-lens asks before it fires** ([#666](https://github.com/wso2/labs-agentic-engineer/issues/666)):
`+ Actor` and `+ Feature` have a subject the document cannot supply, so the click opens the aim box
with one question (*Who are they, and what do they do?* / *Describe the feature in your own words…*)
and one button (*Add actor* / *Add feature*). The send is the command plus the user's words —
`/feature manager should approve` — which is the same shape the per-line lenses compose from the
entry they sit on. An empty send is the bare command, exactly what the lens did before it learned to
ask; the agent then asks in chat instead.

**A command names the user's intent, never the document operation.** `/feature` says what they
came to do; `/amend Add a feature` said what the system does to a file.

**The transcript line is `/start <idea>`, and the console adds nothing to it** — not even a joining
word. The command is set apart (monospace, and the idea in secondary text), which does the work a
connective would; the map's `/start with <idea>` is its own shorthand, not a string. The same shape also
renders a `/start <idea>` a user really typed, and the console cannot tell the two apart, so an added
word would appear inside a message attributed to them that they never wrote.

The idea is attached **server-side**: the kickoff dispatches a bare `/start`, and the platform — the only
party that can read the project descriptor — records the resolved idea on the turn's display record. So
the line is true of what the agent received without a client having composed it.

It is a **transparency device, not a store**
([#528](https://github.com/wso2/labs-agentic-engineer/issues/528)): the user never typed it, and its
whole job is to show the agent working from what they wrote. Two lines of the idea show, and the crop is
CSS — a clamped element keeps the full text for selection, copy and screen readers, and re-measures when
the user drags the panel wider, none of which a truncated string does.

Offering them **where the thing they change lives** — a lens on the section, a lens on the flagged
line — is what retires the `Actions ▾` menu as the way in. Their exact rendering in the transcript
is [#530](https://github.com/wso2/labs-agentic-engineer/issues/530)'s call.

**A section lens is always on show; a line lens appears on its entry's hover.** The section lens is
how a command is discovered at all, so it cannot hide; a twenty-story list with a lens on every
line would be twenty controls competing with the prose they annotate. The flag itself never hides —
an `*assumed*` run and an open question read as unsettled at rest, and only the control that acts
on them waits for the pointer.

That hover is what settled **`/expand` per story rather than one lens on the list**: a feature has
no block of its own in the PRD — the contract keeps depth in feature files — so the nearest thing
to "a feature" is the story line, and per-story is the only placement where the subject comes from
the document instead of the user's memory. Decided against the rendered document, where the cost
of per-line is a control that is only there while the pointer is.

**The lens is a control beside the line, not the line made clickable.** The PRD is a collaborative
editor: a line that fires a command on click is a line the user can no longer put a caret in.

**A lens that starts a turn goes inert, saying which, while an agent holds the turn** — the same
two conditions that gate the header's launcher, since firing a command mid-interview supersedes
the live question form for the whole room. Agree starts no turn and never
goes inert: it is the document being edited, and the document is always the user's to edit.

Shipped in [#579](https://github.com/wso2/labs-agentic-engineer/issues/579); the verdicts in
[#666](https://github.com/wso2/labs-agentic-engineer/issues/666).

## Navigation

The project sidebar is, in order: **Overview · Spec · Builds · Deployments · Validations ·
Issues**. All six stay visible and enabled from project creation
([#522](https://github.com/wso2/labs-agentic-engineer/issues/522)). `Validation` →
**`Validations`** is the only rename, per the plural rule.

The org sidebar (no project in the route) is, in order: **Projects · Resources ·
Endpoints · Alerts**. **Settings** stays in the footer in both contexts
(ADR-0010).

> Two older documents disagree with that list and with each other, both predating this file.
> **ADR-0010** enumerates five and calls Builds *Tasks*; **`PRD.md`** enumerates five and calls
> Spec *Specs & Design*. Neither mentions Validation, which ships. `PRD.md` is corrected alongside
> this file; ADR-0010's decision (the sidebar swaps wholesale to project sections) still holds and
> only its illustrative list is stale, so it is left for an explicit supersede rather than edited
> in place.

## Resources

The org's catalog of **External resources** — integrations registered once so
projects reuse them. Not a Settings page.

| | |
|---|---|
| Nav / heading | **Resources** |
| Primary action | **Register External resource** |
| Register chat | the agent asks, then drafts the form; environment values stay on the form, never in chat |

A later project's Build does not ask again for secrets the org already holds on
that name.
