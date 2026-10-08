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
import { CardOverlay } from "../../../../features/projects/components/CardOverlay";
import { PrototypeWorkspace } from "../../../../features/prototype/components/PrototypeWorkspace";

/** The web application whose prototype is open full screen. */
interface PrototypeSearch {
  review?: string;
}

// The prototype card, beside Spec and Design: each web application's
// prototype, and the full-screen review of one (`?review=expense-web`).
export const Route = createFileRoute("/projects/$projectName/_overview/prototype")({
  validateSearch: (search: Record<string, unknown>): PrototypeSearch =>
    typeof search.review === "string" && search.review ? { review: search.review } : {},
  component: PrototypeRoute,
});

function PrototypeRoute() {
  const { projectName } = Route.useParams();
  const { review } = Route.useSearch();
  const navigate = Route.useNavigate();
  return (
    <CardOverlay card="prototype" fill>
      <PrototypeWorkspace
        projectName={projectName}
        review={review}
        onReview={(component) => void navigate({ search: component ? { review: component } : {} })}
      />
    </CardOverlay>
  );
}
