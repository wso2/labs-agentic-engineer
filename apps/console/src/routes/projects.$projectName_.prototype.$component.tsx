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

import { createFileRoute } from "@tanstack/react-router";
import { PrototypePage } from "../features/spec/components/PrototypePage";

// `$projectName_` (trailing underscore) un-nests this route from the
// /projects/$projectName layout, same trick as `projects.$projectName_.spec`
// — the prototype is a full-screen workspace without the shared project
// header (#348).
//
// `?screen=<Name>` deep-links to a specific screen of the component's
// prototype (e.g. a link shared mid-review), and `?flow=<Name>` deep-links
// the persona/flow alongside it. `PrototypeView` drives screen and flow
// navigation internally and calls back on every change via `onScreenChange`
// / `onFlowChange`; this route syncs both back into the URL with a REPLACE
// navigation (not push), so clicking through screens doesn't pile up history
// entries — the browser back button leaves the prototype rather than
// stepping screen by screen. Both params are merged onto the previous search
// on every sync so changing one never drops the other.
export const Route = createFileRoute(
  "/projects/$projectName_/prototype/$component",
)({
  validateSearch: (search: Record<string, unknown>): { screen?: string; flow?: string } => ({
    ...(typeof search.screen === "string" && search.screen
      ? { screen: search.screen }
      : {}),
    ...(typeof search.flow === "string" && search.flow ? { flow: search.flow } : {}),
  }),
  component: PrototypeRoute,
});

function PrototypeRoute() {
  const { projectName, component } = Route.useParams();
  const { screen, flow } = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <PrototypePage
      projectName={projectName}
      component={component}
      // Both params are preserved on every sync: a shared link restores the
      // persona AND the screen, and changing one must not drop the other.
      onScreenChange={(s) =>
        void navigate({ search: (prev) => ({ ...prev, screen: s }), replace: true })
      }
      onFlowChange={(f) =>
        void navigate({ search: (prev) => ({ ...prev, flow: f }), replace: true })
      }
      {...(screen ? { screen } : {})}
      {...(flow ? { flow } : {})}
    />
  );
}
