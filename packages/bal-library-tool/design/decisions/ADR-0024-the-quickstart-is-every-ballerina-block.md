# ADR-0024 — The quickstart is every Ballerina block, unread and last

Status: accepted · 2026-08-24

Supersedes the selection rule and the position [ADR-0017](ADR-0017-the-entry-document-is-ordered-to-survive-a-window.md)
gave the quoted readme code, and removes the name check it ran over it.

Not a supersede of [ADR-0010](ADR-0010-a-named-gap-beats-a-plausible-guess.md), which is about signatures
this tool GENERATES from a payload and stands unchanged. The distinction is the whole argument under **Decision** below: naming a gap in your own output is honesty, and annotating someone else's quoted
document is a different act.

## Context

ADR-0017 replaced "append the whole readme" with a selected Usage section, because the readme was 44%
of the corpus and its code was 12% of that, so a `head -100` reached a worked snippet in 0 of 11
packages. The selection was by content: a block was kept when it **constructed** a client, **called**
one with `->`, or **attached** a service to a listener, one of each before a second of any, inside a
40-line budget.

That rule was derived from the eleven packages of the 2026-08-15 sweep. All eleven are connectors. The
rule is a description of a connector's quickstart, and a package that is not one falls straight through
it:

| package | Ballerina blocks in readme | reached the reader |
|---|---|---|
| `ballerina/log` | 8 | **0** |
| `ballerina/data.jsondata` | 6 | **0** |
| `ballerina/io` | 0 | 0 |
| `ballerina/xlsx` | 13 | **1** |

`ballerina/log`'s entire surface is module-level functions. It publishes eight worked blocks, none of
which constructs, calls with `->` or attaches, so `overview` showed a package with no examples — and
said nothing, because a block that failed the role test was never a candidate and so never reached the
`omitted` counter that would have printed the pointer to `guide`.

`ballerina/xlsx` is the case that decided this. One of its thirteen blocks qualified, and it qualified
on `sftp->get` and `sftp->put` — **another package's client**, from `ballerina/ftp`. The name check then
marked both of those lines `⚠`. So the single example `overview` carried for a spreadsheet library was
the one block containing two lines the same document said were wrong, while `@xlsx:Name` — the header
mapping, which is not optional trivia for a spreadsheet library and is not inferable from any
signature — and every `xlsx:parseSheet` call were dropped as demonstrating nothing. A rule that admits
a foreign client and rejects the package's own entry point is not a filter worth having.

The corpus could not have caught this. All eleven recorded fixtures were connectors with a client.

## Decision

**The fence is the whole rule.** A block whose fence says `ballerina` or `bal` is quoted; everything
else is not. No role classification, no dedupe, no budget, no per-block cap, no name check. Order is
the readme's own, because a readme is written basic-first and the reader is walking it.

**The section goes last.** Uncapped, its length is a property of the package rather than of this tool —
`ballerinax/postgresql` publishes 28 blocks and its map is now 365 lines. Last is the only position an
unbounded section can hold without pushing the navigation behind a pipe. Measured across the corpus,
`## Next` is at line 15 in the two widest packages and the map's own text ends by line 51, so a
`head -100` now loses trailing examples instead of losing the map.

**The `⚠` marks and the `guide` stale-name paragraph are removed.** ADR-0010 holds that a named gap
beats a plausible guess, and that is still true of a signature this tool GENERATES. It is not the same
claim about a document this tool QUOTES. The reader is already told, in the sentence above the
quotation, that this is Central's text and that the generated signatures win where the two disagree —
which is the general form of what each mark said about one line. What the marks added on top was the
tool arguing with the package's own readme inside the quotation, on a check that had a known blind spot
of its own, recorded when it was built: `ballerinax/redis`'s `string value = check redis->get("key")` is
uncheckable by it, because `get` IS declared and the error is in the union arithmetic. Names were never the
axis on which quoted code fails.

## Consequences

- **`ballerina/xlsx` and `ballerina/log` join the fixture corpus.** Neither declares a client, which is
  the shape the corpus had none of and the reason a connector-derived rule survived eleven packages.
  They are the regression cases for this decision.
- **`overview` is no longer bounded end to end.** `theMapStaysInsideItsOwnBoundRegardlessOfPackageSize`
  now measures the map with the quotation cut out — what must stay bounded is what the tool WRITES,
  which `theOverviewGeneratesNoSignatureAtAll` states from the other side. This is the trade the
  decision makes, and it is why the section is last.
- **`guide` is now a pure reproduction.** Its verb had one job and now has exactly that job.
- **Import-only blocks are quoted.** The old rule dropped them as demonstrating nothing. The module
  alias a package is imported under is a fact the reader needs, and `ballerina/xlsx` is imported as
  `xlsx` while `ballerinax/googleapis.gmail` is imported as `gmail`.
- **A quoted block may not compile on its own,** and this is accepted rather than fixed: it may hold a
  literal `...` placeholder, use a variable another block declared, or name something this version no
  longer declares. All three are the package's own text, which the sentence above the section says.
- `Snippets` drops from 561 lines to 108 (111 with the licence header). `Names` and `PathTree` survive on their other callers.
