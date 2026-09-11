# ADR-0028 — A dependency is a directory in the rail, its definition a view

## Context

An external dependency the design could not settle had one route forward in
the console: "Resolve via chat" in the Build drawer, one full agent round trip
per dependency, with no place to hand over a document and nothing to accept
an assumption on. The drawer was also where the only upload form lived, in
front of the Build button, at the worst moment.

Platform-side, the dependency now has one definition in its own directory
(repo [ADR-0027](../../../../docs/decisions/ADR-0027-one-external-dependency-one-definition.md)),
beside the interface it exposes and, for an SDK, its manifest — so it has
files to show, the way a component does.

## Decision

**Every external dependency is a group in the rail, shaped like a
component's.** The group sits between Flows and the components, with the plug
glyph telling it apart; its header carries the one thing the user must do
(*Choose a provider*, *Needs a contract*, *Needs your acceptance*) as an amber
mark with the words on hover, or the qualifier on a
resolved one (*Assumed*, *Derived from docs*, *Registered*, *SDK only*) as
quiet text. Its rows are
the directory's files, named for what they are under the dependency's own
header — **Definition** (`dependency.json`), **API** (`openapi.yaml` or
`schema.graphql`), **SDK** (`sdk.json`) — the definition first.

**Every view is a file's.** The rail's selection model stays what it was: a
row selects a path, and the pane picks the renderer by path. The definition
renders through `DependencyView` the way a component's `design.json` renders
through `DesignView` — reading the live doc ahead of the commit and the
committed copy otherwise — with the read model adding what a file cannot
know: status, flags, who uses it. The interface and the manifest open in the
existing file viewers. There is no dependency-shaped selection and no page
over the read model.

**The definition view is the component view's shape.** Eyebrow chips (kind,
qualifiers, the todo or *Resolved*), the name as the heading with **Resolve**
or **Reconsider** beside it, then labelled facts — *Provider*, *Style*,
*Source*, *Package* — so the provider reads as the provider and never as the
name said twice. Sections follow: Description, Used by, Provider, then
Interface once a provider is chosen, and Configuration once the keys exist.
The dependency is the *service* the product needs (`currency-service`); the
*provider* is who supplies it.

**The definition owns everything that moves a dependency forward.**

- **Select a provider**, beside the Provider heading while the definition
  names none, runs the resolve flow — the agent never chooses one (repo
  ADR-0027). The choice happens in the flow's own question cards, rendered on
  the spec view around the definition: the first card offers the design
  agent's `suggestions` as options, *Another provider* as free text (a name
  or a document URL) and *Find one for me*; when no published interface
  exists, a second card offers *Give a link*, *Upload one* and *Proceed on
  your assumption*. The header shows no Resolve while none is chosen, so the
  way in is in one place.
- **Typed option actions.** A question option may carry an `action` the
  console runs when the answer is submitted, before it reaches the agent:
  `accept-assumption` records the user's authorization on the definition
  (the acceptance endpoint, which no longer waits for an interface on disk)
  and refreshes the room's copy so the agent's next snapshot carries the
  record; `upload-interface` opens the interface modal over the card and
  sends the answer once the document lands. So *Proceed on your assumption*
  is the whole consent — no acceptance box follows. The box stays only as the
  fallback for an assumed interface nobody authorized from the card.
- **A link in the chat opens a document.** The design turn's closing list
  links each open dependency's definition as `aep://spec/<path>`; the chat's
  markdown renderer turns it into a click that opens the spec view on that
  file (`?file=`, stripped once followed, like `generate`). Nothing more
  happens on the click: the user presses **Select a provider** themselves.
- **Resolve** runs the guided flow once a service is chosen — the message is
  the skill command, `/resolve-dependency <name>`, nothing else, because the
  agent reads the definition from its file and the playbook from its skills.
  A resolved dependency offers **Reconsider** instead, which stays prose: it
  opens a conversation about a choice already made.
- **Provide interface** sits beside the Interface heading and opens a modal:
  a URL the platform fetches, or a dropped file, straight into the
  dependency's directory. A modal rather than a form in the document, because
  the document is what the dependency *is* and the upload is an act on it.
  Once a document is on file the section links to it in place, with its
  provenance, and the button reads **Replace interface**.
- **Accept the assumption** appears inline, only for an interface the agent
  wrote, and is the only way the `assumed` record gets written.

**The Build drawer lists; it does not resolve.** — *superseded by
[ADR-0029](ADR-0029-build-asks-in-a-dialog-that-lists.md).* The rule holds;
the container does not. Build now opens a **Resolve dependencies** dialog —
one row per blocking dependency, names only, and one button, **Resolve**,
which runs the flow over every open dependency. There is no per-row **Open**
(the rail reaches each definition), and the paste-a-spec form is gone.

**One state per dependency.** The dependencies read model is per component;
the definition is one file, so the console folds the rows by name
(`dependencyStates.ts`) and the rail, the view and the resolve dialog read one
answer.

## Consequences

- "Resolve via chat" is gone from the lexicon; the
  design view's dependency cards keep their chat button, now sending the
  skill command.
- `SpecSelection` is unchanged; `followSelection` sends a `dependency.json`
  write to the file, and the pane renders it as the definition.
- *Interface* is the user's word in the rail, the section, the button and the
  modal; `contract` remains the definition's field naming the file.
- Two endpoints back the view: `POST …/dependencies/{name}/contract` and
  `POST …/dependencies/{name}/assumption`.
