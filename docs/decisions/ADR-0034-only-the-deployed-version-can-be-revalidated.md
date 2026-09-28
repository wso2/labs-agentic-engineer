# ADR-0034 — Only the deployed version can be revalidated

**Status:** Accepted · **Date:** 2026-09-23 ·
[#823](https://github.com/wso2/labs-agentic-engineer/issues/823)

Builds on [ADR-0029](./ADR-0029-validation-drives-scenarios-not-compiled-tests.md) (validation
drives scenarios against the deployed system) and the run-kind split that made a validation run a
run of its own.

## Context

A revalidation asks a version's acceptance criteria again. The console offered it on any version's
page, and no layer below refused it: the handler, `Commands.Revalidate`, the adapter,
`Events.Revalidate` and the supervisor between them checked for a live run, for open dev work, and
for criteria — none of which is version-specific. `adopt.go` even logged *"revalidating a deployed
version"*, which was an assumption written as a statement.

But a validation run **drives whatever is deployed right now**. The runner's endpoints are resolved
from OpenChoreo ReleaseBindings at request time, and its criteria come from the branch tip; the
dispatch env carries no git ref. So revalidating v1 while v3 is deployed judges v3's running code
against today's criteria — and files the verdict on v1's milestone.

It files WORK there too. With the default attempt allowance a `failed` verdict mints one repair
issue per failed scenario into that milestone, and the loop proceeds to fix, build and deploy. So
the cost is not only a false record in a ledger: it is repair work aimed at a version nobody is
running, and a build triggered off it.

## Decision

**A revalidation is refused unless its target is the deployed version.**

1. **"Deployed" is a fact about runs, not about the cluster.** `delivery.DeployedRun` is the newest
   **succeeded dev run** in the project. A running v2 does not unseat a live v1, so the newest run
   is the wrong answer and only a succeeded one counts. This is the same rule `deploy.version`
   reports, which is what makes the refusal agree with what the overview says is live.

2. **The server refuses it.** `Events.Revalidate` resolves the deployed run and rejects any other
   milestone with `ErrVersionNotDeployed`, mapped to **409** beside the two conflicts that were
   already there (a live run, open work). It sits in the event plane rather than in the handler
   because that is where the other two live, and because a direct API call must meet the same rule
   as the button.

3. **The console is told, not asked to derive it.** `ValidationDetail.deployed` is computed
   server-side; the trigger disables on `live || !deployed` with a tooltip naming the condition.
   The console could re-derive this — it holds the build ledger — but deriving platform rules
   client-side is exactly what produced #423, and a second implementation is a second answer.

4. **The other refusals stay the server's alone.** Open work on the version and a version with no
   criteria are refused by message, not by a disabled control: the platform's sentences are better
   than a greyed-out menu item, and neither is a rule the console can evaluate without more reads.

## Consequences

- **An older version's page is readable but not actionable**, which is the honest shape: its
  reports and logs are history, and history is not re-runnable.
- **To re-judge an older version you deploy it first.** That is not a workaround — it is the only
  sequence under which the answer means anything, since the runner tests what is serving.
- **`skipped` is unreachable by trigger**, since a version with no criteria is refused earlier.
- **The rule has one home.** `DeployedRun` is in `delivery` beside the verdict vocabulary, so the
  flag the page reads and the refusal the endpoint gives cannot diverge.
