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
import { BuildHistory } from "../../../../features/builds/components/BuildHistory";
import { PageWithCards } from "../../../../features/shell/components/PageWithCards";

// Build history, the project's ledger of versions, and the Card it lists (a
// version's Build) as its child route.
export const Route = createFileRoute("/projects/$projectName/builds")({
  component: BuildsRoute,
});

function BuildsRoute() {
  const { projectName } = Route.useParams();
  return (
    <PageWithCards>
      <BuildHistory projectName={projectName} />
    </PageWithCards>
  );
}
