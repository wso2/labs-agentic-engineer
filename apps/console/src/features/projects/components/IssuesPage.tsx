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

import { Link } from "@tanstack/react-router";
import { PageHeader } from "../../../components/PageHeader";
import { IssuesList } from "../../issues/components/IssuesList";
import { useProject } from "../api/queries";

export function IssuesPage({ projectName }: { projectName: string }) {
  const project = useProject(projectName);

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
