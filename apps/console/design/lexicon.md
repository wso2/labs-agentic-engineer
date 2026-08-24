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
   information, never echoes its header — `ACCEPTANCE CRITERIA › Acceptance criteria` fails
   this.
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
| `DESIGN` (not `DESIGNS` — one design, several files) | **Architecture** · **Design overview** · **Security** · then per-component | `specs/design/` |
| `VALIDATION` | **Acceptance criteria** | `specs/validation/validation-criteria.json` |

The repo paths **do not change**. They are the internal language, consumed by the agents, the
runner's validation cycle and aep-api; renaming them buys nothing a user can see and costs a
migration for every existing project. This table *is* the mapping — keep it, so nobody later
"fixes" the inconsistency in the wrong direction. It holds only while the user never sees a
path, which requires the agent to stop quoting them
([#530](https://github.com/wso2/labs-agentic-engineer/issues/530)).

Placeholder for an artifact class with nothing in it: **"Not created yet"**. Active wording is
reserved for when an agent is genuinely working — the old *"Being derived…"* claimed work that
was not happening.

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

### Card grammar

Every stage card says the same things in the same slots, so the pattern is learned once:

- **which stage** — the title
- **where it stands** — one line, always the user's situation, never the system's dependency
- **what you can do** — a CTA, present *only* when there is something to do (per
  [#522](https://github.com/wso2/labs-agentic-engineer/issues/522), when the flow stopped there)
- **version** — only when one exists. No em-dash placeholder; blank says "not yet" better
- **progress** — only while something is running

| state | line | version | CTA |
|---|---|---|---|
| not reached | *Nothing built yet* | — | none |
| running | *Building 3 of 7 tasks* | yes | none |
| settled | *Built* | yes | view |
| failed | *Build failed* | yes | fix |

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

**Outdated is the load-bearing one.** Edit requirements after a design exists and Requirements
goes *active* while Design and Validation go *outdated* — the same rail that reported progress
reports staleness, with no second mechanism to learn.

### Artifact state

| state | shown as |
|---|---|
| planned | ghosted placeholder, declared **this turn** |
| writing / modifying | active; the stream already distinguishes `add` from `edit` |
| done | normal, clickable |
| error | flagged, clickable to recover |

**Planned means about to be written now.** The plan is turn-scoped, never project-scoped —
pre-creating design placeholders during a requirements turn would recreate the `Being derived…`
defect this file already removed.

**The plan arrives in stages.** The design agent writes the cell first; only then does the
component set exist, and the per-component files join the list. A count (*2 of 6*) is what answers
"how long do I wait", and it is honest precisely because it grows.

### The pulse

Work in progress is the app's existing `agentChatWorkingPulse` — an 8px `primary.main` dot,
opacity .3→1, scale .85→1, 1.2s ease-in-out, from `WorkingIndicator.tsx`. Not a spinner, and not a
second animation: "working" looks the same everywhere it appears.

### Build, at the foot of the rail

| situation | Build says |
|---|---|
| design not written yet | *after the design is written* |
| design outdated | **blocked** — *The design is behind your requirements.* + **Update the design** |

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

The chat panel is the spine and **never collapses itself**; only the user closes it. But it stops
pointing at a form that already owns the screen, and its composer stays live during a form — the
agent is waiting on the user, not working, and the user may want to talk instead of fill.

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
| Validations | **Nothing validated yet.** After a build, your software is checked against the **acceptance criteria** in your spec; results appear here. | — |
| Components *(overview)* | **No components yet.** Components are the services and apps your design is made of — they appear as agents build them. | — |
| Recent activity *(overview)* | **No activity yet.** Agents report what they are doing here as they work. | — |
| Chat | **Hi! I'm your Agent.** This is where we talk through what you're building. Ask about a decision, change what's in scope, or take up anything I marked as assumed. | the composer, plus three suggestions |

**Chat's empty state is the one a user reaches last, not first.** Since
[#522](https://github.com/wso2/labs-agentic-engineer/issues/522) fires the kickoff at project
creation, the first transcript is never empty; this appears only after **New conversation**, on a
project that already has a spec. The old string — *"Ask me to edit this project's spec — I join the
shared workspace and you can watch the files change live"* — invited the user to start something
that had already started, and narrated the *how* (a shared workspace, files changing) that nothing
on screen calls by those names. Its suggestions offered to draft requirements that exist. Both now
open a conversation **about** the spec.

The **Validations** wording is load-bearing: renaming the artifact to *Acceptance criteria* while
the section stayed *Validations* broke the link between the criteria and the runs against them, and
this sentence is where it is restored.

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

1. **Name things the way the UI names them.** *Architecture*, not `design.cell`. *Acceptance
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
| add a feature | `/feature <idea>` | code lens on the story list |
| add an actor | `/actor <who>` | code lens on Actors |
| go deeper on a feature | `/expand <story>` | code lens on the story, which carries itself as the subject |
| settle an assumption or an open question | `/settle <the point>` | code lens on the flagged line |
| take up the open questions | `/settle` over the section | code lens on **Open Questions** |

**A command names the user's intent, never the document operation.** `/feature` says what they
came to do; `/amend Add a feature` said what the system does to a file.

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

**A lens goes inert, saying which, while an agent holds the turn** — the same two conditions that
gate the header's launchers, since firing a command mid-interview supersedes the live question form
for the whole room.

Shipped in [#579](https://github.com/wso2/labs-agentic-engineer/issues/579).

## Navigation

The project sidebar is, in order: **Overview · Spec · Builds · Deployments · Validations ·
Issues**. All six stay visible and enabled from project creation
([#522](https://github.com/wso2/labs-agentic-engineer/issues/522)). `Validation` →
**`Validations`** is the only rename, per the plural rule.

> Two older documents disagree with that list and with each other, both predating this file.
> **ADR-0010** enumerates five and calls Builds *Tasks*; **`PRD.md`** enumerates five and calls
> Spec *Specs & Design*. Neither mentions Validation, which ships. `PRD.md` is corrected alongside
> this file; ADR-0010's decision (the sidebar swaps wholesale to project sections) still holds and
> only its illustrative list is stale, so it is left for an explicit supersede rather than edited
> in place.

`Validations` (the runs) and `Acceptance criteria` (what they check) no longer share a word, so
the link between them is made explicit in the section's empty state
([#533](https://github.com/wso2/labs-agentic-engineer/issues/533)).
