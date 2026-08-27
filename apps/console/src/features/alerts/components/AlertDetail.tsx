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
  Chip,
  CircularProgress,
  Link,
  PageContent,
  Step,
  StepContent,
  StepLabel,
  Stepper,
  Typography,
  formatRelativeTime,
} from "@wso2/oxygen-ui";
import { ExternalLink } from "@wso2/oxygen-ui-icons-react";
import { Link as RouterLink, useNavigate } from "@tanstack/react-router";
import { useAlertReport } from "../api/queries";
import { classificationLabel, classificationTone } from "../classification";
// The diagnosis is Markdown (headings / lists / code / timeline); render it as
// a preview rather than raw text. MarkdownView is the console's shared
// theme-token-styled react-markdown renderer.
import { MarkdownView } from "../../../components/MarkdownView";
import { PageHeader } from "../../../components/PageHeader";
import { StatusChip } from "../../../components/StatusChip";
import type { components } from "../../../generated/aep-api";

type RcaAgentReport = components["schemas"]["RcaAgentReport"];
type Stage = "received" | "issue-created" | "coding-handover" | "verify-fix";

const STAGE_INDEX: Record<Stage, number> = {
  received: 0,
  "issue-created": 1,
  "coding-handover": 2,
  "verify-fix": 3,
};

// Derived client-side from existing report fields — no stored state machine
// (#155 decision). "No issue" (config-level/none classifications) is
// terminal at Alert Received; the remaining stages never apply.
function deriveStage(report: RcaAgentReport): Stage {
  if (!report.issueNumber) return "received";
  if (report.deployed) return "verify-fix";
  if (report.dispatched) return "coding-handover";
  return "issue-created";
}

function AlertReceivedContent({ report }: { report: RcaAgentReport }) {
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
        {report.classification && (
          <StatusChip
            label={classificationLabel(report.classification)}
            tone={classificationTone(report.classification)}
            variant="outlined"
          />
        )}
        {report.component && (
          <Chip label={report.component} size="small" variant="outlined" />
        )}
        {report.createdAt && (
          <Typography variant="caption" color="text.secondary">
            {formatRelativeTime(new Date(report.createdAt))}
          </Typography>
        )}
      </Box>
      {report.diagnosis && <MarkdownView>{report.diagnosis}</MarkdownView>}
    </Box>
  );
}

function IssueCreatedContent({ report }: { report: RcaAgentReport }) {
  if (!report.issueNumber) {
    return (
      <Alert severity="info">
        No GitHub issue was created for this alert —{" "}
        {report.classification === "config-level"
          ? "the handoff resolved it via a config change, no code action needed."
          : "the handoff found no actionable remediation."}
      </Alert>
    );
  }
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
      <Typography variant="subtitle2">{report.issueTitle}</Typography>
      <Typography variant="body2" color="text.secondary">
        {report.issueExcerpt}
      </Typography>
      {report.issueUrl && (
        <Box>
          <Button
            component={Link}
            href={report.issueUrl}
            target="_blank"
            rel="noopener noreferrer"
            size="small"
            variant="outlined"
            endIcon={<ExternalLink size={14} />}
          >
            View issue #{report.issueNumber} on GitHub
          </Button>
        </Box>
      )}
    </Box>
  );
}

function CodingHandoverContent({ report }: { report: RcaAgentReport }) {
  const navigate = useNavigate();
  // Land on the specific Task/build for this alert's issue, not the tasks
  // list. Guarded below: the buttons only render when issueNumber is set.
  const goToBuild = () =>
    void navigate({
      to: "/projects/$projectName/tasks/$issueNumber",
      params: {
        projectName: report.project!,
        // A number now: `/tasks/$issueNumber` renders the task itself and parses
        // its param (ADR-0021 §7). It used to be a bare redirect into
        // `/builds/$issueNumber`, which typed the param as a string.
        issueNumber: report.issueNumber!,
      },
    });

  if (!report.issueNumber) {
    return (
      <Typography variant="body2" color="text.secondary">
        Not applicable — no issue was created.
      </Typography>
    );
  }
  if (!report.dispatched) {
    return (
      <Alert
        severity="warning"
        action={
          <Button onClick={goToBuild} size="small">
            Dispatch from Build
          </Button>
        }
      >
        Issue #{report.issueNumber} was created, but no coding agent has been
        dispatched yet.
      </Alert>
    );
  }
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
      <Typography variant="body2" color="text.secondary">
        The coding agent has been dispatched for issue #{report.issueNumber}.
      </Typography>
      <Box>
        <Button onClick={goToBuild} size="small" variant="outlined">
          View build phase
        </Button>
      </Box>
    </Box>
  );
}

function VerifyFixContent({ report }: { report: RcaAgentReport }) {
  if (!report.issueNumber) {
    return (
      <Typography variant="body2" color="text.secondary">
        Not applicable — no issue was created.
      </Typography>
    );
  }
  if (!report.deployed) {
    return (
      <Typography variant="body2" color="text.secondary">
        Waiting for the fix to be built and deployed before it can be
        verified.
      </Typography>
    );
  }
  return (
    <Alert severity="success">
      The fix was deployed
      {report.deployedAt ? ` ${formatRelativeTime(new Date(report.deployedAt))}` : ""}.
      Reproduce the original failure and confirm it's resolved.
    </Alert>
  );
}

export function AlertDetail({ alertId }: { alertId: string }) {
  const { data: report, isPending, isError, error, refetch } = useAlertReport(alertId);

  return (
    <PageContent>
      <PageHeader
        title={report?.title ?? "Alert"}
        {...(report?.project && { subtitle: report.project })}
        backTo={{ link: <RouterLink to="/alerts" />, label: "Back to Alerts" }}
      />

      {isPending ? (
        <Box sx={{ display: "flex", justifyContent: "center", p: 6 }}>
          <CircularProgress aria-label="Loading alert" />
        </Box>
      ) : isError || !report ? (
        <Alert
          severity="error"
          action={<Button onClick={() => void refetch()}>Retry</Button>}
        >
          Failed to load this alert
          {error instanceof Error && error.message ? `: ${error.message}` : ""}
        </Alert>
      ) : (
        <Stepper activeStep={STAGE_INDEX[deriveStage(report)]} orientation="vertical" nonLinear sx={{ height: "45vh", overflow: "auto" }}>
          <Step completed={STAGE_INDEX[deriveStage(report)] > 0}>
            <StepLabel>Alert Received</StepLabel>
            <StepContent TransitionProps={{ in: true }}>
              <AlertReceivedContent report={report} />
            </StepContent>
          </Step>
          <Step completed={STAGE_INDEX[deriveStage(report)] > 1}>
            <StepLabel>Issue Created</StepLabel>
            <StepContent TransitionProps={{ in: true }}>
              <IssueCreatedContent report={report} />
            </StepContent>
          </Step>
          <Step completed={STAGE_INDEX[deriveStage(report)] > 2}>
            <StepLabel>Coding Handover</StepLabel>
            <StepContent TransitionProps={{ in: true }}>
              <CodingHandoverContent report={report} />
            </StepContent>
          </Step>
          <Step completed={false}>
            <StepLabel>Verify Fix</StepLabel>
            <StepContent TransitionProps={{ in: true }}>
              <VerifyFixContent report={report} />
            </StepContent>
          </Step>
        </Stepper>
      )}
    </PageContent>
  );
}
