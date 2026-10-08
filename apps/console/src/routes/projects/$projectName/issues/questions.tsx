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
import { QuestionsList } from "../../../../features/agent-chat/components/QuestionsList";
import { CardOverlay } from "../../../../features/projects/components/CardOverlay";

// The Questions card over the Issues Page: every question the Issues agent is
// waiting on, answered in one list (ADR-0002). The Issues chat's pointer opens
// it, and a send closes back to the Issues Page. With `?issue=N` it answers
// that issue's own chat instead, and a send closes back to the issue's card.
interface IssuesQuestionsSearch {
  issue?: number;
}

export const Route = createFileRoute("/projects/$projectName/issues/questions")({
  validateSearch: (search: Record<string, unknown>): IssuesQuestionsSearch => {
    const issue = Number(search.issue);
    return Number.isInteger(issue) && issue > 0 ? { issue } : {};
  },
  component: IssuesQuestionsRoute,
});

function IssuesQuestionsRoute() {
  const { projectName } = Route.useParams();
  const { issue } = Route.useSearch();
  return (
    <CardOverlay card="questions" page="issues" fill>
      {issue === undefined ? (
        <QuestionsList projectName={projectName} view="issues" />
      ) : (
        <QuestionsList projectName={projectName} view="issue" issueNumber={issue} />
      )}
    </CardOverlay>
  );
}
