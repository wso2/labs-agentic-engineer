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
import { SpecView } from "../features/spec/components/SpecView";

// `$projectName_` (trailing underscore) un-nests this route from the
// /projects/$projectName layout: the spec view is a full-screen workspace
// without the shared project header (#80).
//
// `?import=requirements` (ADR-0020): arriving from the onboard create path —
// open the import dialog immediately; no `/start` kickoff ran for this project.
//
// `?generate=design` (#159): arriving from a "Generate/Re-generate design" CTA
export const Route = createFileRoute("/projects/$projectName_/spec")({
  validateSearch: (
    search: Record<string, unknown>,
  ): { generate?: "design"; view?: "architecture"; import?: "requirements" } => ({
    ...(search.generate === "design" ? { generate: "design" as const } : {}),
    ...(search.view === "architecture" ? { view: "architecture" as const } : {}),
    ...(search.import === "requirements" ? { import: "requirements" as const } : {}),
  }),
  component: SpecRoute,
});

function SpecRoute() {
  const { projectName } = Route.useParams();
  const { import: importMode } = Route.useSearch();
  return (
    <SpecView
      projectName={projectName}
      openImportOnMount={importMode === "requirements"}
    />
  );
}
