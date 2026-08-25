# ADR-0008 — the declaration registers document, the compact views quote

Status: accepted · 2026-08-12

## Context

Central publishes a description for 2,755 parameters and 1,532 return parameters across the nine fixtures.
`Signatures.renderStandaloneFunction` had always turned those into Ballerina's `# + name - description` doc
rows; `renderMemberFunction` — every client method, class method, listener method and service contract
method — rendered none of them. So the same fact appeared in one section of a document and not in another,
for no reason anybody had written down.

Wiring the rows into `renderMemberFunction` is the obvious repair and it has a cost that is not obvious.
`renderMemberFunction` is shared: `api` and `type` print declarations with it, and the compact views
QUOTE it, which is what makes their agreement structural rather than merely tested.
`Overview.resourceSection` lists a client's resource functions inline only while they fit under
`MAX_INLINE_SIGNATURE_BYTES` (20,000) and `MAX_INLINE_OPERATIONS` (100); past either it prints a path
summary instead. `ballerinax/googleapis.gmail` sits inside that budget today at 32 operations and 15.3KB,
and it publishes 397 parameter descriptions. Adding the rows to the quoted form pushes it past the budget,
so the change meant to enrich the view would have replaced 32 real signatures with a tree.

## Decision

The DECLARATION is one code path and one set of bytes. What differs is the documentation around it, named
as `Signatures.Detail`:

- **`Detail.FULL`** — the description, then a `# +` row per documented parameter and one for the return.
  Used by `TypeDefs.renderMembers`, which is the body of every object, client and service template the
  `api` document and the `type` verb print.
- **`Detail.SIGNATURE`** — the description and the declaration. Used by `Signatures.renderSignature`, which
  is what the container verbs quote (`views/Containers`; `overview` generates no signature at all since
  ADR-0017).

`renderStandaloneFunction` stays separate and keeps its rows, because a module-level function's declaration
differs from a member's in a way a mode cannot express: it carries `public`, and no member does.

## Consequences

- The api document gains 4,545 doc lines across the corpus and the views gain none but the constructor doc
  comments they were owed (SAP-05, 36 lines). `ViewsAgreeTest`'s oracle — every line a view prints is in the
  api snapshot verbatim — holds in the direction that matters, since the views now print a subset.
- This is not a new principle, it is an existing one made explicit. `Overview.clientSection` already prints
  a client's description as `description.split("\n")[0]` — its first line only. A view that answers inside a
  budget abbreviates prose; a register that lists declarations does not.
- The measured shape of what the rows carry is worth recording, because it is the argument someone will want
  to reopen: 282 distinct strings cover all 2,755 parameter descriptions, `ballerinax/github` draws its 1,168
  from five of them, and 789 of the return descriptions are the word "Response". The reason to print them
  anyway is that WHICH parameter carries which is not inferable, and the package's own source states it on
  every one. If that trade is ever revisited, revisit it with these numbers rather than by re-measuring.
- A third detail level is the obvious next request ("rows, but only where the description is not the
  parameter's name again"). Resist it without a rule that can be stated: "the description restates the name"
  is true of 2,089 of the 2,755 by one normalisation and a different number by any other, which is a
  judgement dressed as a threshold.

---

## Amended by the 2026-08-17 interface change

**The rule is unchanged and strengthened. What changed is what the register is keyed on.**

It was keyed on the VERB: `type` and `api` were code, everything else was a report. It is now keyed on
the DOCUMENT, because `-r` can be reached through `client`, `class` or `funcs` and its response is
nothing but declarations — so it is pasteable whole, and a Markdown table in the middle of it would
break a caller redirecting into a `.bal` file. `RegisterTest` states it as: *`type`, `api`, and any
`-r` response are code; everything else is a report.*

The quote-don't-re-spell half needed no amendment, and it earned its keep during the change. A revision
of the redesign proposed a space-aligned summary form for compact views; against real
`ballerinax/github` output that sample carried **four compile errors in one snippet** — `'key` lost the
apostrophe that a Ballerina keyword requires, `ActionsCacheListActionsCaches` was shortened to a name
the package does not declare, an included-record parameter became two invented ones, and
`isolated resource function` was dropped along with the `->` call form it implies. None of those is
sloppiness in one sample; they are what happens structurally when a renderer may invent a shorter
spelling, because it must pick one and no test can catch a spelling nothing else in the tool produces.

`Signatures.Detail` therefore still has exactly two levels, and the tier ladder the container verbs use
selects **how many** declarations to quote and how much prose surrounds them — never how to spell one.
