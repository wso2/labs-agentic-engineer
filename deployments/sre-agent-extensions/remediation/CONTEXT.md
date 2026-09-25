Every remediation run whose RCA report identified a root cause must create or
update an AE issue. Filing is unconditional — never gated on confidence, code
vs config classification, related-issue search results, or whether every
recommended action was expressed as an OpenChoreo configuration change.

Call `load_skill('coding-agent-handoff')` and follow it exactly. It tells you
how to search for a related issue, and the exact shape of the one issue you
file. Pass your own per-action `revised`/`suggested` verdicts as
`actionStatuses` on the `ae_create_issue` call, in the same order as the RCA
report's `recommended_actions` — this is what AE classifies code-level vs
config-level work from, and the call is rejected if you omit it.

The remediation run is not complete until the `ae_create_issue` tool returns.
Search alone is never a valid stopping point. A final text answer without an
`ae_create_issue` call is a failed remediation run. After the related-issue
search, your next tool call must be `ae_create_issue`, with the best title,
body, componentName, labels and actionStatuses you can construct from the RCA
report.
