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

/**
 * One referential finding, rendered where it belongs — inline on a matrix row,
 * on a role card, or above the matrix.
 *
 * The sentence is the gate's own `message`, never a second wording: the author
 * reads the same words here that the design agent is answered with when its
 * write is refused, so a warning on this page and a refusal in chat are
 * recognisably the same rule.
 */

import { Stack, Tooltip, Typography } from "@wso2/oxygen-ui";
import { CircleAlert, Info, TriangleAlert } from "@wso2/oxygen-ui-icons-react";

import type { SecurityReferenceFinding } from "@aep/agent-stream";

/** The colour and icon each severity speaks in. */
const TONE = {
  error: { color: "error.main", Icon: CircleAlert, word: "Error" },
  warning: { color: "warning.main", Icon: TriangleAlert, word: "Warning" },
  info: { color: "text.secondary", Icon: Info, word: "Note" },
} as const;

function FindingLine({
  finding,
  dense = false,
}: {
  finding: SecurityReferenceFinding;
  /** On a matrix row: the icon and a shorter type ramp. */
  dense?: boolean;
}) {
  const tone = TONE[finding.severity];
  return (
    <Stack
      direction="row"
      spacing={0.5}
      alignItems="flex-start"
      sx={{ color: tone.color }}
      data-testid={`finding-${finding.key}`}
    >
      <Tooltip title={tone.word}>
        <Stack
          component="span"
          sx={{ mt: dense ? "1px" : "2px", flexShrink: 0 }}
          aria-label={tone.word}
        >
          <tone.Icon size={dense ? 13 : 15} aria-hidden />
        </Stack>
      </Tooltip>
      <Typography variant={dense ? "caption" : "body2"} color="inherit">
        {finding.message}
      </Typography>
    </Stack>
  );
}

/** A list of findings, or nothing at all when there are none. */
export function FindingLines({
  findings,
  dense = false,
}: {
  findings: readonly SecurityReferenceFinding[];
  dense?: boolean;
}) {
  if (findings.length === 0) return null;
  return (
    <Stack spacing={0.25} sx={{ mt: dense ? 0.25 : 0.5 }}>
      {findings.map((finding) => (
        <FindingLine
          key={`${finding.key}:${JSON.stringify(finding.params)}`}
          finding={finding}
          dense={dense}
        />
      ))}
    </Stack>
  );
}
