---
name: coding-agent-handoff
description: Use for every completed SRE RCA remediation handoff — how to search related issues, then file the incident with ae_create_issue.
---

# Coding agent handoff

**Audience: the SRE agent's handoff stage.** You are the last step of an RCA run.
Your job is to create or update the AE issue for this incident.

Your output is one GitHub issue. Filing it is the whole hand-over — once the issue
is created, AE decides what to do with it.

Non-negotiable success condition: before you finish, `ae_create_issue` must have
returned. If you have only called `ae_search_related_issues`, you are not done.

Most of this incident is already **settled** — the classification, the dedupe
key and the tracking policy after creation. One thing is **yours**: the words of
the issue, and the `componentName` you pass.

## WHY YOU ARE HERE

Every RCA report with an identified root cause reaches this stage — filing is
unconditional, never gated on classification. AE derives the classification
(`code_level`, `config_level`, `mixed`, or `none`) from the remediation
agent's verdict on each action, in the `status`, and answers it back on the
create call's response — after you file, never before.

**You are the one who supplies that verdict.** `ae_create_issue` takes a
required `actionStatuses` argument — an array, one entry per action in the
RCA report's `result.recommendations.recommended_actions`, **in that same
order**. Nothing derives this for you and nothing defaults it: the call fails
schema validation without it, for every report, even one with a single
action or none.

| Your value | What it means | What it is to you |
|---|---|---|
| `"revised"` | you expressed it as an OpenChoreo ReleaseBinding change | context, never work |
| `"suggested"` | you could not express it as config | the code-level work |
| `null` | you did not address this action | write from the root cause alone |

Each action already carries a `status` field from the RCA phase — read it,
but do not just echo it uncritically: it is that phase's own placeholder, and
your own verdict is what actually happened when you (the remediation agent)
looked at it this run. If you made no OpenChoreo config change at all this
run, every action's value here is `"suggested"` or `null`, never `"revised"`.
A report with zero recommended actions still requires the argument — pass an
empty array.

The classification is not yours to compute or predict, and neither is the
fix's worth. Even a report remediation fully resolved through configuration
still reaches you and still gets filed — AE answers that one `config_level`, a
ledger entry it does not dispatch. Filing is not a verdict on the work either:
the coding agent examines every issue against the repository and the
specification, and either implements a fix, leaves the issue open with a
diagnostic when confidence is too low, or closes it as `not_planned` when it
concludes no code fix is possible or warranted.

**A low-confidence root cause is still handed over.** A code-level action stays
code-level even when the report is unsure — confidence is never a reason to
withhold it.

## THE FLOW

1. **Search** for related issues with `ae_search_related_issues`.
   *Done when* 1-2 keyword queries have run and you have judged each candidate
   related or not. A discovery pass, not the main task.
2. **Immediately file** the one issue with `ae_create_issue` — including when
   step 1 found a match, for the reason
   in DEDUPLICATION. Include `actionStatuses` (see WHY YOU ARE HERE): the call
   is rejected without it, so building that array is part of this step, not
   an afterthought.
   *Done when* an `ae_create_issue` call has returned and the body it carried
   used the skeleton in WHAT MAKES A GOOD ISSUE. Nothing after it is reported
   anywhere — see CONSTRAINTS.

## RELATED-ISSUE DISCOVERY

Call `ae_search_related_issues` with a handful of **space-separated distinct
keywords** — the component name plus the root-cause symptom terms,
`<component> <subsystem> <exception or failure reason>`. Try 1-2 variations if
the first pass surfaces nothing relevant. If the call itself errors (as distinct
from finding nothing), retry once at most, then file without related-issue
context rather than retry a second time.

An issue is related when it plausibly shares the same root cause or the same
affected component — not merely the same repo or a similar word. When unsure,
leave it out: a wrong link confuses the human reviewer more than a missing one.
Closed matches and partial overlaps become links.

**Related issues are a ledger, not an instruction.** A CLOSED match still matters
on its own terms: it signals a recurrence, so the earlier fix did not hold — say
so when you reference it.

## WHAT MAKES A GOOD ISSUE

**Title** — the component, the site and what is observed there: the handler or
function plus what happens when it runs ("`payment-service` does not log
upstream failures", "`report-api`'s summary handler panics on an unmapped
response"). Name the symptom, never the fix.

**Body** — this skeleton, in this order, dropping only the headings that do not
apply:

      ## RCA summary
      ## Root cause
      ## Evidence
      ## Related issues

The coding agent reads this body, so a fixed shape is what lets it find the
constraint every time. Everything under the headings comes from the RCA report.

### What fills each heading

- **Evidence**: carry across what the report already collected — trace links or
  IDs, and the log lines it quoted.
- **Related issues**: one line each as `- #N — <one-line reason>` (`- #12 — same
  root cause in the same handler, fixed by PR #13 but recurring`). Take each `#N`
  from the search results rather than from memory: those mentions ARE the
  cross-link — GitHub turns each into a clickable reference and adds a
  "mentioned" event on the other issue's timeline.

### Throughout

- Describe the problem and the desired outcome rather than a code diff; the
  coding agent designs the implementation.
- Mention each `revised` action, so the coding agent does not redo in code what
  configuration already handled: "the resource limit was already raised in
  configuration; the unbounded input that exhausts it still needs a fix" — see
  "What you see and must not carry forward" for what to leave out of that
  mention.

### What you see and must not carry forward

The report you receive is complete, including two things that are never
material to the code change you are filing:

- **The `change` patch on an already-`revised` action.** You may say THAT
  configuration already handled part of the incident (see "Mention each
  `revised` action" above); never quote or describe the ReleaseBinding patch
  itself. You can only edit the repository, so a concrete config patch in
  front of you is an invitation to open a wrong pull request expressing
  config as code.

## DEDUPLICATION

Deduplication is settled server-side. AE derives a stable key from the incident
this request belongs to, from the identity headers the run carries. That key is
not an argument you can pass and not a value you can spell, so the only way to
learn what it already covers is to file.

**File; the create call's answer decides dedupe, not your search.** A search hit
is a discovery signal, not the verdict: your judgement of "related" is looser
than the key, and the key sees closed issues, no-change verdicts and
recurrences that a keyword match cannot tell apart. An issue that already
covers this incident is settled by the platform, not by you writing on it.

- **Evidence attaches itself.** On a recurrence the platform appends your
  evidence into the issue's own body, not as a comment — a comment can be
  skipped past, a body section cannot. Where it lands is settled; writing it into
  the body you file is yours.
- **`componentName` is yours to get right.** The key is derived, but this
  argument feeds it, and when the calling process has no identity for this
  incident your value is the only thing that produces a key at all. A wrong one
  dedupes against the wrong history.
- **The tracking labels are settled.** `bug` and `incident` are added to every
  issue you file, on top of any `labels` you pass. `incident` is what lets
  a human filter for every issue this system has ever filed, independent of the
  per-component dedupe key.

## CONSTRAINTS

- **One write, ever.** `ae_create_issue` is the only call you make in this run —
  every other issue you touch is one you read, and the `#N` mentions in your
  body are the only mark you leave on them. A retry after a partial failure
  risks a duplicate that only the dedupe key can catch, so there is no second
  attempt to fall back on.
