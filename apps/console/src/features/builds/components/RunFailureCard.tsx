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

import { Fragment, useState } from "react";
import { Box, Button, Collapse, Stack, Typography, alpha } from "@wso2/oxygen-ui";
import { ArrowRight } from "@wso2/oxygen-ui-icons-react";
import { createLink } from "@tanstack/react-router";
import type { components } from "../../../generated/aep-api";
import { detailsText, failureCopy, type FailureDetails } from "../lib/failure";

type MilestoneRunView = components["schemas"]["MilestoneRunView"];

const LinkButton = createLink(Button);

/**
 * Why the run failed — or what it is retrying — in the platform's recorded
 * words, under the page header and above the summary card.
 *
 * Draws only when there is something to explain: a failed run, or a run still
 * moving with a recorded fault (a fault being retried, amber). A cancelled run
 * draws nothing — a person stopping an increment is not a fault. Every sentence
 * comes from `failureCopy`, the one place a failure is put into words, so this
 * card, the ledger chip and the overview's track cannot disagree.
 *
 * Progressive disclosure: the headline says what, where and next; *Show
 * details* opens the facts a bug report needs — the code, whether it is
 * permanent, the attempts, the platform's own error text, and the run and
 * workflow ids an operator reads history by — with a Copy button, because
 * "paste what the card says" is the support conversation this exists to start.
 */
export function RunFailureCard({
  projectName,
  run,
}: {
  projectName: string;
  run: MilestoneRunView | undefined;
}) {
  const [open, setOpen] = useState(false);
  if (!run) return null;
  const copy = failureCopy(run);
  if (!copy) return null;
  const tone = copy.tone;

  return (
    <Box
      role="status"
      aria-label={tone === "error" ? "Build failure" : "Build retrying"}
      sx={(theme) => {
        const main = theme.palette[tone].main;
        return {
          border: `1px solid ${alpha(main, 0.35)}`,
          bgcolor: alpha(main, 0.06),
          borderRadius: 2,
          px: 1.75,
          py: 1.25,
        };
      }}
    >
      <Stack spacing={0.75}>
        <Typography variant="subtitle2" sx={{ fontWeight: 600 }}>
          {copy.title}
        </Typography>
        <Typography variant="body2" color="text.secondary">
          {copy.body}
        </Typography>
        <Stack direction="row" spacing={1} sx={{ alignItems: "center", flexWrap: "wrap" }}>
          {copy.next && (
            <LinkButton
              to={copy.next.to}
              params={{ projectName }}
              {...(copy.next.search ? { search: copy.next.search } : {})}
              size="small"
              color="inherit"
              endIcon={<ArrowRight size={14} aria-hidden />}
              sx={{ fontWeight: 500, px: 1.25 }}
            >
              {copy.next.label}
            </LinkButton>
          )}
          <Button
            size="small"
            color="inherit"
            onClick={() => setOpen((o) => !o)}
            aria-expanded={open}
            sx={{ px: 1.25 }}
          >
            {open ? "Hide details" : "Show details"}
          </Button>
        </Stack>
        <Collapse in={open} unmountOnExit>
          <FailureDetailsList details={copy.details} />
        </Collapse>
      </Stack>
    </Box>
  );
}

function FailureDetailsList({ details }: { details: FailureDetails }) {
  const [copied, setCopied] = useState(false);
  const rows: Array<[string, string | undefined]> = [
    [
      "code",
      details.permanent === undefined
        ? details.code
        : `${details.code} · ${details.permanent ? "permanent" : "retryable"}`,
    ],
    ["attempts", details.attempts],
    ["window", details.window],
    ["recorded", details.detail],
    ["run", details.runId],
    ["workflow", details.workflowId],
  ];
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(detailsText(details));
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // The clipboard is a convenience; the text is on screen regardless.
    }
  };
  return (
    <Box
      sx={(theme) => ({
        mt: 1,
        pt: 1,
        borderTop: `1px solid ${theme.palette.divider}`,
        display: "grid",
        gridTemplateColumns: "max-content 1fr",
        columnGap: 2,
        rowGap: 0.5,
        fontFamily: "monospace",
        fontSize: 12,
      })}
      data-testid="run-failure-details"
    >
      {rows
        .filter((r): r is [string, string] => Boolean(r[1]))
        .map(([k, v]) => (
          <Fragment key={k}>
            <Typography component="dt" variant="caption" color="text.secondary" sx={{ fontFamily: "inherit" }}>
              {k}
            </Typography>
            <Typography component="dd" variant="caption" sx={{ fontFamily: "inherit", m: 0, wordBreak: "break-word" }}>
              {v}
            </Typography>
          </Fragment>
        ))}
      <Box sx={{ gridColumn: "1 / -1", mt: 0.5 }}>
        <Button size="small" color="inherit" onClick={copy} sx={{ px: 1.25 }}>
          {copied ? "Copied" : "Copy details"}
        </Button>
      </Box>
    </Box>
  );
}
