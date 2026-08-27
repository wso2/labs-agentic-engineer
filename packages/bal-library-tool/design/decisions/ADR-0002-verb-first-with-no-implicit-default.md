# ADR-0002 — Verb-first, no implicit default verb, per-verb flags enforced by declaration

**Status:** Accepted. The grammar decisions stand; the exit codes they were first written against do not
— ADR-0015 collapsed every failure to exit 1 and made the JSON `kind` the discriminator, and a usage
request moved to stdout at exit 0. The text below is in those terms.

## Context

The reader this tool was ported from is a standalone binary whose grammar was
`bal-library [verb] <org/name> ...`, with one convenience: a first positional containing `/` was taken as
a package and the verb defaulted to `overview`. It also hand-built its per-verb flag rules — a
`VERB_FLAGS` table, a `verbsTaking` lookup, a `rejectForeignFlags` check and a `describeParseError`
mapper, roughly sixty lines whose whole job was "a flag a verb does not take is a failure, not a silently
dropped argument".

Under `bal` the same grammar has a different context: `bal` owns the first token, and a tool is reached as
`bal library <rest>`.

## Decision

### Verb-first stays

It is standard practice (`git`, `docker`, `kubectl`), and there is a specific reason beyond convention. A
verb has no `/`, so a stale binary fails it against the qualified-name pattern as `kind: validation`,
which means "fix your arguments". A verb placed *after* the package used to be the unsafe form: a stale
binary read it as a version and reported `package-not-found`, which the skill driving this tool teaches
means "retry", so an agent retried a command that could never succeed.
`CliTest.aVerbLeadsAndNoVerbTakesAPositionalItHasNoMeaningFor` pins the rule and records that the hazard
itself is gone with the version slot (ADR-0021): `overview` takes exactly one positional, so a second is
rejected before any request.

### The implicit default verb is dropped

`bal library ballerinax/github` is a validation failure rather than an overview.

Under `bal`, a bare first token reads as a **subcommand**, so a package name there is indistinguishable
from a typo — and `bal library overview ballerinax/github` costs one token to be unambiguous. What is kept
is the *error*: the failure names every verb, and when the token contains a `/` the suggestion hands back
the exact command rather than making the caller read a usage block.

```
$ bal library ballerinax/github; echo $?
{"kind":"validation","message":"'ballerinax/github' is not a verb.","suggestion":"A verb comes first:
try `bal library overview ballerinax/github`. The verbs are find, overview, client, class, funcs, type,
guide, api."}
1
```

This is the one place the port deliberately changed observable behaviour, and it moved four lines across
the report snapshots (`bal-library <pkg>` → `bal library overview <pkg>`).

### Per-verb flags are enforced by where the field is declared

`--module` is declared on `Commands.Guide` and nowhere else, so `overview --module` is an unmatched
argument before any of our code runs. The sixty hand-maintained lines are gone, and so is the table that
first replaced them: `Commands.Grammar.flagOwners()` derives the owning verbs from the declarations
(ADR-0012), and it is used **only for the message** — without it the failure would read "Unknown option
'--module'", which is true and leaves the caller to guess which verb wanted it.
`CliTest.aFlagTheVerbDoesNotTakeIsRejectedNotSilentlyIgnored` is the pin.

This is the one place the port is deliberately *better* than its source rather than faithful to it.

### picocli parses; we own the help and the errors

picocli's own error printing is never used: stderr has to hold exactly one `Failure` object, and picocli
would write a usage block beside it. `Usage` is hand-written and golden-filed, and it is rendered from the
picocli model rather than restated (ADR-0012).

## Consequences

**`--help` needed a declaration to reach us.** This was found by the live protocol, not by the test suite:
`bal`'s launcher rejects an undeclared `--help` with `ballerina: unknown option: '--help'` at exit 1 —
its error, and the wrong answer for a usage request. `LibraryTool` therefore declares a hidden
`--help`/`-h` and puts it back on the argument list, so `Cli` remains the only thing that decides what
help means for which verb. Everything else passes through untouched.

**Exit 0 does not call `System.exit`.** `bal` exits 0 on its own, and short-circuiting the launcher on the
success path would skip whatever it does after a tool returns. Only a non-zero code exits.

**A flag-shaped option value is rejected by hand.** picocli 4.0.1 consumes the next token as an option
value even when it looks like a flag, and the setting that changes that arrived in 4.4. So a flag given
another flag as its value is checked for explicitly: six lines rather than a dependency the distribution
does not ship.
