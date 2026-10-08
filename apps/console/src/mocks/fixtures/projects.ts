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

import type { components } from "../../generated/aep-api";

type Project = components["schemas"]["Project"];

// The running example every screen is approved against: Acme Corp's expense
// tracker.
export const acmeExpenses: Project = {
  name: "acme-expenses",
  displayName: "Acme Expenses",
  description: "An expense tracker for our staff",
  repoUrl: "https://github.com/acme/acme-expenses.git",
  createdAt: "2026-09-29T09:00:00Z",
};

// Two more for the Projects grid, so it reads as an org with several projects.
export const employeeOnboarding: Project = {
  name: "employee-onboarding",
  displayName: "Employee onboarding",
  description: "Checklists and accounts for new starters",
  repoUrl: "https://github.com/acme/employee-onboarding.git",
  createdAt: "2026-08-12T09:00:00Z",
};

export const triageAgent: Project = {
  name: "triage-agent",
  displayName: "Triage agent",
  description: "Sorts incoming support tickets and drafts first replies",
  repoUrl: "https://github.com/acme/triage-agent.git",
  createdAt: "2026-09-21T09:00:00Z",
};

export const projects: Project[] = [acmeExpenses, employeeOnboarding, triageAgent];
