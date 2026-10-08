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

import { createRouter } from "@tanstack/react-router";
import { routeTree } from "./generated/routeTree.gen";

// Module-scoped so non-component code (the OIDC sign-in callback) can
// navigate without a hook.
// An unknown address is the root's to answer (its not-found Page, in the
// shell's main area), never a project's: `/projects/acme-expenses/tasks/14`
// would otherwise land on that project's overview with nothing said.
export const router = createRouter({ routeTree, notFoundMode: "root" });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
