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
import { BuildsLedger } from "../features/builds/components/BuildsLedger";

// The version ledger (ADR-0021 §1). One version's story is its own page now.
//
// `?tag=vN` used to select which version this page told the story of. That
// surface moved, so an old link carrying the search param is redirected to the
// version's page rather than silently dropping the tag and landing the reader
// on a list — the link named a version, and it still resolves to one.
export const Route = createFileRoute("/projects/$projectName/builds/")({
  validateSearch: (search: Record<string, unknown>): { tag?: string } => {
    const tag = search.tag;
    return typeof tag === "string" && tag !== "" ? { tag } : {};
  },
  beforeLoad: ({ params, search }) => {
    if (search.tag) {
      throw redirect({
        to: "/projects/$projectName/builds/$tag",
        params: { projectName: params.projectName, tag: search.tag },
        replace: true,
      });
    }
  },
  component: BuildsRoute,
});

function BuildsRoute() {
  const { projectName } = Route.useParams();
  return <BuildsLedger projectName={projectName} />;
}
