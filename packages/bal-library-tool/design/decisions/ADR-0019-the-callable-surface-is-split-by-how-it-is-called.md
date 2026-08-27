# ADR-0019 — The callable surface is split by how it is called, and a wrong guess costs a line

Status: accepted · 2026-08-17

Supersedes the `ops` verb, whose own ADR is deleted with it; the measurements that decided it are the
appendix at the bottom of this one.

## Context

`ops` addressed the whole callable surface through one verb with two addressing modes — by path, or by
name in the same slot. It worked, and the 2026-08-16 sweep scored 17/17 green with it. What it did not do was tell a caller
**how to call** what it found, and that is the fact the tool exists to supply: both signature errors in
the 2026-08-15 sweep came from `->` versus `.` being absent, and a database client declaring
`remote function close()` is a real case where `dbClient.close()` does not compile.

Three further facts shaped the split rather than merely permitting it:

- **Central publishes no `isClient` key at all**, and `isService` is present but false on every object
  in the recorded corpus — including the seven `ballerina/http` service types whose own source says
  `distinct service object`. The role is already DERIVED from the grammar by `FromCentral.roleOf`: an
  object with a `remote` method is a client, a `serviceTypes` entry is a service.
- **Central's `clients` array is not the callable surface.** For `ballerina/sql` it is EMPTY while
  `Client` and `SchemaClient` — both reached with `->` — are filed among the ordinary declarations with
  their 122 methods. `ballerina/http` is the same shape in this corpus: ten clients, of which
  `ClientObject` and `StatusCodeClientObject` are filed as declarations.
- **One verb over three kinds could not name its own scope.** `ops <pkg> <constant>` failed with a
  client-ambiguity error for a name the package declares plainly, because the verb had no vocabulary for
  "that is not a callable".

## Decision

**Three verbs, split by how a symbol is called, over a partition derived from the grammar.**

| Verb | Addresses | Called with |
|---|---|---|
| `client` | every object whose derived role is CLIENT, from EITHER Central bucket | `->` |
| `class` | every object whose derived role is PLAIN or SERVICE | `.` |
| `funcs` | functions at module scope | `.` |

`symbols/Surface` is the one place that partition is computed, and `SurfaceTest` asserts it is
**exhaustive and disjoint** in both directions: an object in neither verb is unreachable, and one in
both is a document that says two different things about one name.

**A wrong guess costs a printed line, not a round trip.** This is what makes kind-specific verbs safe
for an agent, and without it the split would be strictly worse than one verb:

- A symbol of another kind still resolves and renders, prepending one line that names the canonical
  verb. `client <pkg> Cookie` shows the class; `client <pkg> ClientConfiguration` shows the record.
- A name that is a MEMBER rather than a container resolves and names its owner. On several owners the
  answer is a roster with counts and the command that opens each — never a bare validation failure,
  which was the sweep's most-hit ergonomic bug and whose suggestion rebuilt the command *without* the
  argument that had failed, so following it looped.
- The note is written in the register of the document it lands in: a facts row in a report, a `//`
  comment in the code register.

**The selector grammar is a property of the resolved CONTAINER, not of the verb.** A container that
declares resource functions reads `get`/`post`/`delete` as an accessor and the token after it as a path;
one that does not reads the same token as a member name. This corrects `ops`'s claim that HTTP-verb
parsing could be confined to one verb: in Ballerina a client IS a class, so `ballerina/http:Client` is a
legal argument to both `client` and `class` and declares seven resource functions either way. The
consequence for the implementation is that the container is resolved **before** the remaining positionals
are parsed.

**An exact name never loses to a substring.** Resolution runs exact-container, then exact-member-in-scope,
then another scope, then substring-member. The order is load-bearing rather than tidy: `Cookie` is a class
and also a substring of `getCookieStore`, which two of `ballerina/http`'s ten clients declare, so a fuzzy
pass placed earlier answers `client ballerina/http Cookie` with a roster of two unrelated clients instead
of routing to the class the caller plainly named.

## Consequences

- `ops` is gone with no alias. It cannot be aliased correctly, because it splits three ways.
- A constructor is part of its container and is reachable, which `ops` could not do at all.
- `client <pkg>` on a package with several clients is a ROSTER at exit 0, where `ops` failed at exit 1
  with "cannot pick one" — an answer an agent had to recover from rather than read.
- The call form is printed on every section heading, because it is derived and load-bearing.
- Three verbs share one implementation (`views/Containers`). Three copies would be three places for the
  same rule to rot separately, and the snapshots pin all three per fixture for that reason.

## Appendix — the spelling tolerance, completed 2026-08-18

The 2026-08-17 eval round produced three misses where a caller typed something reasonable and the tool
said "nothing matched" on a container with hundreds of members. All three were the same defect wearing
different clothes: **the tolerance existed in the path walk and not everywhere a path can arrive.**

| typed | resolved before | why |
|---|---|---|
| `repos/[owner]/[repo]/issues` | no | the declaration pattern required a type before the name — and on `[owner]` it did not decline, it backtracked and read the name as `r` |
| `get [PathParamType ...path]` as ONE argument | no | a one-token selector was compared against an entry's label, which carries the display spelling, and never reached the walk |
| `-s "repos/{owner}/{repo}/issues"` | no | a resource function's searchable text joins segments with spaces, and the query was split on whitespace only, so a `/` could never be a substring |

Fixed by making the tolerance uniform rather than by adding three special cases:

1. `declaredName` takes the **last word inside the brackets**, ellipsis stripped, so the type is optional.
   `[string owner]`, `[owner]`, `[...path]` and `[PathParamType ...path]` all yield the parameter's name.
2. A single selector whose first word is an accessor the container declares is split there, so the
   accessor-plus-path form reaches the walk in one argument as it always did in two. Safe because a
   member name cannot contain whitespace, and gated on the accessor being real, or `Producer send` would
   parse `Producer` as one.
3. `-s` splits its query on `/` as well as on whitespace, and normalises through `readableSelector`.

**What `-s` deliberately did NOT become.** It is still an unordered, unanchored AND over tokens — the
same meaning it always had for whitespace-separated words, now reaching segments. Anchoring is the
positional selector's property and the reason it walks a tree instead of filtering strings; a caller who
needs "this path, not one that merely shares its segments" wants that slot. The flag's description now
says so, because it previously claimed path matching it did not do, which is the version of this bug that
survived longest.

Verified against the 27-command session path: every existing match set is byte-identical apart from the
ADR-0023 length stamp.


## Appendix — what the superseded `ops` decision leaves behind

`ops` addressed the whole callable surface through one verb with two addressing modes, by path or by
name, and the 2026-08-16 sweep scored 17/17 green with it. Its ADR is not kept — the verb never shipped
outside that fortnight — but its measurements are not reversed by the split, and they are what the three
verbs are held to:

- **Both halves of a client are answered.** `ballerina/http`'s `Client` still prints its 7 resource
  functions and its 19 named ones, split by call form, under one command.
- **Addressing is derived from what the package declares**, never asked for as a flag. What used to be
  "two modes chosen by what the client declares" is now "the selector grammar is a property of the
  resolved container" — which is the same rule, extended to cover the case `ops` got wrong: it
  claimed HTTP-verb parsing could be confined to one verb, and in Ballerina a client IS a class, so
  `ballerina/http:Client` is a legal argument to both.
- **No camelCase hierarchy is invented.** The `ballerinax/redis` measurement — `z` 20, `s` 14, `h` 14,
  `l` 10, single letters cut off `zAdd`/`hGet`/`lPush` — still stands, and it is now enforced rather
  than only recorded: the grouped tier requires a prefix of at least three characters, which is exactly
  the guard that measurement demands.
- **A wildcard discloses the branches it did not take**, because `--all` (which replaced `--sigs`)
  promises completeness.
- **A roster row never dead-ends.** `overview` used to offer `ops <pkg> <path>` unconditionally, so on
  `ballerinax/aws.s3` it returned "Resource functions | none in any client" and pointed back at
  `overview` — a two-call loop with zero information, from a document that had already printed
  `Remote functions — 19`. Every row now ends in the command that opens it, and `PointersTest` RUNS
  every command every document prints.
