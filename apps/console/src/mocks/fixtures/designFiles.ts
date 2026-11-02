/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

import { DESIGN_REVIEW_PATH } from "@aep/agent-stream";
import type { DesignDependency } from "../../features/design/api/designModel";
import { DECISIONS_PATH } from "../../features/design/model/designFiles";
import { splitQuote } from "../../features/design/model/review";
import { manifestPath, sourcePath } from "../../features/prototype/model/prototypes";
import type { ArtifactDraft } from "./design";
import { SAMPLE_COMPONENT, SAMPLE_MANIFEST, SAMPLE_SOURCE } from "./prototype";

// PROVISIONAL — mock-only; see features/design/api/designModel.ts.
//
// The files a mock design turn writes into the room, as the design agent
// writes them on the platform: the Design card reads its rail from them
// (features/design/model/designFiles.ts), and review.json beside them. They
// follow the turn's catalog (fixtures/design.ts): the architecture's cell, each
// component's design.json and contract, and the acceptance files. Acme gets
// the rest of a full design too: its data model, roles and permissions, the
// expense web app's prototype (the kit sample, fixtures/prototype.ts), the
// Xero dependency Payroll export waits on, its technical decisions, and a
// review that already holds one comment the agent addressed.

const DESIGN = "specs/design/";

const ACME_DOMAIN_MODEL = `# Acme Expenses data model

Claims, the expenses inside them, and the approvals they need.

\`\`\`mermaid
erDiagram
  CLAIM ||--|{ EXPENSE : holds
  CLAIM ||--o{ APPROVAL : needs
  CLAIM {
    string id
    string employee
    string status
    int totalCents
  }
  EXPENSE {
    string id
    int amountCents
    string category
    string receipt
  }
  APPROVAL {
    string id
    string step
    string by
    string reason
  }
\`\`\`

- Amounts are kept in cents, so totals never round.
- A claim over $1,000 needs a second approval from Finance.
`;

const ACME_SECURITY = `{
  "version": 3,
  "permissions": [
    {
      "resource": "claims",
      "component": "expense-api",
      "description": "Expense claims",
      "actions": [{ "handle": "submit", "description": "Submit a claim of their own" }]
    },
    {
      "resource": "approvals",
      "component": "expense-api",
      "description": "Decisions on claims",
      "actions": [
        { "handle": "read", "description": "See their team's pending claims" },
        { "handle": "decide", "description": "Approve or reject a team member's claim, never their own" },
        { "handle": "finance", "description": "Give the second approval a claim over $1,000 needs" }
      ]
    }
  ],
  "groups": [
    { "name": "staff", "description": "Everyone at Acme" },
    { "name": "managers", "description": "People who lead a team" },
    { "name": "finance-team", "description": "The Finance team" }
  ],
  "roles": [
    {
      "name": "Employee",
      "description": "Anyone at Acme: submits their own expenses and follows their claims.",
      "stories": ["F1.1", "F1.3", "F1.4"],
      "grants": ["claims:submit"],
      "assignTo": ["staff"]
    },
    {
      "name": "Manager",
      "description": "Approves or rejects their own team's claims, never their own.",
      "stories": ["F2.1", "F2.2"],
      "grants": ["claims:submit", "approvals:read", "approvals:decide"],
      "assignTo": ["managers"]
    },
    {
      "name": "Finance",
      "description": "Gives the second approval on claims over $1,000.",
      "stories": ["F2.5"],
      "grants": ["claims:submit", "approvals:read", "approvals:finance"],
      "assignTo": ["finance-team"]
    }
  ],
  "testUsers": [
    { "username": "priya", "roles": ["Employee"] },
    { "username": "sam", "roles": ["Manager"] },
    { "username": "dana", "roles": ["Finance"] }
  ]
}
`;

const ACME_DECISIONS = `# Technical decisions

- [data-model] Amounts are stored in cents, never as decimals. *assumed* (F1, F2)
- [architecture] One service, expense-api, holds claims and approvals; the web app only calls it. (F1, F2)
- [roles] A manager approves their own team's claims only, and never their own. (F2)
- [contract] Pending approvals are listed oldest first. *assumed* (F2)
`;

const XERO_DEPENDENCY = `{
  "name": "xero",
  "resource": {
    "name": "xero",
    "description": "Payroll export posts each approved claim to Xero as a bill."
  },
  "suggestions": [
    { "name": "Xero Accounting API", "style": "rest-api", "description": "Xero's published REST API" }
  ]
}
`;

/** A review the agent already answered once: one comment on the technical decisions, addressed. */
const ACME_REVIEW = `{
  "version": 1,
  "artifacts": [
    {
      "path": "${DECISIONS_PATH}",
      "comments": [
        {
          "id": "sample-sam-1",
          "author": { "id": "mock-sub-sam-lee", "name": "Sam Lee" },
          "text": "Why cents and not decimals?",
          "createdAt": "2026-10-09T15:12:00Z",
          "status": "addressed",
          "sentAt": "2026-10-09T15:20:00Z",
          "reply": "Totals of many expenses never round when they are added in cents; the line now says so.",
          "revision": 1,
          "addressedAt": "2026-10-09T15:24:00Z"
        }
      ]
    }
  ]
}
`;

/** The room files a catalog stands for: the cell, each component's design and contract, the documents and the acceptance files. */
function catalogFiles(drafts: readonly ArtifactDraft[]): Record<string, string> {
  const files: Record<string, string> = {};
  for (const d of drafts) {
    const s = d.source;
    if (s.kind === "architecture") files[`${DESIGN}design.cell`] = s.cell;
    if (s.kind === "contract") {
      files[`${DESIGN}components/${d.id}/design.json`] = s.design;
      if (s.openapi) files[`${DESIGN}components/${d.id}/openapi.yaml`] = s.openapi;
    }
    if (s.kind === "acceptance" || s.kind === "document") files[s.path] = s.kind === "acceptance" ? s.content : s.markdown;
  }
  return files;
}

/**
 * Every file a design turn leaves in the room, in the order Design writes
 * them (the room is a map, but the turn streams them in this order, so each
 * gate sees what it reads already there).
 */
export function designRoomFiles(projectName: string, drafts: readonly ArtifactDraft[], dependencies: readonly DesignDependency[]): Record<string, string> {
  const catalog = catalogFiles(drafts);
  if (projectName !== "acme-expenses" || drafts.length === 0) return catalog;
  const cell = `${DESIGN}design.cell`;
  const { [cell]: cellText, ...rest } = catalog;
  const xero = dependencies.some((d) => d.needs === "Xero");
  return {
    ...(cellText !== undefined ? { [cell]: cellText } : {}),
    ...(xero ? { [`${DESIGN}dependencies/xero/dependency.json`]: XERO_DEPENDENCY } : {}),
    [`${DESIGN}domain-model.md`]: ACME_DOMAIN_MODEL,
    [`${DESIGN}security.json`]: ACME_SECURITY,
    ...rest,
    [manifestPath(SAMPLE_COMPONENT)]: SAMPLE_MANIFEST,
    [sourcePath(SAMPLE_COMPONENT)]: SAMPLE_SOURCE,
    [DECISIONS_PATH]: ACME_DECISIONS,
    [DESIGN_REVIEW_PATH]: ACME_REVIEW,
  };
}

/** A decision line as words: no bullet, no `*assumed*`, no markdown escapes, single-spaced. */
function decisionWords(line: string): string {
  return line
    .replace(/^\s*[-*]\s+/, "")
    .replace(/\s*\*assumed\*/g, "")
    .replace(/\\([\\`*_{}[\]()#+\-.!|>~])/g, "$1")
    .replace(/\s+/g, " ")
    .trim();
}

/**
 * How the mock agent applies a Change… comment on a technical decision: the
 * comment quotes the decision on its first line, and the words under the
 * quote become the decision, still `*assumed*`, as the design agent leaves
 * it: the new words are the agent's reading of the comment, and the user
 * keeps them (ADR-0006). The line keeps its artifact tag and its features.
 * Null when the comment quotes no decision in the file, or says nothing
 * under the quote.
 */
export function reviseDecision(markdown: string, comment: string): { from: string; to: string; words: string } | null {
  const split = splitQuote(comment);
  if (split.quote === null) return null;
  const quoted = split.quote.replace(/\s+/g, " ").trim();
  const words = split.words.replace(/\s+/g, " ").trim();
  if (!words) return null;
  const from = markdown.split("\n").find((l) => /^\s*[-*]\s/.test(l) && decisionWords(l) === quoted);
  if (!from) return null;
  const lead = /^\s*[-*]\s+(?:\\?\[[^\]]*?\\?\]\s+)?/.exec(from)![0];
  // The features the decision serves, unless the comment's words name them; the tag goes before them.
  const own = /\s*(\([^()]*\))$/.exec(words);
  const decision = own ? words.slice(0, own.index) : words;
  const features = own ? ` ${own[1]}` : (/\s*\([^()]*\)\s*$/.exec(from)?.[0].trimEnd() ?? "");
  return { from, to: `${lead}${decision} *assumed*${features}`, words };
}
