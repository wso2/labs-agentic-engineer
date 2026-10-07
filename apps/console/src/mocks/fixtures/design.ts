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

import type { ArtifactSource, DesignArtifact, DesignDependency } from "../../features/design/api/designModel";
import type { SpecFeature } from "../../features/spec/api/specModel";
import { parseLine, type LineBlock } from "../../features/spec/model/ids";

// PROVISIONAL — mock-only until E1/E4/E5; see features/design/api/designModel.ts.
//
// What the mock design agent writes. Acme Expenses gets the full design of a
// product with screens: a prototype made from the wireframes, a flow, roles,
// the data model, the architecture (a cell), a component and its contract,
// security, and one acceptance file per feature. The viewers' formats follow
// the old console's fixtures (classic-console tag, src/mocks/fixtures/project.ts and
// validation.ts): wireframes DSL, cell DSL, design.json (the web app's too,
// which is what Make prototype looks for), OpenAPI YAML,
// Gherkin with rules tagged `@story-F2.3`. Payroll export is never designed
// here (it waits on a question), so nothing below draws it; the design turn
// finds that it needs Xero, which blocks its build and nothing else.
//
// Any other project gets an acceptance file per designed feature, written
// from its stories. It has no wireframes, so it gets no prototype: a design
// only has a prototype when the product has screens.
//
// Addressing a comment can change the design. The changes are `tweaks` the
// catalog is drawn with, so a later Update design keeps them.

/** A change a comment made to Acme's design, kept across later design turns. */
export type DesignTweak = "receipts" | "approveRight" | "bigTotal" | "approverName";

/** An artifact as the catalog writes it; the design state stamps its revisions. */
export type ArtifactDraft = Omit<DesignArtifact, "addedIn" | "changedIn">;

const NAVBAR = `  navbar "Acme Expenses | My claims | Approvals | Reports"`;

function expenseWebDsl(t: ReadonlySet<DesignTweak>): string {
  const total = t.has("bigTotal") ? `  heading "$1,200.00 · 3 expenses"` : `  text "Total $1,200.00 · 3 expenses"`;
  const receipts = t.has("receipts")
    ? `  row
    card "Hotel receipt\\n$780.00"
    card "Flight receipt\\n$390.00"
    card "Taxi receipt\\n$30.00"
`
    : "";
  const approve = `    button "Approve" primary -> Approved`;
  const reject = `    button "Reject"`;
  const actions = t.has("approveRight") ? `  row\n${reject}\n    right\n${approve}` : `  row\n${approve}\n${reject}`;
  const pendingHead = t.has("approverName") ? "Employee | Claim | Total | Submitted | Last action" : "Employee | Claim | Total | Submitted";
  const pendingRows = t.has("approverName")
    ? `    row "Priya Shah | Claim 42 | $1,200.00 | Mon 22 Sep | submitted by Priya"
    row "Dev Patel | Claim 43 | $84.00 | Tue 23 Sep | submitted by Dev"`
    : `    row "Priya Shah | Claim 42 | $1,200.00 | Mon 22 Sep"
    row "Dev Patel | Claim 43 | $84.00 | Tue 23 Sep"`;
  return `screen NewClaim "Priya photographs her receipts and submits them as one claim"
${NAVBAR}
  heading "New claim"
  split 60/40
    left
      table "Expense | Category | Amount"
        row "Hotel, Denver | travel | $780.00"
        row "Flight DEN-SFO | travel | $390.00"
        row "Taxi | travel | $30.00"
      button "Add a receipt photo"
    right
      card "Read from the photo"
        text "Amount and date are filled in for you"
        select "Category: travel"
      button "Submit claim" primary -> MyClaims

screen MyClaims "Priya sees which of her claims are pending, approved or rejected"
${NAVBAR}
  heading "My claims"
  table "Claim | Total | Status"
    row "Claim 42 | $1,200.00 | Waiting for Sam"
    row "Claim 39 | $64.00 | Approved"
  button "New claim" -> NewClaim

screen PendingApprovals "Sam sees his team's pending claims, oldest first"
${NAVBAR}
  heading "Pending approvals"
  table "${pendingHead}"
${pendingRows}
  button "Open claim 42" primary -> ClaimDetail

screen ClaimDetail "Sam approves or rejects the claim with a reason"
${NAVBAR}
  heading "Claim 42 · Priya Shah"
${total}
${receipts}  table "Expense | Category | Amount"
    row "Hotel, Denver | travel | $780.00"
    row "Flight DEN-SFO | travel | $390.00"
    row "Taxi | travel | $30.00"
  input "Reason: Client visit, agreed in advance"
${actions}

screen Approved "A claim over $1,000 waits for Finance after the manager"
${NAVBAR}
  heading "Approved by you"
  text "Waiting for Finance: claims over $1,000 need a second approval"
  button "Back to pending approvals" -> PendingApprovals

flow "Submit a claim"
  role "Priya (employee)"
  description "Priya photographs three receipts and submits them as claim 42"
  NewClaim
  MyClaims

flow "Approve a claim"
  role "Sam (manager)"
  description "Sam approves claim 42, which then waits for Finance"
  PendingApprovals
  ClaimDetail
  Approved
`;
}

const ARCHITECTURE = `title Acme Expenses
version v1

component expense-web web-app
component expense-api service

north Staff -> expense-web : HTTPS
expense-web -> expense-api
expense-api -> south expenses-db : postgres
expense-web -> east company-sso : sign-in
`;

const EXPENSE_WEB_DESIGN = `{
  "name": "expense-web",
  "type": "web-application",
  "version": "0.1.0",
  "language": "TypeScript",
  "buildpack": "docker",
  "appPath": "expense-web",
  "exposure": "intranet",
  "description": "Where staff submit claims and managers approve them.",
  "dependencies": [
    { "kind": "component", "name": "expense-api" }
  ]
}`;

const EXPENSE_API_DESIGN = `{
  "name": "expense-api",
  "type": "service",
  "version": "0.1.0",
  "language": "Go",
  "buildpack": "docker",
  "appPath": "expense-api",
  "entrypoint": "cmd/main",
  "exposure": "intranet",
  "description": "Claims, expenses and their approvals.",
  "dependencies": [
    { "kind": "platform-resource", "name": "expenses-db", "resourceType": "postgres-cnpg" }
  ]
}`;

function expenseApiOpenApi(t: ReadonlySet<DesignTweak>): string {
  return `openapi: 3.0.0
info:
  title: expense-api
  version: 0.1.0
  description: Claims, expenses and their approvals.
components:
  securitySchemes:
    oauth2:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://sso.acme.example/authorize
          tokenUrl: https://sso.acme.example/token
          scopes:
            claims:submit: Submit a claim
            approvals:read: Read a team's pending claims
            approvals:decide: Approve or reject a claim
            approvals:finance: Give a second approval
security:
  - oauth2: []
paths:
  /claims:
    post:
      summary: Submit a claim (F1.3)
      security:
        - oauth2: [claims:submit]
      responses:
        "201":
          description: The claim, waiting for the manager
  /approvals/pending:
    get:
      summary: A manager's pending claims, oldest first (F2.1)
      security:
        - oauth2: [approvals:read]
      responses:
        "200":
          description: The claims${t.has("approverName") ? ", each with its last action and who took it" : ""}
  /claims/{id}/approve:
    post:
      summary: Approve a claim with a reason (F2.2)
      security:
        - oauth2: [approvals:decide]
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: The claim, approved or waiting for Finance
  /claims/{id}/finance-approve:
    post:
      summary: Second approval over $1,000 (F2.5)
      security:
        - oauth2: [approvals:finance]
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: The claim, approved
`;
}

const OPENAPI_ROLES = {
  "claims:submit": ["Employee", "Manager", "Finance"],
  "approvals:read": ["Manager"],
  "approvals:decide": ["Manager"],
  "approvals:finance": ["Finance"],
};

function approvalsFeature(t: ReadonlySet<DesignTweak>): string {
  return `Feature: F2 Approvals
  Managers approve or reject their team's claims; large claims also go to Finance.

  @story-F2.1
  Rule: A manager sees the team's pending claims in one list, oldest first

    Scenario: The oldest claim is first
      Given Priya submitted claim 42 on Monday
      And Dev submitted claim 43 on Tuesday
      When Sam opens Pending approvals
      Then claim 42 is listed before claim 43

  @story-F2.2
  Rule: A manager approves or rejects a claim with a reason

    Scenario: Approving with a reason
      Given claim 43 for $84.00 is waiting for Sam
${t.has("receipts") ? "      And Sam has opened its receipt\n" : ""}      When Sam approves it with the reason "Client lunch"
      Then claim 43 is approved
      And the approval records Sam and his reason

    @negative
    Scenario: A rejection needs a reason
      Given claim 43 is waiting for Sam
      When Sam rejects it without a reason
      Then claim 43 is still waiting for Sam
      And Sam is asked for a reason

    Scenario: A rejected claim goes back to be edited
      Given Sam rejected claim 43 with the reason "No receipt"
      When Priya opens claim 43
      Then she can edit it and submit it again

  @story-F2.4
  Rule: A deputy approves while the manager is on leave

    Scenario: A deputy approves while the manager is on leave
      Given Sam is on leave and named Lee as his deputy
      And claim 43 is waiting for Sam
      When Lee opens Pending approvals
      Then claim 43 is in Lee's queue
      And Lee can approve it for Sam

  @story-F2.5
  Rule: Finance gives a second approval on any claim over $1,000

    Scenario: A large claim waits for Finance
      Given claim 42 for $1,200.00 is waiting for Sam
      When Sam approves claim 42
      Then claim 42 waits for Finance
      And Dana sees claim 42 in Second approvals
`;
}

const SUBMIT_FEATURE = `Feature: F1 Submit expenses
  Employees record expenses with a receipt photo and submit them as a claim.

  @story-F1.1
  Rule: The amount and date are read from a receipt photo

    Scenario: A clear photo fills in the expense
      Given Priya has a photo of a $30.00 taxi receipt from 18 Sep
      When she adds the photo to a new claim
      Then the expense reads $30.00 on 18 Sep

  @story-F1.2
  Rule: Every expense has a category

    Scenario: Picking a category
      Given Priya added a hotel receipt
      When she picks travel
      Then the expense is filed under travel

  @story-F1.3
  Rule: Expenses are saved as a draft and submitted later as one claim

    Scenario: Submitting three expenses as one claim
      Given Priya has three expenses in a draft
      When she submits the draft
      Then claim 42 holds the three expenses and waits for Sam

    @negative
    Scenario: A large expense without a receipt is refused
      Given Priya adds a $40.00 expense without a receipt
      When she submits the claim
      Then she is told a receipt is needed above $25

  @story-F1.4
  Rule: An employee sees where each claim stands

    Scenario: A submitted claim shows as pending
      Given Priya submitted claim 42
      When she opens My claims
      Then claim 42 shows "Waiting for Sam"
`;

function flowApprove(t: ReadonlySet<DesignTweak>): ArtifactSource {
  return {
    kind: "flow",
    lanes: [
      { name: "Priya", role: "employee" },
      { name: "Sam", role: "manager" },
      { name: "Expense app" },
      { name: "Dana", role: "finance" },
    ],
    steps: [
      { from: 0, to: 2, text: "Submits claim 42: 3 expenses, $1,200", story: "F1.3" },
      { from: 2, to: 1, text: "Claim 42 is waiting for your approval", story: "F2.1" },
      {
        from: 1,
        to: 2,
        text: t.has("receipts")
          ? "Opens the 3 receipts, then approves with a reason"
          : 'Approves with a reason: "client visit, agreed"',
        story: "F2.2",
      },
      { from: 2, to: 3, text: "Over $1,000, so it needs a second approval", story: "F2.5", source: "policy p.7" },
      { from: 3, to: 2, text: "Dana approves: claim 42 is approved", story: "F2.5" },
      { from: 2, to: 0, text: "Claim 42 shows as approved", story: "F1.4" },
    ],
  };
}

/** Acme's design over the features designed so far. */
function acmeCatalog(designed: ReadonlySet<string>, tweaks: ReadonlySet<DesignTweak>): ArtifactDraft[] {
  const both = designed.has("F1") && designed.has("F2");
  const all = [...designed].sort();
  const out: ArtifactDraft[] = [
    {
      id: "prototype",
      title: "Wireframes: the expense web app",
      depth: "business",
      features: all,
      source: { kind: "prototype", path: "specs/design/components/expense-web/wireframes.dsl", dsl: expenseWebDsl(tweaks) },
    },
  ];
  if (both) {
    out.push({
      id: "flow-approve",
      title: "Flow: a claim from submit to approval",
      depth: "business",
      features: ["F1", "F2"],
      source: flowApprove(tweaks),
    });
  }
  out.push(
    {
      id: "roles",
      title: "Roles: who can do what",
      depth: "business",
      features: all,
      source: {
        kind: "roles",
        roles: ["Employee", "Manager", "Finance", "Admin"],
        rows: [
          { action: "Submit a claim", grants: [true, true, true, false] },
          { action: "Approve their team's claims", grants: [false, true, false, false] },
          { action: "Give a second approval over $1,000", grants: [false, false, true, false] },
          { action: "Manage users and roles", grants: [false, false, false, true] },
        ],
        note: "Everyone signs in with company SSO (P4, org default).",
      },
    },
    {
      id: "data",
      title: "Data model: Claim, Expense, Approval",
      depth: "business",
      features: all,
      source: {
        kind: "data",
        records: [
          {
            name: "Claim",
            about: "what an employee submits for approval",
            fields: [
              { name: "Employee", example: "Priya Shah" },
              { name: "Status", example: "waiting for Finance" },
              { name: "Total", example: "$1,200.00, kept in cents (P3)" },
              { name: "Submitted", example: "Mon 22 Sep" },
            ],
          },
          {
            name: "Expense",
            about: "one receipt inside a claim",
            fields: [
              { name: "Amount", example: "$780.00" },
              { name: "Category", example: "travel" },
              { name: "Receipt", example: "photo, kept 7 years (P2)" },
            ],
          },
          {
            name: "Approval",
            about: "one decision on a claim",
            fields: [
              { name: "Step", example: "manager, then finance over $1,000" },
              { name: "By", example: "Sam Lee" },
              { name: "Reason", example: "Client visit, agreed in advance" },
            ],
          },
        ],
        relation: "A claim has one or more expenses, and one or two approvals. Example values are from Priya's claim 42.",
      },
    },
    {
      id: "architecture",
      title: "Architecture: web app, API, database",
      depth: "technical",
      features: all,
      source: { kind: "architecture", cell: ARCHITECTURE },
    },
    {
      id: "expense-web",
      title: "Component and contract: expense-web",
      depth: "technical",
      features: all,
      source: { kind: "contract", design: EXPENSE_WEB_DESIGN, openapi: null },
    },
    {
      id: "expense-api",
      title: "Component and contract: expense-api",
      depth: "technical",
      features: all,
      source: {
        kind: "contract",
        design: EXPENSE_API_DESIGN,
        openapi: expenseApiOpenApi(tweaks),
        roles: OPENAPI_ROLES,
        // The Resources mock's registered currency-service, reused here.
        resources: ["currency-service"],
      },
    },
    {
      id: "security",
      title: "Security: sign-in and grants",
      depth: "technical",
      features: all,
      source: {
        kind: "security",
        rows: [
          { subject: "Sign-in", rule: "company SSO (P4, org default)" },
          { subject: "Manager", rule: "may approve claims of their own team only" },
          { subject: "Finance", rule: "second approvals over $1,000" },
          { subject: "Audit", rule: "every approval, rejection and edit is logged (P1)" },
        ],
      },
    },
  );
  if (designed.has("F1")) {
    out.push({
      id: "acceptance-F1",
      title: "Acceptance: F1-submit-expenses.feature",
      depth: "business",
      features: ["F1"],
      source: { kind: "acceptance", path: "specs/validation/acceptance/F1-submit-expenses.feature", content: SUBMIT_FEATURE },
    });
  }
  if (designed.has("F2")) {
    out.push({
      id: "acceptance-F2",
      title: "Acceptance: F2-approvals.feature",
      depth: "business",
      features: ["F2"],
      source: { kind: "acceptance", path: "specs/validation/acceptance/F2-approvals.feature", content: approvalsFeature(tweaks) },
    });
  }
  return out;
}

function slug(text: string): string {
  return text.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
}

/** An acceptance file written from a feature's stories: one rule per story, tagged with it. */
function storiesFeature(feature: SpecFeature, lines: LineBlock[]): string {
  const rules = lines
    .map((l) => parseLine(l.text, l.emphasis))
    .filter((p) => p.lead?.id.startsWith(`${feature.id}.`))
    .map(
      (p) => `
  @story-${p.lead!.id}
  Rule: ${p.body}

    Scenario: ${p.body}
      Given ${feature.name} as designed
      When it is used as the story says
      Then the story holds`,
    );
  return `Feature: ${feature.id} ${feature.name}\n  ${feature.purpose}\n${rules.join("\n")}\n`;
}

function genericCatalog(features: SpecFeature[], lines: ReadonlyMap<string, LineBlock[]>): ArtifactDraft[] {
  return features.map((f) => {
    const file = `${f.id}-${slug(f.name)}.feature`;
    return {
      id: `acceptance-${f.id}`,
      title: `Acceptance: ${file}`,
      depth: "business",
      features: [f.id],
      source: { kind: "acceptance", path: `specs/validation/acceptance/${file}`, content: storiesFeature(f, lines.get(f.path) ?? []) },
    };
  });
}

/** The design over every designed feature, as the agent writes it for this project. */
export function designCatalog(
  projectName: string,
  designed: SpecFeature[],
  lines: ReadonlyMap<string, LineBlock[]>,
  tweaks: ReadonlySet<DesignTweak>,
): ArtifactDraft[] {
  if (designed.length === 0) return [];
  if (projectName === "acme-expenses") return acmeCatalog(new Set(designed.map((f) => f.id)), tweaks);
  return genericCatalog(designed, lines);
}

/** What the design finds a feature needs from outside the product. */
export function dependencyOf(projectName: string, feature: SpecFeature): DesignDependency | null {
  if (projectName !== "acme-expenses" || feature.name !== "Payroll export") return null;
  return {
    featureId: feature.id,
    needs: "Xero",
    question: "How does Payroll export reach Xero?",
    why: "Payroll export sends approved claims to a payroll system. Your policy names Xero (p.9), so the design uses Xero, and nothing in your organisation provides it yet. Until this is settled, Payroll export can't be built; everything else can.",
    options: ["Use Xero's published API", "We have our own contract"],
    answer: null,
  };
}

/** What addressing one comment does: the agent's reply, the tweak it makes, the artifacts and spec line it changes. */
export interface Feedback {
  reply: string;
  tweak: DesignTweak | null;
  /** Artifacts the change touches, besides the one the comment is on. */
  artifacts: string[];
  /** The spec line it rewrites, when the comment is really a requirement. */
  spec: { featureId: string; lineId: string; from: string; to: string } | null;
}

/** How the mock agent reads a comment. Acme's prototype knows a few; anything else is noted in the design as said. */
export function feedbackFor(projectName: string, text: string): Feedback {
  const t = text.toLowerCase();
  const acme = projectName === "acme-expenses";
  if (acme && /receipt/.test(t)) {
    return {
      reply: "Sam now sees the receipts before approving. That's a requirement, so F2.2 in Approvals changed too.",
      tweak: "receipts",
      artifacts: ["prototype", "flow-approve", "acceptance-F2"],
      spec: {
        featureId: "F2",
        lineId: "F2.2",
        from: "- F2.2 As a manager, I approve or reject a claim with a reason.",
        to: "- F2.2 As a manager, I approve or reject a claim with a reason, after seeing its receipts.",
      },
    };
  }
  if (acme && /right|left/.test(t) && /approve|reject|button/.test(t)) {
    return { reply: "Moved: Approve is on the right, Reject on the left.", tweak: "approveRight", artifacts: ["prototype"], spec: null };
  }
  if (acme && /total|bigger|larger|prominent/.test(t)) {
    return { reply: "The claim total is now the largest figure on the claim.", tweak: "bigTotal", artifacts: ["prototype"], spec: null };
  }
  if (acme && /approver|who approved|last action/.test(t)) {
    return {
      reply: "Each pending claim now shows its last action and who took it.",
      tweak: "approverName",
      artifacts: ["prototype", "expense-api"],
      spec: null,
    };
  }
  return { reply: `Changed in the design: "${text.trim()}".`, tweak: null, artifacts: [], spec: null };
}
