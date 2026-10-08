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
import { SettingsCard } from "../../features/settings/components/SettingsCard";
import { sectionFromSearch, type SettingsSection } from "../../features/settings/settingsSection";

interface SettingsSearch {
  section?: SettingsSection;
}

// The org's Settings card, drawn over the Dashboard. `?section=` names the
// section open in it; an unknown or missing one reads as the first.
export const Route = createFileRoute("/_dashboard/settings")({
  validateSearch: (search: Record<string, unknown>): SettingsSearch =>
    search.section === undefined ? {} : { section: sectionFromSearch(search.section) },
  component: SettingsRoute,
});

function SettingsRoute() {
  const { section } = Route.useSearch();
  return <SettingsCard section={sectionFromSearch(section)} />;
}
