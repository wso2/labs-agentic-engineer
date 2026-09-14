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
// `?generate=design` (#159): arriving from a "Generate/Re-generate design" CTA
// — AppLayout opens the agent panel and auto-sends the design turn.
//
// The requirements half of that signal is GONE (#562). The platform fires
// `/start` itself at project creation, so the console no longer has a
// generate-requirements moment to hand across a navigation; the one CTA that
// still starts an interview seeds the chat directly, from wherever the user is.
//
// `?view=architecture`: land on the Architecture tab instead of the workspace's
// default file. The overview's architecture panel links here — it offers the
// link BECAUSE it is showing a diagram, so dropping the reader on the PRD would
// make them hunt the rail for the thing they just clicked.
//
// Unlike `generate`, this is not stripped after use: it names WHICH view, so it
// stays a shareable deep link. A manual rail click still wins for the session.
export const Route = createFileRoute("/projects/$projectName_/spec")({
  validateSearch: (
    search: Record<string, unknown>,
  ): { generate?: "design"; view?: "architecture"; file?: string } => ({
    ...(search.generate === "design" ? { generate: "design" as const } : {}),
    ...(search.view === "architecture" ? { view: "architecture" as const } : {}),
    // `?file=specs/…` — a link from the chat into one document (ADR-0028: the
    // design turn's closing list links each open dependency's definition).
    // Stripped once followed, like `generate`: it names a moment, not a view.
    ...(typeof search.file === "string" && search.file.startsWith("specs/") ? { file: search.file } : {}),
  }),
  component: SpecRoute,
});

function SpecRoute() {
  const { projectName } = Route.useParams();
  return <SpecView projectName={projectName} />;
}
