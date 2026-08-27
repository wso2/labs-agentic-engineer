# ADR-0012 — Every list in the usage text is rendered from the grammar; only paragraphs are written

**Status:** Accepted

## Context

ADR-0011 made `--help` the whole agent contract and deleted the copy the agent skill held. It did not
look at the copy inside this package.

`Commands` already declared the grammar completely: five verbs, their positionals with param labels
and arity, and every flag on the verb that accepts it. `Usage` then described the same grammar a
second time, in a hand-written string — a five-line synopsis block, a verb list, a flag list, and a
per-verb synopsis for each of the five. Nothing compared the two.

They had already disagreed. The root synopsis advertised `--client C`; the flag list two paragraphs
below it, and all five verb texts, said `--client <Name>`. `Commands` says `paramLabel = "<Name>"`.
Every test was green, because no test could see both.

Three smaller versions of the same problem sat beside it. `FLAG_OWNERS` was a hand-maintained `Map`
from flag to accepting verbs, used to make a rejection message name the right verb — derivable from
where the field is declared, and wrong the moment a flag moved. `KNOWN_FLAGS` was a hand-written
sentence listing the same flags again. And each verb's description existed in two variants, a short
one for its own `--help` and a longer one for the root list, so `overview` promised "No other types"
in one place and "No other types — they are 80% of a large package" in the other.

## Decision

**Every LIST is rendered from picocli's model; every PARAGRAPH is written by hand.**

- `UsageRenderer` renders the root synopsis, each verb's synopsis, the verb list and the flag lists
  from `CommandSpec`. A label, an arity or a flag's owner cannot be stated wrongly, only laid out
  badly.
- One-line prose moves into the annotation next to what it describes — `@Command(description)` and
  `@Option(description)` — and is rendered into **both** the root list and the verb's own text.
  A flag shared by two verbs is a mixin for the same reason: one flag, one sentence.
- `Usage` keeps the paragraphs, stored as sentences and wrapped on the way out. Nothing in the
  package is hand-wrapped; re-flowing after an edit is the upkeep that gets skipped.
- `flagOwners()` and the known-flag list are derived from the same model.

**picocli's own renderer is not used.** Measured against 4.0.1, the version on the Ballerina
distribution's classpath: it sorts options alphabetically and emits them *before* the positionals, in
a tool whose whole grammar is positional-first, and no 4.0.1 setting reorders them. It also writes
`--client=<Name>` and breaks continuation lines inside a flag's own label. Forty lines of layout buy
`<org/name>` in front of the flags. picocli still owns parsing and the model, which is where the
leverage is.

## Consequences

**`<org/name>` is `arity = "1"`,** so picocli enforces it and the null check the four package verbs
each carried is gone. Its suggestion — "Pass 'org/name', e.g. …" — survives, keyed by the slot rather
than by the verb it was written under.

**The two variadic slots are deliberately NOT given a minimum arity.** Measured: `arity = "1..*"` on
`<Name>` or `<keywords>` makes picocli 4.0.1 consume the following tokens *including an unrecognised
flag*, so `search kafka --sigs` parses with `--sigs` as a keyword. That is the silent class this CLI
refuses everywhere else, and it was caught by `CliTest.aFlagTheVerbDoesNotTakeIsRejectedNotSilentlyIgnored`.
Those two keep an emptiness check in `Cli`, and the renderer treats a variadic slot as required
because in this grammar it always is.

**Flags are listed narrowest-first** — a flag one verb declares before one two verbs share, and the
resolution flags, which every verb that reads a package takes, last. The ordering is computed, so the
unknown-flag suggestion changed order with it.

**The usage goldens are now captured through `Cli.run`** rather than by calling the text builder, so
what they pin is what a caller's stderr actually receives — the text, on the right stream, under the
right exit code. `Usage` and `Commands` went back to package-private as a result.

**`LibraryToolTest.everyFlagIsSpelledTheSameWayEverywhere`** compares every `--flag <label>` in the
root text and all five verb texts against the declared `paramLabel`. It is the test that would have
caught `--client C`.

**The README stopped repeating the contract.** It had a third copy — the grammar, the exit codes, the
version-resolution rule, the cache flags. It now points at `--help` and keeps only what `--help` has
no room for: the measurements that justify each verb, and how to build and verify the tool.
