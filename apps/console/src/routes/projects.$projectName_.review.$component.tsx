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
import { ComponentPrototypePage } from "../features/prototype/components/ComponentPrototypePage";
import {
  PROTOTYPE_MODES,
  type PrototypeMode,
  type PrototypeViewRequest,
} from "../features/prototype/model/viewState";

// The prototype review page for one web-application component (#813), read
// from its prototype.json. `$projectName_` un-nests it from the project
// layout, and AppLayout drops the console chrome for it: the page takes over
// the whole viewport.
//
// Screen, flow, display state, mode and role ride the URL so a link opens exactly
// what the reviewer saw. The page reports the WHOLE view on every change and
// the route writes it with a REPLACE navigation: clicking through screens
// piles up no history, so Back leaves the prototype, and no callback can drop
// a param another one set. An unknown mode, and an empty or non-string param,
// is dropped; unknown IDs (a role included) are the page's to repair against
// the model.
function nonEmpty(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

export function validatePrototypeSearch(search: Record<string, unknown>): PrototypeViewRequest {
  const screen = nonEmpty(search.screen);
  const flow = nonEmpty(search.flow);
  const state = nonEmpty(search.state);
  const mode = PROTOTYPE_MODES.find((m) => m === search.mode);
  const role = nonEmpty(search.role);
  return {
    ...(screen ? { screen } : {}),
    ...(flow ? { flow } : {}),
    ...(state ? { state } : {}),
    ...(mode ? { mode: mode satisfies PrototypeMode } : {}),
    ...(role ? { role } : {}),
  };
}

export const Route = createFileRoute("/projects/$projectName_/review/$component")({
  validateSearch: validatePrototypeSearch,
  component: PrototypeRoute,
});

function PrototypeRoute() {
  const { projectName, component } = Route.useParams();
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <ComponentPrototypePage
      projectName={projectName}
      component={component}
      search={search}
      onSearchChange={(next) =>
        void navigate({
          search: (prev) =>
            validatePrototypeSearch({
              ...prev,
              screen: undefined,
              flow: undefined,
              state: undefined,
              mode: undefined,
              role: undefined,
              ...next,
            }),
          replace: true,
        })
      }
    />
  );
}
