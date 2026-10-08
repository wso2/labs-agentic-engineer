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
import { DesignActions, DesignWorkspace } from "../../../../features/design/components/DesignWorkspace";
import { CardOverlay } from "../../../../features/projects/components/CardOverlay";

/** The open artifact, or a dependency (`dep-F3`); none opens the first artifact. */
interface DesignSearch {
  art?: string;
}

// The design card, drawn over the project overview: the design review.
export const Route = createFileRoute("/projects/$projectName/_overview/design")({
  validateSearch: (search: Record<string, unknown>): DesignSearch =>
    typeof search.art === "string" && search.art ? { art: search.art } : {},
  component: DesignRoute,
});

function DesignRoute() {
  const { projectName } = Route.useParams();
  const { art } = Route.useSearch();
  return (
    <CardOverlay card="design" fill actions={<DesignActions projectName={projectName} />}>
      <DesignWorkspace projectName={projectName} art={art} />
    </CardOverlay>
  );
}
