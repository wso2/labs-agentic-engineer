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
import { BuildsPage } from "../features/builds/components/BuildsPage";

// ?tag=vN selects a previous build; absent = the newest build (#185). The
// param is validated as "a non-empty string" only — an unknown tag falls
// back to newest inside BuildsPage rather than erroring the route.
export function validateBuildsSearch(search: Record<string, unknown>): {
  tag?: string;
  connections?: "open";
} {
  const tag = search.tag;
  return {
    ...(typeof tag === "string" && tag !== "" ? { tag } : {}),
    ...(search.connections === "open" ? { connections: "open" as const } : {}),
  };
}

export const Route = createFileRoute("/projects/$projectName/builds/")({
  validateSearch: validateBuildsSearch,
  component: BuildsRoute,
});

function BuildsRoute() {
  const { projectName } = Route.useParams();
  const { tag, connections } = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <BuildsPage
      projectName={projectName}
      tag={tag}
      connectionsOpen={connections === "open"}
      onTagChange={(next) =>
        void navigate({
          search: {
            ...(next ? { tag: next } : {}),
            ...(connections === "open" ? { connections: "open" as const } : {}),
          },
          replace: true,
        })
      }
    />
  );
}
