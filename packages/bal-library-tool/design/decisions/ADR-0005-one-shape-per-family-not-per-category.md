# ADR-0005 — one schema shape per family, not one per Central category

**Status:** Accepted

## Context

Central's docs payload files a module's declarations under 32 keys. The reader read 19 of them and, for
the ones it did read, treated each as its own shape: `stringTypes` and `booleanTypes` as a name plus a
description, `arrayTypes` and `unionTypes` as a name plus members, `classes` and `objectTypes` as a bare
name, `serviceTypes` as a name plus methods, `listeners` as a name plus its `init` parameters.

Stage 1 of the fidelity fix plan was "read what the payload already carries". Measuring the payload
before writing any of it turned out to matter more than the reading:

- Across the nine recorded fixtures, every item of **fourteen** categories — `stringTypes`,
  `integerTypes`, `decimalTypes`, `booleanTypes`, `simpleNameReferenceTypes`, `arrayTypes`,
  `unionTypes`, `intersectionTypes`, `anyDataTypes`, `tupleTypes`, `functionTypes`,
  `typeDescriptorTypes`, and the always-empty `anyTypes`/`mapTypes`/`streamTypes`/`tableTypes`/
  `xmlTypes` — has the **same key set**: a name, a description, and every key a type node has.
- `classes`, `objectTypes`, `serviceTypes` and `listeners` likewise share one key set. A listener is an
  object plus `lifeCycleMethods`.
- `variables` and `configurables` share a third.

That is not a coincidence to exploit; it is what the payload IS. A Ballerina type alias is a name bound
to a type descriptor, and Central files it by the *shape of the right-hand side* while publishing the
same object either way.

## Decision

**Model the family, not the category.** `CentralDocs` gained three shapes — `AliasDecl`, `ObjectDecl`,
`VariableDecl` — and `MemberTypes` and `ServiceType` were deleted. Downstream, seventeen alias categories
collapse into one `TypeDef.Alias`, and `TypeDef.Union` was deleted with them.

An `AliasDecl` reads the declaration object AS a type node. That is not a trick: the alias's right-hand
side is published on the declaration itself, which is why `simpleNameReferenceTypes` had a resolved
`http:ClientError` sitting in it that the old reader discarded (SAP-01).

The five always-empty type categories are read with the same reader. The argument is that they are
siblings of the twelve that are populated, and that a wrong guess surfaces as a located
`schema-drift` failure naming the exact path — which is the whole point of having a schema — rather than
as a silently dropped declaration.

`types`, `resources` and `relatedModules` are deliberately NOT read. The first two are empty in every
fixture, so their item shape is unknown, and inventing one would put a guess inside the one file whose
job is to state what Central actually sends. `KeySpaceTest` snapshots the payload's object shapes, so the
first package to populate either shows up as a reviewable diff.

## Consequences

**The renderer has one alias form to get right instead of six half-forms.** `type TsDef string;` and
`type Cloneable (any & readonly)|xml|Cloneable[]|map<Cloneable>|table<map<Cloneable>>;` are the same code
path. That closed SLACK-01 (102 aliases that printed as `// Unknown type: X`), SLACK-02/SLACK-13 (the
categories nothing parsed), HTTP-01, KAFKA-01 and SAP-01 as one change rather than five.

**`// Unknown type:` still exists, and now means what it says.** It is what an alias renders as when the
descriptor could not be encoded — not what a whole category renders as because nobody wrote a reader for
it. One survives in the corpus: `RequestMessage`, which a patch injects because Central publishes it in
none of its 32 categories.

**A category the reader ignores is a decision with a name now**, recorded above rather than implied by
absence. That is the difference between "we do not read `resources`" and "nobody noticed `resources`".

**`variables` and `configurables` are parsed and not yet rendered.** They are a real gap and the register
records it as one; a configurable is the single declaration a deployer must set, and it appears in no
verb. The shape is ready for whichever stage takes it.

## The same argument, one layer down (added 2026-08-12)

The callable-surface work found the pattern repeated INSIDE an object rather than across categories.
Central publishes an object's method list four times: `methods`, `otherMethods`, `lifeCycleMethods`, and
`initMethod`. Measured across every object in the nine fixtures, the last three are all subsets of the
first — `otherMethods` on all 226 objects, `lifeCycleMethods` on all 4 listeners, and all 103
`initMethod`s byte-identical to the `init` entry in the same object's `methods`, with no exception.

So the reader takes `methods` and ignores the other three. Reading them all would have printed the
constructor twice and each lifecycle method three times; treating them as four sources would have needed
four merge rules, none of which the payload justifies. Same conclusion, same method of reaching it: count
first, then decide how many shapes there are.

The exception that proves the rule is worth keeping in view. Two keys on an object are NOT groupings of
another and had to be read: `fields`, and the qualifier flags. And two facts a caller needs are in NO key
at all — whether the object is a client and whether it is a service — so they are derived from the
grammar instead, which is a different move from collapsing a shape and is recorded where it happens.
