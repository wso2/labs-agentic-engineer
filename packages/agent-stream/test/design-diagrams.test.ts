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

/**
 * Write-gate behavior for the design's diagram documents (ADR-0020): one
 * mermaid diagram per file, of the kind the file is for, in the subset the
 * design skill prescribes — and a flow's participants resolve against the
 * cell's nodes and the PRD's actors already in the bundle.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { cellNodeIds, checkDesignDiagram, prdActors } from "../src/design-diagrams.ts";
import { FileBundle } from "../src/bundle.ts";

const CELL = `---
sourceSpec: v1
---
title Expense approval

component expense-webapp as "Expense Portal" web-application
component expense-api as "Expense API" service
component expense-db database

east thunder as "Thunder Auth" identity-server
south slack as "Slack" service

north -> expense-webapp
expense-webapp -> expense-api
expense-api -> expense-db
expense-api -> slack
`;

const PRD = `# Expense approval

## Actors
- **Employee** — submits expense claims.
- Line Manager: approves or refuses a claim.

## User Stories
1. As an Employee, I want to submit a claim, so that I get reimbursed.
2. As a Finance admin, I want to export approved claims, so that payroll runs.
`;

const FLOW = "specs/design/flows/submit-a-claim.md";
const DOMAIN = "specs/design/domain-model.md";

const bundle = (files: Record<string, string> = {}) =>
  new FileBundle({ "specs/design/design.cell": CELL, "specs/requirements/prd.md": PRD, ...files });

const flow = (body: string) => `# Submit a claim\n\nAn Employee submits a claim.\n\n\`\`\`mermaid\n${body}\`\`\`\n`;

const GOOD_FLOW = flow(`sequenceDiagram
    actor Employee
    participant expense-webapp
    participant expense-api
    autonumber

    Employee->>expense-webapp: fill claim
    expense-webapp->>+expense-api: POST /claims
    Note over expense-api: validates receipt
    alt no receipt
        expense-api-->>-expense-webapp: 400
    else
        expense-api->>expense-db: insert
        expense-api-->>expense-webapp: 201
        expense-api-)slack: notify
    end
    LineManager->>expense-webapp: approve
`);

const GOOD_DOMAIN = `# Domain model

\`\`\`mermaid
erDiagram
    direction LR
    CLAIM ||--|| RECEIPT : carries
    CLAIM }o--|| EMPLOYEE : "submitted by"
    CLAIM {
        string id PK
        decimal amount
        string status "submitted | approved"
        string[] tags
    }
    RECEIPT {
        string id PK
        string claimId FK
    }
    EMPLOYEE
\`\`\`
`;

test("cellNodeIds: components and boundary externals, frontmatter and edges skipped", () => {
  assert.deepEqual(cellNodeIds(CELL), {
    components: ["expense-webapp", "expense-api", "expense-db"],
    externals: ["thunder", "slack"],
  });
});

test("prdActors: Actors bullets plus every story's subject", () => {
  assert.deepEqual(prdActors(PRD), ["Employee", "Line Manager", "Finance admin"]);
});

test("a path that is not a diagram document is not judged", () => {
  assert.equal(checkDesignDiagram("specs/design/notes.md", "no diagram here", bundle()), null);
  assert.equal(checkDesignDiagram("specs/design/flows/nested/x.md", "", bundle()), null);
});

test("a well-formed flow passes: declared, implicit, aliased and note participants all resolve", () => {
  assert.equal(checkDesignDiagram(FLOW, GOOD_FLOW, bundle()), null);
});

test("a well-formed domain model passes", () => {
  assert.equal(checkDesignDiagram(DOMAIN, GOOD_DOMAIN, bundle()), null);
});

test("no mermaid block, or more than one, is rejected as a shape problem", () => {
  const none = checkDesignDiagram(FLOW, "# Flow\n\nprose only\n", bundle());
  assert.equal(none?.code, "INVALID_DIAGRAM");
  assert.match(none!.message, /no ```mermaid block/);

  const two = checkDesignDiagram(FLOW, GOOD_FLOW + GOOD_FLOW, bundle());
  assert.equal(two?.code, "INVALID_DIAGRAM");
  assert.match(two!.message, /2 mermaid blocks/);
  assert.match(two!.message, /own specs\/design\/flows/);
});

test("the wrong diagram kind for the file is rejected", () => {
  const p = checkDesignDiagram(DOMAIN, GOOD_FLOW, bundle());
  assert.equal(p?.code, "INVALID_DIAGRAM");
  assert.match(p!.message, /must open with `erDiagram`, not `sequenceDiagram`/);
});

test("a sequence statement outside the subset is named by line", () => {
  const bad = flow(`sequenceDiagram
    participant expense-api
    expense-api ==> expense-db: not an arrow
`);
  const p = checkDesignDiagram(FLOW, bad, bundle());
  assert.equal(p?.code, "INVALID_DIAGRAM");
  assert.match(p!.message, /line 8: .* is not a sequence statement/);
});

test("an unclosed alt block is rejected at the line it opened", () => {
  const bad = flow(`sequenceDiagram
    alt something
        expense-api->>expense-db: x
`);
  const p = checkDesignDiagram(FLOW, bad, bundle());
  assert.match(p!.message, /line 7: block opened here is never closed/);
});

test("an ER attribute outside the subset is named by line", () => {
  const bad = `# D\n\n\`\`\`mermaid\nerDiagram\n    CLAIM {\n        id\n    }\n\`\`\`\n`;
  const p = checkDesignDiagram(DOMAIN, bad, bundle());
  assert.equal(p?.code, "INVALID_DIAGRAM");
  assert.match(p!.message, /line 6: not an attribute/);
});

test("an invented participant is rejected, naming what it could have been", () => {
  const bad = flow(`sequenceDiagram
    actor Employee
    Employee->>payments-gateway: pay
`);
  const p = checkDesignDiagram(FLOW, bad, bundle());
  assert.equal(p?.code, "UNKNOWN_PARTICIPANT");
  assert.match(p!.message, /`payments-gateway` is neither a node design.cell declares/);
  assert.match(p!.message, /components: expense-webapp, expense-api, expense-db/);
  assert.match(p!.message, /boundary externals: thunder, slack/);
  assert.match(p!.message, /actor the PRD names \(Employee, Line Manager, Finance admin\)/);
});

test("participants match with case, spaces and hyphens flattened", () => {
  const ok = flow(`sequenceDiagram
    actor line_manager
    line_manager->>Expense-API: approve
`);
  assert.equal(checkDesignDiagram(FLOW, ok, bundle()), null);
});

test("a flow written before the cell is refused until the cell exists", () => {
  const p = checkDesignDiagram(FLOW, GOOD_FLOW, new FileBundle({ "specs/requirements/prd.md": PRD }));
  assert.equal(p?.code, "UNKNOWN_PARTICIPANT");
  assert.match(p!.message, /design.cell is not in the bundle yet/);
});

test("FileBundle: a rejected flow leaves the bundle byte-for-byte unchanged; the fix lands", () => {
  const b = bundle();
  const bad = flow(`sequenceDiagram\n    Employee->>ghost: boo\n`);
  const res = b.addFile(FLOW, bad);
  assert.equal(res.ok, false);
  assert.equal(b.has(FLOW), false);
  assert.deepEqual(b.touched(), []);
  assert.equal(b.addFile(FLOW, GOOD_FLOW).ok, true);
  assert.equal(b.has(FLOW), true);
  // An edit that breaks the diagram is refused the same way.
  const edit = b.editFile(FLOW, "Employee->>expense-webapp: fill claim", "Employee->>ghost: fill claim");
  assert.equal(edit.ok, false);
  assert.equal(edit.ok === false && edit.code, "UNKNOWN_PARTICIPANT");
  assert.equal(b.read(FLOW), GOOD_FLOW);
});

test("a PRD that names no actors leaves `actor` declarations unchecked, nodes still enforced", () => {
  const legacy = new FileBundle({
    "specs/design/design.cell": CELL,
    "specs/requirements/prd.md": "# Old PRD\n\n## Functional Requirements\n\n1. **Open a round**: any signed-in user can start one.\n",
  });
  const ok = flow(`sequenceDiagram\n    actor Teammate\n    Teammate->>expense-webapp: open\n`);
  assert.equal(checkDesignDiagram(FLOW, ok, legacy), null);
  const bad = flow(`sequenceDiagram\n    actor Teammate\n    Teammate->>ghost-service: open\n`);
  assert.equal(checkDesignDiagram(FLOW, bad, legacy)?.code, "UNKNOWN_PARTICIPANT");
});

test("an unterminated mermaid fence is rejected at the line it opened", () => {
  const bad = "# Flow\n\n```mermaid\nsequenceDiagram\n    actor Employee\n";
  const p = checkDesignDiagram(FLOW, bad, bundle());
  assert.equal(p?.code, "INVALID_DIAGRAM");
  assert.match(p!.message, /fence opened at line 3 is never closed/);
});

test("a multi-word declared name is taught the alias form, echoing the line", () => {
  const bad = flow(`sequenceDiagram\n    actor Warehouse Staff\n    participant expense-api\n`);
  const p = checkDesignDiagram(FLOW, bad, bundle());
  assert.equal(p?.code, "INVALID_DIAGRAM");
  assert.match(p!.message, /`actor Warehouse Staff` — a name is ONE word/);
  assert.match(p!.message, /actor WarehouseStaff as Warehouse Staff/);
});

test("a multi-word message endpoint is taught the one-word-id rule", () => {
  const bad = flow(`sequenceDiagram\n    actor Employee\n    Warehouse Staff->>expense-api: log parcel\n`);
  const p = checkDesignDiagram(FLOW, bad, bundle());
  assert.match(p!.message, /`Warehouse Staff->>expense-api: log parcel` — a message's endpoints are one-word ids/);
});

test("a generic unknown line is echoed back verbatim", () => {
  const bad = flow(`sequenceDiagram\n    expense-api ==> expense-db: not an arrow\n`);
  const p = checkDesignDiagram(FLOW, bad, bundle());
  assert.match(p!.message, /`expense-api ==> expense-db: not an arrow`/);
});

test("the skill's worked example passes the gate as written", () => {
  const example = `# Submit a claim\n\nAn Employee submits a claim; the Line Manager approves.\n\n\`\`\`mermaid\nsequenceDiagram\n    actor Employee\n    actor LineManager as Line Manager\n    participant expense-webapp\n    participant expense-api\n\n    Employee->>expense-webapp: submit claim (amount, receipt)\n    expense-webapp->>expense-api: create claim\n    alt no receipt\n        expense-api-->>expense-webapp: refused\n    else\n        expense-api-->>expense-webapp: created\n    end\n    LineManager->>expense-webapp: approve\n\`\`\`\n`;
  assert.equal(checkDesignDiagram(FLOW, example, bundle()), null);
});

test("the repair guidance keeps the declaration's own keyword", () => {
  const bad = flow(`sequenceDiagram\n    participant Order Service\n`);
  const p = checkDesignDiagram(FLOW, bad, bundle());
  assert.match(p!.message, /declare `participant OrderService as Order Service`/);
  const badCreate = flow(`sequenceDiagram\n    create participant Order Service\n`);
  assert.match(checkDesignDiagram(FLOW, badCreate, bundle())!.message, /declare `create participant OrderService as Order Service`/);
});
