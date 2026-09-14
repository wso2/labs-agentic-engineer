# ADR-0029 — Build asks in a dialog, and the dialog lists

## Context

One Build click led to two different containers. A project whose design still
held an unsettled dependency got a right-hand drawer
([ADR-0028](ADR-0028-a-dependency-is-a-directory-in-the-rail.md)); every other
project got a centred modal — the cut-version ceremony. Which one appeared
depended on state the user could not see before clicking, so the same button
taught two surfaces.

The console has one other drawer (the marketplace catalog) and seventeen
dialogs. Nothing in `design-system.md` said which to reach for.

## Decision

**A decision is a dialog.** A dialog blocks and asks to be answered now; a
drawer is an inspector you keep open beside your work. Build cuts a git tag
that cannot be un-cut, so both of its surfaces are dialogs. `Drawer` stays for
browsing — the catalog — and is not the container for an act.

**One dialog, one job.** The Build click opens exactly one of two:

- **Resolve dependencies**, when a dependency has no provider, no interface on
  file, or names an org-service this project cannot see. It carries the names
  and one action, **Resolve**, which runs the guided flow over all of them.
- **Start build**, otherwise. It carries the version name and what this
  version changes, and its action builds.

The blocking case is not a group inside the build dialog. Mixing *you must
act* rows among *this is what changed* rows gives one list two grammars, puts
buttons on some rows only, and leaves the primary action disabled for a reason
sitting halfway down a scroll.

**A dialog lists; it never resolves.** Neither dialog holds the
provider-picking or the interface upload — those live on the dependency's
definition, which is the document the dependency *is* (ADR-0028). The user
leaves the dialog, resolves, and presses Build again. Two Build presses is the
accepted cost of keeping one way to do the work.

**One row, one name.** A row in either dialog is a name and at most a chip. It
carries no per-row button: a second button in a dialog is a second thing to
learn, and the rail already reaches every definition.

**A name alone does not say what a name is, so the change list is GROUPED** —
**Components**, **External dependencies**, **Platform resources**, in that
order, headings in bold, empty groups unrendered, and the whole list on its own
surface so the frame that scrolls is visible as a frame. `ceramics-db` and
`currency-service` read identically as bare names, and one is a database the
build is about to stand up while the other needs a provider and its keys from
the user. The two dependency kinds are never one group for exactly that reason.
The heading is all the dialog says about it: a line of explanation under each
(*you choose the provider and supply its keys*) was tried and read as clutter
over a list this short.

**Every row names something that EXISTS once the version is built.** That is
what keeps the requirements out of the list: they are the input to the version,
not a thing it makes, and they move on nearly every version — so the row sat
among component names as though it were one, saying nothing. What changed in
the requirements is read where the requirements are.

## Consequences

- `BuildDependencyDrawer` is deleted. The paragraph *"The Build drawer lists;
  it does not resolve"* in ADR-0028 is superseded by this ADR — the same rule,
  in a dialog, and without the per-row **Open**.
- The Build click no longer branches on container, only on content: one
  preflight answer picks which dialog opens.
- The lexicon's Build-drawer entry is replaced by the two dialogs' copy.
- A future surface that must be answered before work continues is a dialog. A
  future surface for browsing may be a drawer, and says why.
