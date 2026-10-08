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

import { useState, type ReactNode } from "react";
import { Alert, Box, Button, Skeleton, Typography } from "@wso2/oxygen-ui";
import { ExternalLink } from "@wso2/oxygen-ui-icons-react";
import { stamp } from "../../../lib/stamp";
import { TaskLog } from "../../builds/components/TaskLog";
import { statusLine } from "../../builds/model/taskRow";
import { useIssueThreadState } from "../../agent-chat/useIssueThread";
import { CardOverlay } from "../../projects/components/CardOverlay";
import { useHandToCodingAgent } from "../api/handOff";
import { useIssueDetail, useProjectIssues } from "../api/issues";
import {
  attentionLabel,
  attentionNeedsPerson,
  attentionWhy,
  codingAgentTookOn,
  issueOrigin,
  issueStateLabel,
  issueText,
} from "../model/issues";
import { ComponentPicker } from "./ComponentPicker";
import { ORIGIN_LABEL } from "./IssuesPage";

// An Issue card, over the Issues Page: the issue's text, who opened it, its
// newest status line, why it needs a person, and, once the coding agent has
// taken it on, the agent's log on it (the same log the Build card's task row
// shows). An open issue has an agent of its own, whose chat the chat panel
// draws beside the card; a closed one has none, and the card says it is
// closed (the issue list's word, or the server's once its chat was removed).
//
// An open issue the coding agent has not taken on can be handed to it from
// here: the person picks the design's component it is about, and once aep-api
// has it the card says so and its log takes over.

function Part({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Box component="section" aria-label={title} sx={{ display: "flex", flexDirection: "column", gap: 0.75 }}>
      <Typography component="h3" sx={{ fontSize: "0.8125rem", fontWeight: 600 }}>
        {title}
      </Typography>
      {children}
    </Box>
  );
}

export function IssueCard({ projectName, number }: { projectName: string; number: string }) {
  const issueNumber = /^\d+$/.test(number) ? Number(number) : null;
  return (
    <CardOverlay card="issue" title={issueNumber ? `Issue #${issueNumber}` : "Issue"}>
      {issueNumber ? (
        <IssueBody projectName={projectName} issueNumber={issueNumber} />
      ) : (
        <Typography variant="body2" color="text.secondary">
          There is no issue &ldquo;{number}&rdquo;.
        </Typography>
      )}
    </CardOverlay>
  );
}

function IssueBody({ projectName, issueNumber }: { projectName: string; issueNumber: number }) {
  const issues = useProjectIssues(projectName);
  const detail = useIssueDetail(projectName, issueNumber);
  const issue = issues.data?.find((i) => i.Number === issueNumber);
  const closed = useIssueThreadState(projectName, issueNumber) === "closed";
  const handOff = useHandToCodingAgent(projectName, issueNumber);
  const [picking, setPicking] = useState(false);

  if (!issue) {
    if (issues.isPending || (issues.isError && detail.isPending)) return <Skeleton variant="rounded" height={200} />;
    const error = issues.error ?? detail.error;
    return (
      <Alert severity={error ? "error" : "info"}>
        {error ? error.message : `Issue #${issueNumber} is not one of this project's issues.`}
      </Alert>
    );
  }

  const origin = issueOrigin(issue.Labels);
  const reason = issue.attentionReason;
  const newest = detail.data?.comments?.at(-1);
  const line = detail.data ? statusLine(detail.data) : null;
  const text = issueText(issue.Body);
  const tookOn = codingAgentTookOn(issue, detail.data) || handOff.isSuccess;
  const canHand = !tookOn && !closed;
  // Closed by the server's word while the list still lags: the header says closed too.
  const stateLabel = issueStateLabel(closed && issue.State !== "closed" ? { State: "closed" } : issue);

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2.5, maxWidth: "72ch" }}>
      <Box>
        <Typography component="h2" variant="h6" sx={{ fontWeight: 600 }}>
          {issue.Title}
        </Typography>
        <Typography variant="body2" color="text.secondary">
          {ORIGIN_LABEL[origin.kind]} · Opened by {origin.by} ·{" "}
          {stateLabel}
        </Typography>
      </Box>
      {closed && <Alert severity="info">This issue is closed.</Alert>}
      {reason && (
        <Alert severity={attentionNeedsPerson(reason) ? "warning" : "info"}>
          <strong>{attentionLabel(reason)}.</strong> {attentionWhy(reason)}
        </Alert>
      )}
      <Box sx={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 1 }}>
        <Button
          size="small"
          variant="outlined"
          href={issue.URL}
          target="_blank"
          rel="noreferrer"
          endIcon={<ExternalLink size={14} aria-hidden />}
        >
          Open on GitHub
        </Button>
        {canHand && !picking && (
          <Button size="small" variant="contained" onClick={() => setPicking(true)}>
            Hand to the coding agent
          </Button>
        )}
      </Box>
      {canHand && picking && (
        <ComponentPicker
          projectName={projectName}
          handOff={handOff}
          onCancel={() => {
            handOff.reset();
            setPicking(false);
          }}
        />
      )}
      {handOff.isSuccess && <Alert severity="success">Handed to the coding agent.</Alert>}
      <Part title="Status">
        {detail.isPending ? (
          <Skeleton width="60%" />
        ) : detail.isError ? (
          <Typography variant="body2" color="text.secondary">
            The issue&apos;s comments could not be read.
          </Typography>
        ) : line && newest ? (
          <Typography variant="body2">
            {line}
            <Typography component="span" variant="caption" color="text.secondary">
              {" "}
              · {newest.author || "a deleted account"}
              {stamp(newest.createdAt) && `, ${stamp(newest.createdAt)}`}
            </Typography>
          </Typography>
        ) : (
          <Typography variant="body2" color="text.secondary">
            No one has commented on it yet.
          </Typography>
        )}
      </Part>
      <Part title="The issue">
        {text ? (
          <Typography variant="body2" component="div" sx={{ whiteSpace: "pre-wrap", wordBreak: "break-word" }}>
            {text}
          </Typography>
        ) : (
          <Typography variant="body2" color="text.secondary">
            It has no description.
          </Typography>
        )}
      </Part>
      {tookOn && (
        <Part title="The coding agent's log">
          <Box sx={{ border: 1, borderColor: "divider", borderRadius: 2, overflow: "hidden" }}>
            <TaskLog projectName={projectName} issueNumber={issueNumber} />
          </Box>
        </Part>
      )}
    </Box>
  );
}
