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
import { ProjectOverview } from "../features/projects/components/ProjectOverview";

// `?chat=open` (#562): arriving straight from project creation. The platform
// has already fired `/start`, so the user lands with the agent chat open and
// the transcript showing their own idea going in — the journey is visibly
// underway rather than waiting on them. AppLayout consumes the signal and
// strips it, so a refresh or a Back does not reopen a panel the user closed.
//
// It is a signal to OPEN A PANEL, never to navigate: nothing on this journey
// moves the user's viewport for them (#522).
export const Route = createFileRoute("/projects/$projectName/")({
  validateSearch: (search: Record<string, unknown>): { chat?: "open" } =>
    search.chat === "open" ? { chat: "open" as const } : {},
  component: ProjectOverviewRoute,
});

function ProjectOverviewRoute() {
  const { projectName } = Route.useParams();
  return <ProjectOverview projectName={projectName} />;
}
