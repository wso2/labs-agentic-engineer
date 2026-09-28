import type { IssueInfo } from "../../features/issues/api/queries";

export type IssuesScenario = "some" | "empty" | "error";

export const seedIssues: IssueInfo[] = [
  {
    Number: 42,
    Title: "checkout-service returns 500 on payment callback",
    Body: "The SRE agent found a code-level RCA and dispatched the coding agent.",
    URL: "https://github.com/acme/checkout/issues/42",
    State: "open",
    Labels: ["bug", "incident", "aep"],
  },
  {
    Number: 43,
    Title: "inventory-worker fix needs verification",
    Body: "The coding agent opened a low-confidence fix and left the issue for human review.",
    URL: "https://github.com/acme/checkout/issues/43",
    State: "open",
    Labels: ["bug", "incident"],
    attentionReason: "unverified_fix",
  },
  {
    Number: 44,
    Title: "reports-api has no repository fix",
    Body: "The coding agent closed this incident as not planned.",
    URL: "https://github.com/acme/checkout/issues/44",
    State: "closed",
    Labels: ["bug", "incident"],
    attentionReason: "no_change_verdict",
  },
  {
    Number: 45,
    Title: "auth-service timeout recurred four times",
    Body: "The same incident signature has recurred and needs escalation.",
    URL: "https://github.com/acme/checkout/issues/45",
    State: "open",
    Labels: ["bug", "incident"],
    attentionReason: "escalated",
  },
];

export const issuesError = { message: "issues unavailable" };
