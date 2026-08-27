# ADR-0010 — where Central drops the signal, name the gap instead of guessing

Status: accepted · 2026-08-12

## Context

Three findings in the last two stages had the same shape: Central publishes almost enough to answer, the
missing piece is not recoverable, and the reader was filling it in.

- **HTTP-14** — every `distinct service object` type got a `service X on new Listener(…)` template. 5 of the
  10 the corpus produced do not compile: `ballerina/http`'s four interceptor types and `ballerina/graphql`'s
  `Interceptor` are service objects a listener does not accept. Measured in the published source, the three
  attachable ones write `*Service;` and the four interceptors write nothing — and measured in the payload,
  `ServiceContract` arrives carrying `description`, `isDistinct` and `name` and no fields, no methods and no
  `inclusionType`, byte-for-byte indistinguishable from `RequestInterceptor`.
- **SLACK-10** — 693 of slack's record fields carry an annotation. `@jsondata:Name` is 599 of them, its config
  record has a required `value` field, and the value appears nowhere in the 2.7MB payload.
- **`configurable`** — Central publishes 14 across the corpus and nothing rendered any. Compiled:
  `http:maxActiveConnections` from another module is `attempt to refer to non-accessible symbol`, because a
  `configurable` is module-private.

## Decision

**A declaration that does not compile is never printed. Where the fact is missing, the document says what is
missing and why, in the register that fits.**

- **HTTP-14** — a template is written only for the service type the listener's `attach` names, which is the one
  attachability signal the payload carries. The other types get one comment naming them, stating that `attach`
  takes one specific type and that the inclusion which would make them subtypes of it is not published. Their
  contract is not lost: each is already declared in full in the Types section.
- **The skeleton's other hole is named too.** Compiling the surviving templates turned up a second half nobody
  had filed: `graphql:Service` and `kafka:Service` are attachable and Central publishes no methods for either,
  while both listeners require one — `a GraphQL service must include at least one resource method with the
  accessor 'get'` and `Service must have remote method onConsumerRecord`. `http:Service` is the case where an
  empty body genuinely compiles. No payload key separates the three, so the body carries a comment saying the
  method contract is unpublished rather than presenting an empty block as complete.
- **`configurable`** — held in `Library.configurables`, apart from the declarations, and reported by `overview`
  as a `Config.toml` fragment. It is deliberately absent from the `api` document: a declaration in the code
  register is something to copy, and this is something to set. It must never gain the blanket `public` the
  other declarations carry.
- **SLACK-10** — not attempted, recorded with the compiler proof. The 15 `@constraint:*` fields are the only
  subset where a valueless marker would carry a fact, and reopening them needs a `// Special Agent Note:`
  clause rather than an annotation.

## Consequences

- The cost is explicit and small: two templates that WOULD have compiled (`http:ServiceContract` and
  `http:InterceptableService`) are withheld, because the payload cannot distinguish them from the four
  interceptors. Printing five that do not compile is the worse trade, and the note is what keeps the withheld
  two from being a silent omission.
- The complement of this rule already ran the other way, and both directions matter. `Library.variables` — 64
  module-level `public final` declarations, 61 of them http's status constants — WERE renderable and were being
  dropped, so they are now printed with their initialiser. That initialiser is not decoration:
  `public final T X;` is `uninitialized variable 'X'`, and all 64 are written `= {}` in their published sources,
  so the rendered line is the source's own rather than a guess that happens to compile. Compiled: a reference to
  all 61 http variables as rendered builds clean.
- The rule generalises the one PSQL-05 established in stage 6a — "fixed by removing a claim, not by making a
  better one" — from a summary row to the code register.
- What it forbids is the tempting version of each of these: a name-suffix heuristic for "is this an
  interceptor", a subtype guess from the presence of methods, a bare `@jsondata:Name` that does not compile, a
  `configurable` printed as a declaration. Each would be right about most cases and wrong about the reason, and
  a reader cannot tell which case they have.
