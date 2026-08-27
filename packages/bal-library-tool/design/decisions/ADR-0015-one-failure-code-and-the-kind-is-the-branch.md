# ADR-0015 — One failure code, and the `kind` is the branch

**Status:** Accepted — supersedes the exit-code half of ADR-0002

## Context

The tool shipped with three codes: 0 for a document, 1 for "Central could not answer, retryable in
principle", 2 for "your arguments are wrong, re-running cannot help". The split was meant to be the
first thing an agent branched on.

A seven-case sweep of real agent runs measured what it actually did. Three of the four paths that
matter were **inverted**:

| call | code | what the code told the caller | what was true |
|---|---|---|---|
| `bal library --help` | 2 | "your arguments are wrong" | the caller asked for help and got it |
| `package-not-found` | 1 | "run it once more" | no retry can find a package that is not there |
| a submodule that does not resolve | 1 | "run it once more" | the recovery is `--version`, an argument |
| `symbol-not-found` | 2 | edit the arguments | correct |

Worse, nothing surfaced it. Every agent in the sweep piped the command through `head` or `grep`, so
the `$?` it could have read was the pipe's, not the tool's — and the two independent statements of
"run it unpiped" (the skill and `--help`) moved that in zero of nineteen calls. A discriminator no
reader reads cannot be worth the risk of it being wrong, and it was wrong.

The failure JSON already carried the honest discriminator: `kind`, one of six values, on stderr, in
the same bytes as the `suggestion` that says what to do about it.

## Decision

**Two codes.**

| | |
|---|---|
| exit 0 | stdout holds the requested document, complete |
| exit 1 | one JSON `Failure` object on stderr, nothing on stdout |

`Failure.exitCode()` is deleted. `Cli.fail` returns 1 for every failure, and the sealed switch that
forced a new failure mode to declare its cost now does that through `kind()` and `describe()`, which
are the fields a caller actually reads.

**A usage request is answered, not failed.** `--help`, a verb's `--help`, and a bare `bal library`
print the usage text to **stdout** at **exit 0**. This reverses ADR-0002, which put usage on stderr
"because stdout is the document and nothing else". Once help is not a failure, that rule points the
other way: help *is* the document when help is what was asked for, and usage on stderr under exit 0
would hand a redirecting caller an empty file — the silent class this CLI refuses everywhere else.

**What replaces the split is the `kind`**, and `--help` now says so rather than teaching the codes:

- `upstream`, `timeout` — worth running again unchanged
- `validation`, `package-not-found`, `symbol-not-found` — need a different command, which the
  `suggestion` names
- `schema-drift` — for a human; no argument will help
- none of them is a licence to guess a signature

## Consequences

**stderr is now one thing.** It holds a `Failure` object or it is empty. The old contract had it
holding JSON *or* usage text, which every parser had to allow for.

**Version skew stays loud.** ADR-0002's reason for verb-first was that a stale binary fails a leading
verb against the qualified-name pattern — that is unchanged; it now reads as `kind: validation`
naming the verbs it does know, rather than as a code that happens to differ from the retryable one.

**A caller that branched on 2 sees 1.** Nothing shipped did: the sweep found the exit code was read
through a pipe in every case, and the skill has never held a copy of the contract (ADR-0011).

**The help text carries the change.** `Usage.STREAMS` is rewritten around the `kind` values, and
`type`'s one-line description no longer says "is exit 2 with candidates". Both are golden-filed, so
the diff is reviewable.
