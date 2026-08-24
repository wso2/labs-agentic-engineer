# ADR-0016 — `ops` addresses the whole callable surface, by path or by name

Status: accepted · 2026-08-15

## Context

`ops` was named for a client's operations and implemented a client's **resource** functions:
`PathTree.operationsOf` keeps `Fn.Resource` and drops everything else. A remote or normal function was
reachable from no verb at all — `type` reads declarations and a function is not one, `ops` wanted a path it
does not have, and `api` is the whole package.

Three shapes of damage, all measured on the 2026-08-15 eval sweep and reproduced against the live tool:

- **The large connector had no door but the largest one.** `ballerinax/twilio` is 199 remote functions and
  **62,063 characters** of `overview`, with no `--client` to pick (there is one client), no path to address and
  no cap — `overview --help`'s ">100 functions is replaced by its path tree" counts *resource* functions. The
  eval agent piped `head -150`, bought two usable lines, lost `createMessage` at line 281, and paid for the
  same document a second time through a temp file.
- **A client declaring both was answered with half of it.** `ballerina/http`'s `Client` declares 7 resource
  functions and 19 remote/normal ones. `ops --client Client` printed the 7 under a fact row reading `(7 of 7)`
  and said nothing about `execute`, `forward`, `submit`, `getResponse`, the promise set or the four
  circuit-breaker controls.
- **The pointer led nowhere and the tool knew it.** `overview`'s `## Next` offered
  `bal library ops <pkg> <path>` unconditionally. Followed on `ballerinax/aws.s3`, it returned
  "Resource functions | none in any client" and a `## Next` pointing back at `overview` — a two-call loop with
  zero information, from a document that had already printed `Remote functions — 19` two screens earlier.

## Decision

**One verb, two addressing modes, chosen by what the client declares rather than by a flag.** A resource
function has a path and is addressed by one; a remote or normal function has only a name and is addressed by a
name filter in the same positional slot. `<path>` became `<path|name>`.

**A client that declares both is answered with both** — the path half navigated, the name half listed under
it, split into `Remote`/`Normal` because the call form differs (`->` against `.`) and that is the fact a caller
came for. Under `--sigs` both halves expand, and the offering bullet sizes both: a dump whose contract is
completeness may not silently omit 19 of 26 functions.

**A single token that is not a path but does name part of the other half is a NAME, not a miss.** Without this
the recovery is a loop: the caller is told `execute` is a remote function, runs the command that says so, and
lands back in path resolution because the client also declares resource functions.

**No hierarchy is invented for names.** Splitting camelCase into segments would let one `PathTree` serve
everything, and it reads beautifully on the package that suggests it — twilio's 199 names split into five CRUD
verbs (`list` 61, `fetch` 43, `create` 35, `delete` 33, `update` 27). Measured on `ballerinax/redis` the same
splitter yields `z` 20, `s` 14, `h` 14, `l` 10: single letters cut off `zAdd`/`hGet`/`lPush`, structure in the
tokenizer rather than in the domain. Nothing in the payload separates the two cases, so per ADR-0010 a flat
surface gets a flat index and a name filter.

**A bare token widens to a substring and the document says so** — `| Filter | `message` → read as `*message*`
(9 of 199) |`. A caller who already has the exact name has no reason to run this verb. `*` keeps the meaning it
has on the path side.

**The ambiguity rule stays asymmetric, deliberately.** Two clients that both declare resource functions is
still a `Validation` failure naming them, because `ops` would otherwise pick one silently. Two clients that
declare only remote functions is not: the roster is a one-screen document that now ends each row in the command
that opens it, which beats an exit an agent has to recover from. `ballerinax/kafka`'s three clients kept their
document and gained the descent.

**`overview`'s `ops` pointer is derived, not asserted** — `<path>` where the package has one, `<name>` where it
does not. The document already holds the fact. **`search`'s pointer prints `<path|name>`** instead, because it
has a registry row rather than a loaded package and genuinely cannot know — the neutral grammar is the honest
answer, and it is the grammar the synopsis prints. That one was missed on the first pass and caught by a
measured run: three attempts against `ballerinax/twilio` read `ops ballerinax/twilio <path>` out of the search
document that opens the case, for a package with no path at all.

## Consequences

- `ballerina/uuid`'s 16 module-level functions are addressable for the first time. With no clients there was no
  verb at all: `type ballerina/uuid createRandomUuid` answers `symbol-not-found` with two unrelated candidates
  for the first name in that package's own overview. A package with no clients now answers through the same
  flat index, rendered with `renderStandaloneFunction` because a module function carries `public` and a member
  does not.
- The cost of one question against twilio goes from 62,063 characters to **9,012** for the whole index, or
  **816** for `ops ballerinax/twilio message`, or **2,103 bytes** for `'message' --sigs` with signatures. That
  is the pipe the skill forbids becoming unnecessary rather than merely discouraged.
- Six of the nine corpus fixtures moved their `ops` snapshot, and every move is a section that was missing:
  postgresql (`query`, `queryRow`, `execute`, `batchExecute`), graphql (`executeWithType`, `execute`),
  googleapis.sheets (43), sap (7 remote beneath its 7 resource), http and kafka. `github` and
  `googleapis.gmail` did not move — they declare no standalone functions, which is the check that the path side
  is untouched.
- `ViewsTest.namingAClientDoesNotDiscardTheDiagnosisThatMakesAClientLessOpsUseful` became
  `namingAClientAnswersWithItsOwnCallableSurface`. EMAIL-02's fix pointed the caller at a roster naming the
  client and its counts; this prints the functions, which is what the roster was a directions-sign to. The
  regression it guards — `--client` on a remote-only client must not answer "Nothing is callable here" — is
  asserted as before.
- **The `--sigs` byte count on a mixed client now includes both halves.** It sized only the path half, which is
  how a bullet becomes a number nobody can trust.

---

## Superseded by ADR-0019 (2026-08-17)

`ops` no longer exists. The callable surface is addressed by three verbs — `client`, `class`, `funcs` —
split by how a symbol is CALLED rather than by which array Central filed it in (ADR-0019).

**The decision this ADR recorded is reversed. Its findings are not, and they survive inside the new
verbs:**

- **Both halves of a client are answered.** `ballerina/http`'s `Client` still prints its 7 resource
  functions and its 19 named ones, split by call form, under one command.
- **Addressing is derived from what the package declares**, never asked for as a flag. What used to be
  "two modes chosen by what the client declares" is now "the selector grammar is a property of the
  resolved container" — which is the same rule, extended to cover the case this ADR got wrong: it
  claimed HTTP-verb parsing could be confined to one verb, and in Ballerina a client IS a class, so
  `ballerina/http:Client` is a legal argument to both.
- **No camelCase hierarchy is invented.** The `ballerinax/redis` measurement — `z` 20, `s` 14, `h` 14,
  `l` 10, single letters cut off `zAdd`/`hGet`/`lPush` — still stands, and it is now enforced rather
  than only recorded: the grouped tier requires a prefix of at least three characters, which is exactly
  the guard that measurement demands.
- **A wildcard discloses the branches it did not take**, because `--all` (which replaced `--sigs`)
  promises completeness.
- **A roster row never dead-ends.** Every one ends in the command that opens it, and `PointersTest` now
  RUNS every command every document prints, so the shape of bug this ADR's third finding described
  cannot be reintroduced.
