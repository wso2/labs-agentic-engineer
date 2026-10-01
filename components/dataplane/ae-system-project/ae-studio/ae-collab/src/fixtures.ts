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

import { type SpecFile } from "./bff.js";

// Dev-mode seed content. Mirrors the console mock layer's demo-shop project
// (apps/console/src/mocks/fixtures/project.ts) so `make dev` shows the same
// spec in both the mocked REST reads and the live collab doc.

/** A spec file as the Files API serves it: repo-relative path under specs/. */
export interface RepoSpecFile {
  path: string;
  content: string;
}

export const devSpecFiles: RepoSpecFile[] = [
  {
    path: "specs/requirements/prd.md",
    content: `# Demo Shop — PRD

## Goal

A small storefront where customers browse the product catalog, add items
to a cart, and check out.

## Requirements

- Browse products by category with search.
- Cart persists across sessions.
- Checkout with a mocked payment provider.
- Order history per customer.
`,
  },
  {
    path: "specs/requirements/user-stories.md",
    content: `# Demo Shop — User stories

- As a shopper, I can search the catalog so that I find products quickly.
- As a shopper, my cart survives a page reload so that I don't lose picks.
- As a shopper, I can check out and see an order confirmation.
- As a returning customer, I can see my past orders.
`,
  },
  {
    path: "specs/design/architecture.md",
    content: `# Demo Shop — Component architecture

Three components behind the project cell:

| Component | Type | Responsibility |
|---|---|---|
| storefront | webapp | Customer-facing UI |
| catalog-api | service | Product catalog CRUD + search |
| orders-api | service | Cart, checkout, order history |

The storefront talks to both services; services share nothing.
`,
  },
];

/** The same files as SpecFile records — full specs/ paths (verbatim doc keys). */
export const devSeedFiles: SpecFile[] = devSpecFiles.map((f) => ({
  path: f.path,
  content: f.content,
  sha: `dev-${f.path}`,
}));
