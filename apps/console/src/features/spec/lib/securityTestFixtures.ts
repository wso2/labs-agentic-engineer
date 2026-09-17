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
 * Expense Tracker — the worked example the Security page was designed against —
 * as the files the page reads it from.
 *
 * It is here rather than in `src/mocks` because it is the SPEC being asserted,
 * not a convenience: the example fixes this exact matrix, down to which cells
 * are filled and which handle carries the "used nowhere" warning, so a test
 * that quietly simplified it would stop being evidence. The document is kept as
 * TEXT, in the on-disk form, so the grant toggle's round-trip claim can be made
 * against it.
 */

import type { SecurityReferenceContext } from "@aep/agent-stream";

/** `specs/design/security.json` — the design's Expense Tracker, verbatim. */
export const EXPENSE_TRACKER_TEXT = `{
  "version": 3,
  "permissions": [
    {
      "resource": "claims",
      "component": "expense-api",
      "description": "Expense claims",
      "actions": [
        {
          "handle": "read",
          "description": "See own claims"
        },
        {
          "handle": "read-all",
          "description": "See every claim"
        },
        {
          "handle": "submit",
          "description": "Create and send a claim"
        },
        {
          "handle": "approve",
          "description": "Approve a submitted claim"
        },
        {
          "handle": "reject",
          "description": "Reject a submitted claim"
        }
      ]
    },
    {
      "resource": "reports",
      "component": "expense-api",
      "actions": [
        {
          "handle": "read",
          "description": "Monthly totals"
        },
        {
          "handle": "export",
          "description": "Download CSV"
        }
      ]
    }
  ],
  "groups": [
    {
      "name": "Employees",
      "description": "Everyone who can claim expenses"
    }
  ],
  "roles": [
    {
      "name": "Employee",
      "description": "Submits and follows their own claims",
      "stories": [
        1,
        2
      ],
      "grants": [
        "claims:read",
        "claims:submit"
      ],
      "assignTo": [
        "Employees"
      ]
    },
    {
      "name": "Approver",
      "description": "Approves or rejects submitted claims",
      "stories": [
        3
      ],
      "grants": [
        "claims:read",
        "claims:read-all",
        "claims:approve",
        "claims:reject",
        "reports:read"
      ],
      "assignTo": [
        "Finance"
      ],
      "assignableBy": [
        "Approver"
      ]
    }
  ],
  "testUsers": [
    {
      "username": "test-employee",
      "roles": [
        "Employee"
      ]
    },
    {
      "username": "test-approver",
      "roles": [
        "Approver"
      ]
    }
  ]
}
`;

/** `specs/design/design.cell` — the two components the document names. */
const CELL = `cell expense-tracker {
  component expense-api service
  component expense-spa web-application
}
`;

/**
 * `specs/design/components/expense-api/openapi.yaml`, cut to what the Security
 * page reads: every operation's protection. `/health` is open, `/me` inherits
 * the document default, and the rest name one handle each — which is what makes
 * `reports:export` "declared, used nowhere". The caller's claims live under
 * `/me/claims`; everything else reaches every row (ADR-0031), and the chip
 * beside each handle on the page is read off exactly that.
 */
const EXPENSE_API_OPENAPI = `openapi: 3.0.3
info:
  title: Expense API
  version: 1.0.0
components:
  securitySchemes:
    oauth2:
      type: oauth2
      flows: {}
security:
  - oauth2: []
paths:
  /health:
    get:
      summary: Liveness
      security: []
      responses:
        "200":
          description: ok
  /me:
    get:
      summary: The signed-in account
      responses:
        "200":
          description: ok
  /me/claims:
    get:
      summary: The caller's claims
      security:
        - oauth2: ["claims:read"]
      responses:
        "200":
          description: ok
    post:
      summary: Submit a claim
      security:
        - oauth2: ["claims:submit"]
      responses:
        "201":
          description: created
  /claims:
    get:
      summary: Every claim
      security:
        - oauth2: ["claims:read-all"]
      responses:
        "200":
          description: ok
  /claims/{claimId}/approve:
    post:
      summary: Approve a claim
      security:
        - oauth2: ["claims:approve"]
      responses:
        "200":
          description: ok
  /claims/{claimId}/reject:
    post:
      summary: Reject a claim
      security:
        - oauth2: ["claims:reject"]
      responses:
        "200":
          description: ok
  /reports/monthly:
    get:
      summary: Monthly totals
      security:
        - oauth2: ["reports:read"]
      responses:
        "200":
          description: ok
`;

/**
 * `specs/requirements/prd.md`, cut to its Actors section — the only part the
 * page reads. The two actors are the two roles, which is what a reader is
 * checking when they look at the line above the role cards.
 */
const EXPENSE_TRACKER_PRD = `# Expense Tracker

## Actors

- **Employee** — submits claims and follows their own.
- **Approver** — approves or rejects submitted claims.

## User Stories

1. As an Employee, I want to submit a claim.
`;

/** The sibling spec files, as the panel's `references` prop delivers them. */
export const EXPENSE_TRACKER_REFERENCES: SecurityReferenceContext = {
  read(path: string): string | undefined {
    if (path === "specs/design/design.cell") return CELL;
    if (path === "specs/design/components/expense-api/openapi.yaml")
      return EXPENSE_API_OPENAPI;
    if (path === "specs/requirements/prd.md") return EXPENSE_TRACKER_PRD;
    return undefined;
  },
};
