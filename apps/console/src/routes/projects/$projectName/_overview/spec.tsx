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
import { SpecWorkspace } from "../../../../features/spec/components/SpecWorkspace";

/** The open file (`F2`, `product-wide`, a document's ID; none is the product page) and a line or place in it. */
interface SpecSearch {
  file?: string;
  at?: string;
}

// The spec card, drawn over the project overview.
export const Route = createFileRoute("/projects/$projectName/_overview/spec")({
  validateSearch: (search: Record<string, unknown>): SpecSearch => ({
    ...(typeof search.file === "string" && search.file ? { file: search.file } : {}),
    ...(typeof search.at === "string" && search.at ? { at: search.at } : {}),
  }),
  component: SpecRoute,
});

function SpecRoute() {
  const { projectName } = Route.useParams();
  const { file, at } = Route.useSearch();
  return (
    <CardOverlay card="spec" fill>
      <SpecWorkspace projectName={projectName} file={file} at={at} />
    </CardOverlay>
  );
}
