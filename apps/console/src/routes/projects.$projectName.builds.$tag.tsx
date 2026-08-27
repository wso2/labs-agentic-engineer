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

import { createFileRoute, redirect } from "@tanstack/react-router";
import { BuildDetailPage } from "../features/builds/components/BuildDetailPage";

/**
 * One version's build (ADR-0021 §2, §7).
 *
 * `/builds/:tag` and `/builds/:issueNumber` cannot both exist — the router
 * carries one dynamic child per segment — and this segment is now the VERSION.
 * A numeric segment is therefore an old task link (#185 pointed `/tasks/:n`
 * here; ADR-0021 §7 points it back), and is redirected rather than 404'd, so
 * every `/builds/118` in a comment, a bookmark or a GitHub thread still lands
 * on the task it named.
 */
export const Route = createFileRoute("/projects/$projectName/builds/$tag")({
  beforeLoad: ({ params }) => {
    const asNumber = Number(params.tag);
    if (Number.isInteger(asNumber) && asNumber > 0) {
      throw redirect({
        to: "/projects/$projectName/tasks/$issueNumber",
        params: {
          projectName: params.projectName,
          issueNumber: asNumber,
        },
        replace: true,
      });
    }
  },
  component: BuildDetailRoute,
});

function BuildDetailRoute() {
  const { projectName, tag } = Route.useParams();
  return <BuildDetailPage projectName={projectName} tag={tag} />;
}
