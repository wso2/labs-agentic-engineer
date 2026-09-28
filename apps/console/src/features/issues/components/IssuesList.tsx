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

import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  CircularProgress,
  Link,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { ExternalLink } from "@wso2/oxygen-ui-icons-react";
import { EmptyState } from "../../../components/EmptyState";
import { StatusChip } from "../../../components/StatusChip";
import { attentionDescription, attentionLabel } from "../attention";
import { useProjectIssues, type IssueInfo } from "../api/queries";

function stateTone(state: string): "success" | "warning" | "neutral" {
  if (state === "closed") return "success";
  if (state === "open") return "warning";
  return "neutral";
}

function issueLabels(issue: IssueInfo): string[] {
  return issue.Labels ?? [];
}

export function IssuesList({ projectName }: { projectName: string }) {
  const { data: issues, isPending, isError, error, refetch } = useProjectIssues(projectName);

  if (isPending) {
    return (
      <Box sx={{ display: "flex", justifyContent: "center", p: 6 }}>
        <CircularProgress aria-label="Loading issues" />
      </Box>
    );
  }

  if (isError) {
    return (
      <Alert severity="error" action={<Button onClick={() => void refetch()}>Retry</Button>}>
        Failed to load issues
        {error instanceof Error && error.message ? `: ${error.message}` : ""}
      </Alert>
    );
  }

  if (!issues || issues.length === 0) {
    return (
      <EmptyState
        title="No issues yet"
        description="SRE incident issues and coding-agent handoffs for this project will appear here."
      />
    );
  }

  return (
    <Stack spacing={2}>
      {issues.map((issue) => (
        <Card key={issue.Number} variant="outlined">
          <CardContent>
            <Stack spacing={1.25}>
              <Box sx={{ display: "flex", justifyContent: "space-between", gap: 2 }}>
                <Box>
                  <Typography variant="subtitle1">
                    #{issue.Number} {issue.Title}
                  </Typography>
                  <Typography variant="body2" color="text.secondary">
                    {issue.Body}
                  </Typography>
                </Box>
                <StatusChip label={issue.State} tone={stateTone(issue.State)} variant="outlined" />
              </Box>

              {issue.attentionReason && (
                <Alert severity={issue.attentionReason === "escalated" ? "error" : "warning"}>
                  <strong>{attentionLabel(issue.attentionReason)}:</strong>{" "}
                  {attentionDescription(issue.attentionReason)}
                </Alert>
              )}

              <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap", alignItems: "center" }}>
                {issueLabels(issue).map((label) => (
                  <Chip key={label} label={label} size="small" variant="outlined" />
                ))}
                <Button
                  component={Link}
                  href={issue.URL}
                  target="_blank"
                  rel="noopener noreferrer"
                  size="small"
                  variant="outlined"
                  endIcon={<ExternalLink size={14} />}
                >
                  View on GitHub
                </Button>
              </Box>
            </Stack>
          </CardContent>
        </Card>
      ))}
    </Stack>
  );
}
