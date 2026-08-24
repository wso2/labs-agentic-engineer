# ADR-0011 — The help text is the whole agent contract, and the skill does not repeat it

**Status:** Accepted

## Context

The reader an agent drives through this tool was described in two places at once. `Usage` held the
grammar — verbs, flags, version resolution, the stream contract. The `ballerina` agent skill
(`skills/ballerina/SKILL.md` in the platform repo) held a second copy: the same verbs as a worked
example chain, the same three flags, and then the part `Usage` never had — how to *read* what comes
back. That a trailing `// Special Agent Note:` is the import to add. That a prefixed name without one
is pre-declared and importing it will not compile. That a readme can be stale where the signature
beside it cannot. That `--deps` prints a follow-up command whose version is load-bearing. That an
`Also matched` row means the answer is short.

Two problems, one of which was already real.

The copy drifts, and the tool cannot tell. The skill's list of langlibs that need no import named six
of fourteen and stated the rule as "the `lang.` prefix", which is wrong — the rule is whether the
module's name is a basic-type keyword, so `lang.value`, `lang.array` and `lang.regexp` do need
importing (ADR-0009). Every test in this repo stayed green through that, because none of them can
see a file in another repository.

And the release cadence is backwards. This tool is vendored into the runner image and the skill is
mirrored into every org's repository; they ship on different clocks. A change to what the tool prints
had to be followed by an edit in the platform repo before an agent would read the new output
correctly, and nothing enforced the pairing.

## Decision

**Everything an agent needs in order to use this tool and read its output lives in `--help`.** The
root text gained a *Reading the output* section carrying the five rules above, the examples became a
chain in the order it is walked, and the per-verb texts carry the rule that belongs to their own
reader: `type` and `api` explain the note and the pre-declared prefix, `ops` explains `Also matched`,
`overview` explains readme-versus-signature, `api` explains the `// --- Service ---` templates and
what a listener that could not be confirmed means.

**The skill says "run `bal library --help`" and stops.** What it keeps is only what is not about this
tool: that a signature comes from here rather than from memory or a web search, that a failed lookup
is not a blocker because `bal build` will name the problem, and package-choice guidance. Nine lines
where there were thirty-seven.

`LibraryToolTest.theHelpTextCarriesTheOutputContractTheSkillNoLongerRepeats` asserts each of the five
rules is present in the root text and in the verb that owns it. It is a substring test on purpose:
the golden file already pins the exact bytes, so what this adds is a *reason* attached to each rule —
deleting one during an edit fails a test whose name says why it was there.

## Consequences

**The root help is now about eighty lines.** That is long for a CLI and it is the right trade: it is
read by a program with no cost of reading, and the alternative was a second document that goes stale.
A human skimming for a flag still finds the grammar in the first forty.

**A change to the output format is a change to `--help`, in the same commit.** The two cannot ship
apart any more, because they are the same file.

**The skill can no longer be tuned to work around a tool defect.** Previously a wrong signature or a
confusing document could be papered over with a line of skill guidance. Now the fix belongs here,
which is where it belonged anyway.

**`bal help library` inherits all of it** — `printLongDesc` is the same text, so the platform's own
`bal` dispatch shows the contract without this tool being run.
