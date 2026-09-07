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
import { DeploymentDetailPage } from "../features/projects/components/DeploymentDetailPage";

/**
 * One environment's deployment (ADR-0027). Keyed by ENVIRONMENT rather than a
 * deployment id because the platform keeps no deployment record — a release
 * binding is current state, so an environment has exactly one deployment to
 * show. The page itself rejects a segment that names no environment.
 */
export const Route = createFileRoute("/projects/$projectName/deployments/$environment")({
  component: DeploymentDetailRoute,
});

function DeploymentDetailRoute() {
  const { projectName, environment } = Route.useParams();
  return <DeploymentDetailPage projectName={projectName} environment={environment} />;
}
