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

import { useRepoProjectNames } from "../projects/api/queries";
import { useIssuesByProject, useRcaReports } from "./api/issues";
import { alertItems, needsPersonCount, type AlertItem } from "./model/issues";

// The Dashboard's Alerts and the count on the rail's logo: the issues that
// need attention across all projects, and the RCA agent's reports. No read
// answers that for the org at once, so every project's issues are asked,
// every ALERTS_POLL_MS: the rail reads this on every page, and each project's
// read walks its repository's issues on GitHub.

const ALERTS_POLL_MS = 120_000;

export interface Alerts {
  /** Null until the projects and their issues have answered. */
  items: AlertItem[] | null;
  /** How many need a person: the rail's count. */
  needsPerson: number;
  /** Projects whose issues could not be read: their attention is unknown. */
  unreadProjects: string[];
  error: string | null;
}

export function useAlerts(): Alerts {
  const projects = useRepoProjectNames(ALERTS_POLL_MS);
  const issues = useIssuesByProject(projects.data ?? [], ALERTS_POLL_MS);
  // The reports are an extra: normally there are none, and a failed read hides nothing that needs a person.
  const reports = useRcaReports(ALERTS_POLL_MS);
  const ready = projects.data !== undefined && !issues.isPending;
  const items = ready ? alertItems(issues.byProject, reports.data ?? []) : null;
  return {
    items,
    needsPerson: items ? needsPersonCount(items) : 0,
    unreadProjects: issues.unread,
    error: projects.error?.message ?? null,
  };
}
