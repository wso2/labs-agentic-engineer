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

import type { ReactNode } from "react";
import { createLink } from "@tanstack/react-router";
import { Alert, Box, Button, ButtonBase, Chip, Skeleton, Typography } from "@wso2/oxygen-ui";
import { projectLabel, useProject } from "../../projects/api/queries";
import { useProjectIssues } from "../api/issues";
import {
  attentionLabel,
  attentionNeedsPerson,
  issueOrigin,
  issueSections,
  issueStateLabel,
  type IssueInfo,
  type OriginKind,
} from "../model/issues";

// The Issues Page: the project's GitHub issues, the incidents the SRE agent
// filed, the platform's own and the ones people filed, after the old
// console's IssuesPage. Those that need attention come first, flagged with
// why; then the open ones, then the closed. An issue opens its Issue card
// over this page.

const RowLink = createLink(ButtonBase);

export const ORIGIN_LABEL: Record<OriginKind, string> = {
  incident: "Incident",
  platform: "Platform",
  person: "Person",
};

function IssueRow({ projectName, issue }: { projectName: string; issue: IssueInfo }) {
  const reason = issue.attentionReason;
  return (
    <RowLink
      to="/projects/$projectName/issues/$number"
      params={{ projectName, number: String(issue.Number) }}
      sx={{
        display: "flex",
        alignItems: "baseline",
        gap: 1.5,
        justifyContent: "stretch",
        textAlign: "start",
        px: 1.75,
        py: 1.25,
        "& + &": { borderTop: 1, borderColor: "divider" },
        "&:hover": { bgcolor: "action.hover" },
      }}
    >
      <Typography component="span" variant="caption" color="text.secondary" sx={{ fontFamily: "monospace", minWidth: 40 }}>
        #{issue.Number}
      </Typography>
      <Box component="span" sx={{ flex: 1, minWidth: 0, display: "flex", flexDirection: "column", gap: 0.25 }}>
        <Typography component="span" variant="body2" sx={{ fontWeight: 600 }}>
          {issue.Title}
        </Typography>
        <Typography component="span" variant="caption" color="text.secondary">
          {ORIGIN_LABEL[issueOrigin(issue.Labels).kind]} · {issueStateLabel(issue)}
        </Typography>
      </Box>
      {reason && (
        <Chip
          size="small"
          variant="outlined"
          color={attentionNeedsPerson(reason) ? "warning" : "default"}
          label={attentionLabel(reason)}
        />
      )}
    </RowLink>
  );
}

function Section({ title, projectName, issues }: { title: string; projectName: string; issues: IssueInfo[] }) {
  if (issues.length === 0) return null;
  return (
    <Box component="section" aria-label={title} sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
      <Typography component="h2" sx={{ fontSize: "0.8125rem", fontWeight: 600 }}>
        {title}
      </Typography>
      <Box sx={{ border: 1, borderColor: "divider", borderRadius: 2.5, overflow: "hidden" }}>
        {issues.map((issue) => (
          <IssueRow key={issue.Number} projectName={projectName} issue={issue} />
        ))}
      </Box>
    </Box>
  );
}

export function IssuesPage({ projectName }: { projectName: string }) {
  const project = useProject(projectName);
  const issues = useProjectIssues(projectName);

  let body: ReactNode;
  if (issues.isError) {
    body = (
      <Alert severity="error" action={<Button onClick={() => void issues.refetch()}>Retry</Button>}>
        {issues.error.message}
      </Alert>
    );
  } else if (!issues.data) {
    body = <Skeleton variant="rounded" height={160} />;
  } else if (issues.data.length === 0) {
    body = (
      <Typography variant="body2" color="text.secondary" sx={{ maxWidth: "64ch" }}>
        No issues yet. Incidents the SRE agent files, the platform&apos;s own and those people file on GitHub show here.
      </Typography>
    );
  } else {
    const sections = issueSections(issues.data);
    body = (
      <>
        <Section title="Needs attention" projectName={projectName} issues={sections.attention} />
        <Section title="Open" projectName={projectName} issues={sections.open} />
        <Section title="Closed" projectName={projectName} issues={sections.closed} />
      </>
    );
  }

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 3, maxWidth: 960 }}>
      <Box>
        <Typography variant="caption" color="text.secondary">
          {project.data ? projectLabel(project.data) : projectName}
        </Typography>
        <Typography component="h1" variant="h4" sx={{ fontWeight: 600 }}>
          Issues
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
          The project&apos;s issues on GitHub: incidents, the platform&apos;s own work, and those people filed.
        </Typography>
      </Box>
      {body}
    </Box>
  );
}
