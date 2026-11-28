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

import type { DesignArtifact, DesignDependency } from "../../features/design/api/designModel";
import type { SpecFeature } from "../../features/spec/api/specModel";
import { parseLine, type LineBlock } from "../../features/spec/model/ids";

// PROVISIONAL — mock-only until E1/E4; see features/design/api/designModel.ts.
//
// What the mock design agent's catalog holds (the design model the mock
// serves). Acme Expenses gets the full design of a product with screens:
// roles, the architecture (a cell), its components and their contract,
// security, and one acceptance file per feature; the files
// the turn writes into the room for them are fixtures/designFiles.ts. The
// formats follow the old console's fixtures (classic-console tag): cell DSL,
// design.json, OpenAPI YAML, Gherkin with rules tagged `@story-F2.3`.
// Payroll export is never designed here (it waits on a question), so nothing
// below draws it; the design turn finds that it needs Xero, which blocks its
// build and nothing else.
//
// Any other project gets an acceptance file per designed feature, written
// from its stories.

/** An artifact as the catalog writes it; the design state stamps its revisions. */
export type ArtifactDraft = Omit<DesignArtifact, "addedIn" | "changedIn">;

const ARCHITECTURE = `title Acme Expenses

component expense-web as "Expense web app" web-application
component expense-api as "Expense API" service
component expenses-db as "Expenses database" database

east company-sso as "Company SSO" identity-server

north -> expense-web

expense-web -> expense-api
expense-web -> company-sso
expense-api -> company-sso
expense-api -> expenses-db
`;

const EXPENSE_WEB_DESIGN = `{
  "name": "expense-web",
  "type": "web-application",
  "version": "0.1.0",
  "language": "TypeScript",
  "buildpack": "docker",
  "appPath": "expense-web",
  "entrypoint": "src/main.tsx",
  "exposure": "intranet",
  "description": "Where staff submit claims and managers approve them.",
  "dependencies": [
    { "kind": "component", "name": "expense-api" },
    { "kind": "platform-resource", "name": "company-sso", "resourceType": "idp-app" }
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
    { "kind": "platform-resource", "name": "expenses-db", "resourceType": "postgres-cnpg" },
    { "kind": "platform-resource", "name": "company-sso", "resourceType": "idp-app" }
  ]
}`;

function expenseApiOpenApi(): string {
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
          description: The claims
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

function approvalsFeature(): string {
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
      When Sam approves it with the reason "Client lunch"
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

/** Acme's design over the features designed so far. */
function acmeCatalog(designed: ReadonlySet<string>): ArtifactDraft[] {
  const all = [...designed].sort();
  const out: ArtifactDraft[] = [];
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
        openapi: expenseApiOpenApi(),
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
      source: { kind: "acceptance", path: "specs/validation/acceptance/F2-approvals.feature", content: approvalsFeature() },
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
): ArtifactDraft[] {
  if (designed.length === 0) return [];
  if (projectName === "acme-expenses") return acmeCatalog(new Set(designed.map((f) => f.id)));
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

/** The mock agent's one-line answer to a comment it has no change scripted for: it notes it in the design as said. */
export function replyTo(projectName: string, text: string): string {
  const t = text.toLowerCase();
  if (projectName === "acme-expenses" && /cost cent(re|er)/.test(t)) return "The data model now keys each claim to a cost centre, not a department.";
  return `Changed in the design: "${text.trim()}".`;
}
