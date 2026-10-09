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

import { createRootRoute } from "@tanstack/react-router";
import { ErrorBoundary } from "../components/ErrorBoundary";
import { NotFoundPage } from "../components/NotFoundPage";
import { GatedShell } from "../features/shell/components/GatedShell";

// Everything renders behind the gates (GatedShell: auth, onboarding, AE
// Studio), then the shell: rail, chat panel, and the main outlet.
//
// The app-level boundary is the last one the app owns. The shell's main
// outlet and the chat panel have their own, so what reaches this one is a
// throw in the gates or the shell itself. Once its automatic attempts are
// spent it says so and offers a reload, the one thing that fixes a stale
// bundle after a deploy.
export const Route = createRootRoute({
  // An address the console does not have draws in the shell's main area.
  notFoundComponent: NotFoundPage,
  component: () => (
    <ErrorBoundary
      label="The console"
      exhaustedMessage="Unable to recover. Please contact your administrator."
      offerReload
      fallbackSx={{ minHeight: "100vh", display: "flex", flexDirection: "column", justifyContent: "center" }}
    >
      <GatedShell />
    </ErrorBoundary>
  ),
});
