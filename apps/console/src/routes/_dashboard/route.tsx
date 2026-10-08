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
import { Dashboard } from "../../features/dashboard/components/Dashboard";
import { PageWithCards } from "../../features/shell/components/PageWithCards";

// The org's base Page, the Dashboard, and the Card drawn over it (the org's
// Settings) as its child route. Pathless, so the Dashboard keeps `/` and
// Settings has `/settings`; the Dashboard stays mounted under the card.
export const Route = createFileRoute("/_dashboard")({
  component: DashboardPage,
});

function DashboardPage() {
  return (
    <PageWithCards>
      <Dashboard />
    </PageWithCards>
  );
}
