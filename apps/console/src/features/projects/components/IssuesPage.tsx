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

import { Link, useNavigate } from "@tanstack/react-router";
import { useHasPermission } from "../../../auth/permissions";
import { PageHeader } from "../../../components/PageHeader";
import { PermissionRestrictedPage } from "../../../components/PermissionRestrictedPage";
import { IssuesList } from "../../issues/components/IssuesList";
import { useProject } from "../api/queries";

export function IssuesPage({ projectName }: { projectName: string }) {
  // Exact-match ae:build-view, matching list-issues' own gate
  // (permission_gate.go) and the Issues sidebar leg that reaches here. NOT
  // OR'd with ae:build: that permission authorizes the writes on this surface
  // — filing an issue, promoting one into a task, both of which start a run —
  // and never page entry on its own.
  const canViewIssues = useHasPermission("ae:build-view");
  const navigate = useNavigate();
  const project = useProject(projectName);

  if (!canViewIssues) {
    return (
      <PermissionRestrictedPage
        title="You don't have access to this project's issues"
        description="Issues raised against this project, and the incidents behind them, are restricted for your role. Ask a project admin to grant access."
        backLabel="Back to project overview"
        onBack={() =>
          void navigate({ to: "/projects/$projectName", params: { projectName } })
        }
      />
    );
  }

  return (
    <>
      <PageHeader
        title="Issues"
        {...(project.data && {
          subtitle: project.data.displayName ?? project.data.name,
        })}
        backTo={{
          link: <Link to="/projects/$projectName" params={{ projectName }} />,
          label: "Back to Overview",
        }}
      />
      <IssuesList projectName={projectName} />
    </>
  );
}
