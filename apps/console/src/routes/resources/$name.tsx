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
import { ResourceCard } from "../../features/resources/components/ResourceCard";
import { resourceSearch, type ResourceSearch } from "../../features/resources/model/resources";

// A Resource card, drawn over the Resources Page. Names collide across kinds
// and projects, so the search names the kind and project where the name
// alone is not enough (model/resources.ts, `resourceAddress`).
export const Route = createFileRoute("/resources/$name")({
  validateSearch: (search: Record<string, unknown>): ResourceSearch => resourceSearch(search),
  component: ResourceRoute,
});

function ResourceRoute() {
  const { name } = Route.useParams();
  const search = Route.useSearch();
  return <ResourceCard name={name} search={search} />;
}
