# ADR-0022 — `--help` describes the command; the rules a caller carries into a lookup are the skill's

**Status:** Accepted — narrows ADR-0011 and ADR-0013

## Context

After ADR-0013 the root text was five parts: the synopsis, one sentence saying what the tool reads, the
numbered session, the verb list, and then four closing paragraphs — what a `## Next` block is and that
versions are resolved internally, that a `// Special Agent Note:` IS the import and an absent one is
not, that every other reading rule prints itself, and the stream-and-`kind` contract.

The first four parts answer *what can I ask, and how*. The four paragraphs answer something else: *what
do I do with an answer*. That is a difference in reader and in timing, not in importance:

- They are read at the wrong moment. `--help` is run once, before the first lookup; the import rule
  applies when code is being written, which is several documents later, and the failure taxonomy
  applies when a command has already failed — at which point the caller is reading a JSON object on
  stderr, not this text.
- They are not this command's usage. Nothing in them changes with a verb, a flag or a version. Three
  of the four are true of the *tool*, and the fourth (`## Next` is a pointer, not an instruction) is
  a standing instruction about how to behave, which is what an agent skill is.
- The caller who needs them already has a cheaper channel. The agent that drives this tool loads
  `skills/ballerina/SKILL.md` on every turn; reaching these paragraphs costs it a `--help` call it may
  not make again for the rest of a session.

## Decision

**The root text ends with the verb list.** `Usage.POINTERS`, `Usage.IMPORT_RULE`,
`Usage.RULES_ELSEWHERE` and `Usage.STREAMS` are deleted, and `Usage.root` no longer appends them. It is
41 lines, from 66.

**The four rules move to `skills/ballerina/SKILL.md`**, under `### bal library`, compressed to four
bullets: which package, a note is the import and an absence is not, a `## Next` block is a pointer and
never a plan, and exit 1 with a `kind` that says whether to retry. The skill already held the piping
rule and the `ballerina/*`-versus-`ballerinax/*` rule; these join them, in the document that is in
context when they apply.

**What the output prints, the output still explains.** ADR-0013 is untouched: `type --help` and
`api --help` keep the applied half of the import rule, beside the verbs whose documents carry a note;
`Overview` still says the signature wins over the readme; `Closure` still says to run an edge command
verbatim. The failure taxonomy is still *enforced* here and pinned by ADR-0015 — every `Failure`
carries a `kind` and a `suggestion`, and `CliTest` asserts them. What moved is the paragraph that
described it in advance.

`LibraryToolTest.theHelpTextCarriesNoReadingRule` is the old
`theHelpTextCarriesTheOneRuleNoDocumentCanPrintAndNoOthers`, inverted: every one of the four is asserted
*absent* from the root text, and the two verb pages that keep an applied half are asserted to still have
it. A rule may still not have two homes.

## Consequences

**This reopens ADR-0011's risk, deliberately and narrowly.** The tool and the skill are in separate
repositories on separate clocks, which is exactly the drift that put these paragraphs here — the skill's
langlib list had gone to six modules of fourteen and named the wrong rule. Two things bound the
exposure. First, *what* moved: no langlib list, no flag spelling, no verb roster, no signature — the
class of fact that drifted is generated from the grammar (ADR-0012) or measured with the compiler
(ADR-0009) and stays here. Second, each moved rule has a second, enforced home in this repository: the
import rule on `type`/`api`, the `kind` set in `Failure` and ADR-0015, version resolution in
`Loader.resolve` and in the suggestion `Cli.rejectVersionArguments` prints. The residual risk is that
the skill keeps saying something true of an older tool, and nothing fails. It is accepted; the four
statements are properties of the contract rather than of a release.

**`--help` can no longer be read as the whole agent contract.** ADR-0013 had already broken that for
conditionally-printed rules; this finishes it. An agent driving the tool *without* the skill — a human
at a terminal, a different harness — now learns the grammar and not the reading discipline. For a human
that is the right trade, and it is why the removal is safe: the rules that a wrong reading would turn
into non-compiling code are printed by the documents that carry the thing (`type`, `api`), so a caller
who never opens the skill still meets them where they bite.

**`bal help library` shrinks with it** — `printLongDesc` is the same text, asserted equal in
`LibraryToolTest.theLongDescriptionIsTheUsageText`.
