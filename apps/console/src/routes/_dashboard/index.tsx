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

import { createFileRoute, Navigate } from "@tanstack/react-router";
import { useProjects } from "../../features/projects/api/queries";

// The Dashboard's own address, with no card open: where sign-in lands. The
// layout route above draws the page; this claims the address, and sends an
// organization with no projects yet to New project instead, since there is
// nothing on the Dashboard for it until something is built.
export const Route = createFileRoute("/_dashboard/")({
  component: DashboardHome,
});

function DashboardHome() {
  const projects = useProjects();
  if (projects.data && projects.data.length === 0) return <Navigate to="/projects/new" replace />;
  return null;
}
