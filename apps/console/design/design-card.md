# The Design card

The design as its files say, reviewed the way a pull request is: each file
ticked Checked and commented on from its title line, the comments sent to the
agent together from the Overview, and the agent's questions answered there.
Decided on the design-loop map (D01, D07, D12–D14, L2, L3, L19, L21–L26);
code: `features/design/`, the prototype's part in `features/prototype/`.

## The rail

`model/designFiles.ts` (`designItems`) reads the room's `specs/design/` and
`specs/validation/acceptance/` files and lists one item per file, in the
order Design makes them, an item showing only once its file exists:

    Overview · Comments
    Architecture                 design.cell                 (cell diagram)
    Data model                   domain-model.md             (DocumentView)
    Roles and permissions        security.json               (RolesView, then SecurityView)
    <component>                  components/<c>/design.json  (DesignView)
      › Prototype                prototype.json + .tsx       (a web application)
      › Contract                 openapi.yaml                (DesignView + OpenApiView, a service)
      › Agent                    agent.afm.md                (AgentView's tabs, an ai-agent)
    Dependencies › <name>        dependencies/<n>/dependency.json (+ assumptions.md), then
                                 what blocks a build (`dep-F3`, DependencyView)
    Flows › <title>              flows/<slug>.md             (DocumentView)
    Technical decisions          decisions.md                (DocumentView)
    Acceptance tests › F<n> …    <F-n>.feature               (AcceptanceList)

Components are ordered web applications, services, agents, then any other
kind. Each item carries the path `review.json` keys it by (a prototype by its
manifest), what its hash is taken of, and which Overview list reviews it:
business (prototypes, agents, roles and permissions) or technical
(architecture, data model, contracts, flows, technical decisions). Only those
are checkable; a component's `design.json`, a dependency and the acceptance
tests are for reference and take comments only. `wireframes.dsl` is not read.
At phone width the rail is a picker above the pane. The URL names the pane
(`?art=<key>`, none for the Overview) and a prototype's full-screen review
over it (`&review=<component>`). There is no Prototype route: the chat's
Open prototype and the Track link to `?art=prototype-<component>`.

## The review file

`specs/design/review.json` (`DesignReview` in `@aep/agent-stream`) holds,
per artifact path, its comments and the hash it had when ticked Checked.
`model/review.ts` is every change to it, pure: add, edit and delete a draft,
send, reopen, check, answer the agent's question, the counts, and the one
primary. `useDesignReview` reads
it live from the room and writes it whole through the room's Y.Doc
(`setDocFile`, origin `console:user`): the file is read again at the moment
of writing, the change runs on it, and the result is checked with
`checkDesignReview` first, so an invalid review is never written and a file
that can't be read is never written over (the Overview says why). The author
is the signed-in user (`useSession`, the token's sub and name); ids are
`crypto.randomUUID()`.

The hash (`artifactHashes`) is the lowercase hex SHA-256 of the file's UTF-8
text as the room holds it (`lib/sha256.ts`, plain JavaScript, copied from the
prototype kit, so it is the same over plain HTTP), and for a prototype the
kit's `prototypeHash(manifest, source)`.

## A file's title line

`ArtifactFrame`: the file's name and path, its revision once a design run
has changed it ("Revision 2", the spec state's `artifactRevisions`, by the
review path), then at the right **Checked**
(GitHub's Viewed: ticking records the current hash; a file that changed since
shows unticked with "Revision 2 · Changed since you checked it"; nothing
collapses) and
**💬 N**, a popover with the file's threads (`CommentThreads`) and a text box.
A draft shows **Pending**, with Edit and Delete for its author; a sent one is
with the agent; an addressed one carries the agent's one-line reply ("✓
Addressed: …") with **Reopen**, which puts it in the next batch. The thread
names no revision: the comment's `revision` counts differently from the
title line's, so only the title line numbers the file's revisions.
A comment the agent found ambiguous shows "Agent asks: …" and, once
answered, the answer.
A prototype comment names the screen and elements it was made on. Acceptance
tests have the 💬 but no Checked. There are no comments on spots inside a
file.

## Questions waiting on the user

What the design waits on the user for is stored where its answer goes, never
only said in the chat (`model/questions.ts`, `useDesignQuestions`):

- **A designed feature's open questions**: each entry of the feature file's
  Open Questions that the user has not deferred (the gap check writes one for
  a call only the user can make). Answering is an edit of the file
  (`answerOpenQuestion`, which the spec card's blocking box uses too): the
  entry leaves, the answer lands in Decisions. The feature's design is then
  out of date, and Update design is the next primary. The Overview's note
  says why: "Your answer changed Place an order: update the design" when
  every out-of-date feature was answered on this card, in this browser, and
  not designed since (`useAnsweredHere`); otherwise, and after a reload,
  "You changed Place an order in Spec since it was designed".
- **The agent's question on an ambiguous comment**: `question` on the
  review comment, which the agent leaves `sent`. The user's `answer` reopens
  it (`answerComment`), so Address comments sends it and the agent acts on it
  with the answer. An answer can be changed until it is sent.

While any waits, the top of the Overview says so, with **Answer N
questions**, which drops down a panel (`QuestionsPanel`): each question with
what it is about (the comment and its file, or the feature), the agent's
options as buttons for a feature's question, and a text box. An answered
question leaves the panel; the last one closes it.

## A product recreated from its code

A project seeded from an `.aespecs` bundle (ADR-0053; the status's
`spec.recreate`) has its requirements, data model and dependency definitions
before any design. Its first design is `/recreate`: while the project is
seeded and no design exists, the card's primary reads **Recreate** and
`useDesignTurns` sends that command instead of `/design F1 F2`. The run
shows as a design of every feature (`runOf`, `isDesignRun`), and from the
next run on the card sends `/design` as for any project. The create dialog's
prompt rides that first turn as the owner's note: the pod appends it to the
bare command as it appends the idea to `/start`, so the chat line reads
"Recreate the product from its specs: <note>". With a bundle attached the
prompt is optional and the project name is suggested from the bundle's name.

## The Overview

`DesignOverview`: For business review, For technical review (each row: the
Checked box, the name, its pending 💬 count, Open), Dependencies (each
definition, and what still blocks a build), Acceptance tests (one row per
feature). At the bottom **Address comments (N)** and **Build**, the only
place comments are sent from. Exactly one is the primary, first match wins
(`primaryAction`): Answer N questions while any waits on the user, Address
comments while any comment waits (a draft or a reopened one), Design while
the spec is ahead of the design (the "Update design" or "Design N features"
note at the top of the Overview, which an answered feature question brings
up), Build once every checkable file is checked, otherwise Open on the first
unchecked row. The card's header keeps only Build: the primary when Build
is, except on the Overview, whose own Build carries it there (one primary per
screen). Before anything is designed the card's body offers Design.

The note beside the two buttons (`overviewNote`) says what the primary asks
(comments to send, the design to update), then, while Build is held, why
(`buildHold`, from the build offer): "v2 is building — open it on Builds",
linking to that build; "Wait for the agent to finish" while a turn runs in
the project's chat; "Not ready to build yet — " with each held feature and
the picker's reasons; or "Nothing new to build". Only a Build that can start
reads "Everything is checked."

Any agent turn holds Build: a Design or Address comments turn writes the
design file by file, and a build started half way would freeze a design the
turn had not finished. The hold is in `useBuildAction`, which every Build
reads (the Overview's and the card header's, the Builds page header, Next
up's build item, the track's Build leg, and the picker's Start build), so
they hold and release together.

**Address comments** (`useDesignTurns`) marks every waiting comment `sent`
with `sentAt` in one write, then sends the bare `/address-comments`
(`ADDRESS_COMMENTS_COMMAND`) with the design-review scope; the room is
flushed before every turn, so the agent reads them. If the turn doesn't
start, the comments go back as they were. While it runs the card says so,
and a prototype it may rewrite shows its last revision until the run ends.

**After a run.** What a run's record decides reaches the spec state after
the run's end: the design agent ends the turn's stream, then its usage outbox
posts the record (with the run's `endRef` and `changedPaths`) to aep-api. So
when a turn completes the console reads the spec state again, backing off for
about 15 s, until `designedFrom` changes (`agent-chat/turnRecord.ts`), and
reads the build preflight again. A revision can move on an earlier read,
while the commit the basis is taken from is not readable yet; the build gate
reads the basis, so that read is not when it stops. Read only at the end, a
run read as not designed yet, or as leaving its own design out of date, and
the title line kept the old revision, until a reload.

## The chat

The chat reports and the card acts (D07 rule 2, L26). A design run's closing
ends with the next step on the card ("Next on the Design card: answer 1
question, then check the 3 changed files"), and a question shows in it only
as a count ("1 question for you on the Design card"). When a design run left
questions and the Design card is not open, the chat adds **Answer N
questions** under the run, which opens the card (`model/chatNote.ts`); on the
open card the card's primary is enough.

The **Comments** item lists every comment grouped by file, in the rail's
order, each group linking to its file. The spec's Next up, the build picker's
summary and the project track count the waiting comments from the same file
(`useWaitingComments`).

## The prototype

The prototype's item (`PrototypeEntry`) shows its status and **Review**,
which opens the full-screen review ([prototype-review.md](prototype-review.md))
unchanged except for where its comments go: each queued comment is the
reviewer's Pending draft on the prototype in `review.json`, with its screen,
flow, role, state and elements (`model/reviewDrafts.ts`), and the dock reads
"N pending · sent with Address comments on the Overview". There is no Send to
agent, no Make prototype and no Prototype tab: Design makes the prototype in
its own run.

## Acceptance tests

`AcceptanceList`: the feature file as written, rule bands with their
requirement (`@story-F2.5`), scenarios, and each scenario's Given/When/Then in
a closed `<details>`, which the browser's find still reaches. No search,
filters, tags, outcomes or source links; `AcceptanceView` stays the
Validation report's.

## Mock mode

The mock's design turn (`mocks/fixtures/designTurns.ts`) writes the design's
files into the room one by one, as the design agent does
(`mocks/fixtures/designFiles.ts`; for Acme a full design with the kit's sample
prototype, the Xero dependency, technical decisions and a review holding one
addressed comment), each through the agent's write gates. Its
`/address-comments` turn answers every sent comment with a reply, and revises
the sample prototype where a comment asks for a change it knows. A comment
that offers two readings ("… or …") and has no answer gets a question
instead, and is addressed once answered. Each run closes with what is next
on the card.
