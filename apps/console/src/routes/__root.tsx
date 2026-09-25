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
import { AppLayout } from "../layouts/AppLayout";
import { AuthGuard } from "../auth/AuthGuard";
import { OnboardingGate } from "../features/onboarding/components/OnboardingGate";
import { ErrorBoundary } from "../components/ErrorBoundary";

// Everything renders behind the auth gate (issue #91): routes only ever
// see a signed-in session. Behind it, the onboarding gate (issue #102,
// ADR-0009) holds every route until the org's config is complete.
//
// The app-level boundary is the last one the console owns. The shell's page
// outlet and the chat panel have their own (AppLayout), so what reaches this
// one is a throw in the gates or the shell itself, where nothing survives
// anyway. It replaces the router's bare "Show Error" page with the same
// retry-then-explain fallback the sections use; once the automatic attempts
// are spent it says so and offers a reload, the one thing that fixes a
// stale bundle after a deploy.
export const Route = createRootRoute({
  component: () => (
    <ErrorBoundary
      label="The console"
      exhaustedMessage="Unable to recover. Please contact your administrator."
      offerReload
      fallbackSx={{ minHeight: "100vh", display: "flex", flexDirection: "column", justifyContent: "center" }}
    >
      <AuthGuard>
        <OnboardingGate>
          <AppLayout />
        </OnboardingGate>
      </AuthGuard>
    </ErrorBoundary>
  ),
});
