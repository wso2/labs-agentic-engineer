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

import acmeF1 from "@aep/contracts/requirements/acme-expenses/features/F1-submit-expenses.md?raw";
import acmeF2 from "@aep/contracts/requirements/acme-expenses/features/F2-approvals.md?raw";
import acmeF3 from "@aep/contracts/requirements/acme-expenses/features/F3-payroll-export.md?raw";
import acmeF4 from "@aep/contracts/requirements/acme-expenses/features/F4-spending-reports.md?raw";
import acmeF5 from "@aep/contracts/requirements/acme-expenses/features/F5-mileage-claims.md?raw";
import acmePrd from "@aep/contracts/requirements/acme-expenses/prd.md?raw";
import acmeProductWide from "@aep/contracts/requirements/acme-expenses/product-wide.md?raw";
import type { SpecModel } from "../../features/spec/api/specModel";

/** The mock's spec: the model the platform's state and the live documents add up to, plus the files the room would hold. */
export type MockSpecModel = SpecModel & { files: Record<string, string> };

// PROVISIONAL — mock-only until the spec model is wired; see
// features/spec/api/specModel.ts.
//
// Acme Expenses three weeks in: three features interviewed, two stubs. Its
// files ARE the shared requirements fixture both readers are held to
// (packages/contracts/requirements/acme-expenses, skills/prd-contract), so the
// screens are approved on the same documents aep-api's parser is tested on.
// F2.3 moved to Mileage claims and now reads "F5.1 (was F2.3)"; Approvals'
// Retired section records it. Payroll export's interview waits on a question
// in its Open Questions, tagged `*blocking*`, with the two answers the agent
// offers nested under it.

const featurePath = (file: string) => `specs/requirements/features/${file}.md`;

export const acmeExpensesSpec: MockSpecModel = {
  features: [
    {
      id: "F1",
      name: "Submit expenses",
      path: featurePath("F1-submit-expenses"),
      purpose: "Employees record expenses with a receipt photo and submit them as a claim.",
      stage: "Interviewed",
    },
    {
      id: "F2",
      name: "Approvals",
      path: featurePath("F2-approvals"),
      purpose: "Managers approve or reject their team's claims; large claims also go to Finance.",
      stage: "Interviewed",
    },
    {
      id: "F3",
      name: "Payroll export",
      path: featurePath("F3-payroll-export"),
      purpose: "Finance sends approved claims to Xero every night and fixes the ones that fail.",
      stage: "Interviewed",
    },
    {
      id: "F4",
      name: "Spending reports",
      path: featurePath("F4-spending-reports"),
      purpose: "Finance sees where the money goes, by team and category.",
      stage: "Not interviewed",
    },
    {
      id: "F5",
      name: "Mileage claims",
      path: featurePath("F5-mileage-claims"),
      purpose: "Staff claim for driving to client sites.",
      stage: "Not interviewed",
    },
  ],
  documents: [
    {
      id: "tne-policy",
      title: "Acme T&E Policy v3.pdf",
      pages: 12,
      rows: [
        { page: "p.2", says: "Meals are capped at $50 per day.", landedIn: "F1 Submit expenses, a decision" },
        { page: "p.4", says: "A receipt is required for any expense above $25.", landedIn: "F1 Submit expenses, a decision" },
        { page: "p.7", says: "Claims over $1,000 need a second approval from Finance.", landedIn: "F2.5" },
        { page: "p.8", says: "A mileage claim shows the route driven.", landedIn: "F2.3" },
        { page: "p.9", says: "Approved claims are synced to Xero every night.", landedIn: "F3.1" },
        { page: "p.10", says: "Expense records are kept for 7 years.", landedIn: "P2" },
        { page: "p.11–12", says: "Travel booking rules.", landedIn: null },
      ],
    },
  ],
  design: { designedFrom: {}, openComments: 0, specChanges: [] },
  files: {
    "specs/requirements/prd.md": acmePrd,
    [featurePath("F1-submit-expenses")]: acmeF1,
    [featurePath("F2-approvals")]: acmeF2,
    [featurePath("F3-payroll-export")]: acmeF3,
    [featurePath("F4-spending-reports")]: acmeF4,
    [featurePath("F5-mileage-claims")]: acmeF5,
    "specs/requirements/product-wide.md": acmeProductWide,
  },
};

// A small product: two features, so its product page is the feature list alone.

const triagePrd = `# Triage agent

## Problem Statement

Support gets 300 tickets a day, and the urgent ones wait in the same queue as the rest.

## Solution

An agent that reads each incoming ticket, sorts it by urgency, and drafts a first reply for a person to send.

## Actors

- Support agent: reviews the sorted queue and sends replies.

## Features

- F1 [Classify tickets](features/F1-classify-tickets.md)
- F2 [Draft replies](features/F2-draft-replies.md)

## Out of Scope

- Sending replies without a person approving them.
`;

const triageF1 = `# Classify tickets

## Purpose

Every incoming ticket is sorted by urgency before anyone reads it.

## User Stories

- F1.1 As a support agent, I see new tickets sorted as urgent, normal or low.
- F1.2 As a support agent, I change a ticket's urgency when the agent got it wrong.

## Decisions

- A ticket that mentions an outage is always urgent.
`;

const triageF2 = `# Draft replies

## Purpose

The agent drafts a first reply for each ticket, for a person to send.

## User Stories

- F2.1 As a support agent, I open a ticket and find a drafted reply ready to edit.
- F2.2 As a support agent, I send the draft as it is, or after editing it.

## Decisions

- A draft is never sent without a person pressing Send, as F1.2's corrections are.
`;

const triageProductWide = `# Product-wide

Rules that apply to more than one feature.

## Requirements

- P1 Staff sign in with company SSO. [org default] Applies to: all.
`;

export const triageAgentSpec: MockSpecModel = {
  features: [
    {
      id: "F1",
      name: "Classify tickets",
      path: featurePath("F1-classify-tickets"),
      purpose: "Every incoming ticket is sorted by urgency before anyone reads it.",
      stage: "Interviewed",
    },
    {
      id: "F2",
      name: "Draft replies",
      path: featurePath("F2-draft-replies"),
      purpose: "The agent drafts a first reply for each ticket, for a person to send.",
      stage: "Interviewed",
    },
  ],
  documents: [],
  design: { designedFrom: {}, openComments: 0, specChanges: [] },
  files: {
    "specs/requirements/prd.md": triagePrd,
    [featurePath("F1-classify-tickets")]: triageF1,
    [featurePath("F2-draft-replies")]: triageF2,
    "specs/requirements/product-wide.md": triageProductWide,
  },
};

/** Any other project, a new one included: the kickoff has not written features yet. */
export function freshSpec(productName: string): MockSpecModel {
  return {
    features: [],
    documents: [],
    design: { designedFrom: {}, openComments: 0, specChanges: [] },
    files: {
      "specs/requirements/prd.md": `# ${productName}\n\n## Features\n\nThe agent proposes features once it has read your brief.\n`,
      "specs/requirements/product-wide.md": "# Product-wide\n\nRules that apply to more than one feature.\n",
    },
  };
}
