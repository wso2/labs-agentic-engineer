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
 * The design of record's first worked example — Expense Tracker — as the three
 * files the Security page reads it from.
 *
 * It is here rather than in `src/mocks` because it is the SPEC being asserted,
 * not a convenience: the design draws this exact matrix, down to which cells
 * are filled and which handle carries the "used nowhere" warning, and a test
 * that quietly simplified it would stop being evidence that the page reproduces
 * the design. The document is kept as TEXT, in the on-disk form, so the grant
 * toggle's round-trip claim can be made against it.
 */

import type { SecurityReferenceContext } from "@aep/agent-stream";

/** `specs/design/security.json` — the design's Expense Tracker, verbatim. */
export const EXPENSE_TRACKER_TEXT = `{
  "version": 2,
  "permissions": [
    {
      "resource": "claims",
      "component": "expense-api",
      "description": "Expense claims",
      "actions": [
        {
          "handle": "read",
          "ownership": "own",
          "description": "See own claims"
        },
        {
          "handle": "read-all",
          "ownership": "any",
          "description": "See every claim"
        },
        {
          "handle": "submit",
          "ownership": "own",
          "description": "Create and send a claim"
        },
        {
          "handle": "approve",
          "ownership": "any",
          "description": "Approve a submitted claim"
        },
        {
          "handle": "reject",
          "ownership": "any",
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
          "ownership": "any",
          "description": "Monthly totals"
        },
        {
          "handle": "export",
          "ownership": "any",
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
  "screens": [
    {
      "component": "expense-spa",
      "screen": "My Claims",
      "requires": "claims:read"
    },
    {
      "component": "expense-spa",
      "screen": "My account",
      "requires": null
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
 * `reports:export` "declared, used nowhere".
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
  /claims:
    get:
      summary: List claims
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

/** The sibling spec files, as the panel's `references` prop delivers them. */
export const EXPENSE_TRACKER_REFERENCES: SecurityReferenceContext = {
  read(path: string): string | undefined {
    if (path === "specs/design/design.cell") return CELL;
    if (path === "specs/design/components/expense-api/openapi.yaml")
      return EXPENSE_API_OPENAPI;
    return undefined;
  },
};
