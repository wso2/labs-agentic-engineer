# ADR-0013 — A rule is printed where it applies, and `--help` keeps only what has nowhere to print

**Status:** Accepted — narrows ADR-0011

## Context

ADR-0011 moved the whole reader's contract into `--help` because the agent skill held a second copy in
another repository on another release clock, and that copy had drifted. It worked. It also produced a
ninety-four line root text with nine sections at equal weight, in which:

- the chain — the one thing a caller who has never run the tool needs — arrived at line 62, under
  fifty-five lines of reference material;
- the verb list and the flag list repeated, in full, what `bal library <verb> --help` says on the page
  the caller reaches next;
- four reading rules were stated in the abstract, some lookups before any of them applied.

Meanwhile the documents learned to navigate: `search`, `overview` and `ops` each end with a `## Next`
block naming real commands with the arguments already filled in off the answer just printed. The help
text was explaining, in prose, a walk the output already performs better.

And three of the four reading rules turned out to be about something the output *prints*. A readme is
printed. An `Also matched` row is printed. A `--deps` edge command is printed. Only one is about an
absence: a prefixed name with **no** `// Special Agent Note:` is pre-declared, and importing it is a
compiler error.

## Decision

**A rule about something the output prints is printed by the view, beside the thing it is about.**
`Overview` introduces the readme with the sentence that says the signature wins; `Ops` prints what an
`Also matched` row means for the answer under it, and only when a branch was actually dropped;
`TypeView` heads the cross-package footer with the line saying to run those commands verbatim.

**`--help` keeps the import rule, because an absence has nowhere to print itself.** It is the only
rule a document cannot carry: nothing is printed at the point where nothing is printed.

**A flag is documented on the verb that accepts it.** The root flag list is gone —
`UsageRenderer.flagList` deleted, `verbFlagList` no longer filters out the shared resolution flags.
The root synopsis still *names* each verb's flags, because that is the grammar and it is generated.

**A `@Command(description)` names the verb; it does not explain it.** The sentences that explained —
why `overview` omits types, what `ops` does without `--sigs` — moved to `Usage.notes`, which only the
verb's own page renders. ADR-0012's rule that one description feeds both layouts is untouched.

**`api` is demoted, not hidden.** Its description now leads with "Last resort", and the walk paragraph
does not mention it. It stays listed: an agent that genuinely needs the whole package must be able to
find it.

**The `Cache:` status line is gone.** ADR-0011's `Usage` javadoc called it contractual — the one place
the cache was allowed to speak, and how an operator proved it was alive inside a runner image. That
reader is not this text's reader. `DocsCache.describe()` still exists and `CacheTest` still covers it.
The `BAL_LIBRARY_CACHE` environment block went with it; `README.md` and `internal-docs/system-design.md`
document both variables.

The root text is **46 lines, from 94**, in the order a caller needs: what it is → what exists → how to
walk → the one rule → the failure contract.

## Consequences

**ADR-0011's guarantee holds, and its mechanism changes.** The drift it fixed was across two
repositories on two clocks. A rule printed by a view is in this repository, in the golden snapshots,
shipped in the same commit as the code that prints it — so the skill still says "run `bal library
--help`" and still needs to say nothing else.

**A rule may not have two homes.** `LibraryToolTest.theHelpTextCarriesTheOneRuleNoDocumentCanPrintAndNoOthers`
asserts the import rule is present AND that the three others are absent, because a second copy inside
one repository drifts as readily as one across two. Their assertions live in `ViewsTest` and
`ViewsAgreeTest`, named for the same reason.

**The root text is no longer the complete contract, and that is the trade.** Two of the moved rules
print conditionally — `Also matched` only when a wildcard branches, the edge footer only under
`--deps` — so a caller who never triggers them never reads them. That is correct: a rule that never
applies is noise. But it means `--help` can no longer be read as "everything this tool will ever tell
you", and anything that needs to be true unconditionally has to stay in `Usage`.

**The three shared resolution flags now repeat on four verb pages.** One description rendered four
times, against a root section that told a caller choosing between five verbs what `--refresh` does and
nothing about which verb to run.

**Version resolution is not described anywhere in the root text.** Deliberate, and it is a promise not
yet kept: the intent is that the tool resolves the version itself rather than asking the caller to.
Today `Loader.resolve` reads `Dependencies.toml` only when `--project-dir` is passed, so an unpinned
lookup still gets Central's latest. Defaulting the project directory to the working directory is the
change that makes this text honest, and it ships separately with its own tests.
