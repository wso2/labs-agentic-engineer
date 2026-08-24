# ADR-0002 — Verb-first, no implicit default verb, per-verb flags enforced by declaration

**Status:** Accepted — the exit codes below are superseded by ADR-0015

The grammar decisions here stand. Every **exit 2** in this document is now **exit 1**, and the usage
text moved from stderr to stdout at exit 0: ADR-0015 collapsed the failure codes to one and made the
`kind` field the discriminator. What that costs the decisions here is nothing — the loudness a
leading verb buys is `kind: validation` rather than a distinct code.

## Context

The reader this tool was ported from is a standalone binary whose grammar was
`bal-library [verb] <org/name> ...`, with one convenience: a first positional containing `/` was taken as
a package and the verb defaulted to `overview`. It also hand-built its per-verb flag rules — a
`VERB_FLAGS` table, a `verbsTaking` lookup, a `rejectForeignFlags` check and a `describeParseError`
mapper, roughly sixty lines whose whole job was "a flag a verb does not take is exit 2, not a silently
dropped argument".

Under `bal` the same grammar has a different context: `bal` owns the first token, and a tool is reached as
`bal library <rest>`.

## Decision

### Verb-first stays

It is standard practice (`git`, `docker`, `kubectl`), and there is a specific reason beyond convention: a
verb has no `/`, so a **stale** binary fails it against the qualified-name pattern at **exit 2**, which is
loud and means "fix your arguments". A verb placed *after* the package is the unsafe form — a stale binary
reads it as a version and reports `package-not-found` at **exit 1**, which the skill driving this tool
teaches means "retry". An agent then retries a command that can never succeed.

`CliTest.aVerbLeadsBecauseAVerbAfterThePackageWouldReadAsAVersion` pins exactly that: `overview
ballerinax/github ops` resolves `ops` into the version slot and comes back as
`package-not-found: ballerinax/github:ops` at exit 1.

### The implicit default verb is dropped

`bal library ballerinax/github` is now exit 2 rather than an overview.

Under `bal`, a bare first token reads as a **subcommand**, so a package name there is indistinguishable
from a typo — and `bal library overview ballerinax/github` costs one token to be unambiguous. What is kept
is the *error*: the failure names the five verbs, and when the token contains a `/` the suggestion hands
back the exact command rather than making the caller read a usage block.

```
$ bal library ballerinax/github; echo $?
{"kind":"validation","message":"'ballerinax/github' is not a verb.","suggestion":"A verb comes first:
try `bal library overview ballerinax/github`. The verbs are search, overview, ops, type, api."}
2
```

This is the one place the port deliberately changed observable behaviour, and it moved four lines across
the report snapshots (`bal-library <pkg>` → `bal library overview <pkg>`).

### Per-verb flags are enforced by where the field is declared

`--sigs` is declared on `Commands.Ops` and nowhere else, so `overview --sigs` is an unmatched argument
before any of our code runs. The sixty hand-maintained lines are gone; what remains is a three-entry
`FLAG_OWNERS` table used **only for the message** — without it the failure would read "Unknown option
'--deps'", which is true and leaves the caller to guess which verb wanted it.

This is the one place the port is deliberately *better* than its source rather than faithful to it.

### picocli parses; we own the help and the errors

Two things about the usage text are contractual and a generator owns neither. It goes to **stderr** with
exit **2**, because stdout is the document and nothing else. And it is the one place the cache is allowed
to speak, which is how an operator proves the cache is alive inside a runner image without parsing
anything.

So picocli's own error printing is never used — stderr has to hold exactly one `Failure` object, and
picocli would write a usage block beside it. `Usage` is hand-written and golden-filed.

## Consequences

**`--help` needed a declaration to reach us.** This was found by the live protocol, not by the test suite:
`bal`'s launcher rejects an undeclared `--help` with `ballerina: unknown option: '--help'` at exit 1 —
its error, and the wrong code for a usage request. `LibraryTool` therefore declares a hidden
`--help`/`-h` and puts it back on the argument list, so `Cli` remains the only thing that decides what
help means for which verb. Everything else passes through untouched.

**Exit 0 does not call `System.exit`.** `bal` exits 0 on its own, and short-circuiting the launcher on the
success path would skip whatever it does after a tool returns. Only a non-zero code exits.

**A flag-shaped option value is rejected by hand.** picocli 4.0.1 consumes the next token as an option
value even when it looks like a flag, and the setting that changes that arrived in 4.4. So `--client
--sigs` is checked for explicitly: six lines rather than a dependency the distribution does not ship.
