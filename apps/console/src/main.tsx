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

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider } from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { CssBaseline, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { AppAuthProvider } from "./auth/AuthProvider";
import { aepTheme } from "@aep/ui-theme";
import { router } from "./router";

// Retry 3 for queries (TanStack default, made explicit), no automatic retry
// for mutations; per-query staleTime is set at the hook.
const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 3 },
    mutations: { retry: 0 },
  },
});

// Mock mode (VITE_API_MODE=mock) runs the whole app on MSW handlers and
// fixtures: every screen is built and approved on it before it is wired to
// aep-api. Dev-only and dynamic-import-guarded: `import.meta.env.DEV` is
// statically false in production builds, so the msw chunk is eliminated.
async function enableMocking(): Promise<void> {
  if (!import.meta.env.DEV || import.meta.env.VITE_API_MODE !== "mock") {
    return;
  }
  const { worker } = await import("./mocks/browser");
  await worker.start({ onUnhandledRequest: "bypass" });
}

void enableMocking().then(() => {
  createRoot(document.getElementById("app")!).render(
    <StrictMode>
      {/* Auth outermost: the OIDC session exists before any UI. */}
      <AppAuthProvider>
        <OxygenUIThemeProvider theme={aepTheme}>
          <CssBaseline />
          <QueryClientProvider client={queryClient}>
            <RouterProvider router={router} />
          </QueryClientProvider>
        </OxygenUIThemeProvider>
      </AppAuthProvider>
    </StrictMode>,
  );
});
