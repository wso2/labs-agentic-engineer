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

import { Box, Button, Stack, Typography } from "@wso2/oxygen-ui";
import { createLink } from "@tanstack/react-router";
import { StatusChip } from "../../../components/StatusChip";
import type { components } from "../../../generated/aep-api";
import { gateSubject } from "../../tasks/lib/issueRows";
import { gateRows, gatesNeedAction } from "../lib/provisioning";
import { stageTone, type StageState } from "../lib/stage";

type TaskView = components["schemas"]["TaskView"];

// Router-typed Oxygen Button (the console's createLink pattern) so the way out
// of a stalled connection is a real navigation.
const LinkButton = createLink(Button);

/**
 * The PROVISIONING stage's own content: one row per connection, saying who is
 * acting on it, plus the way out when one is stalled on a person.
 *
 * The stage itself — step 1 of the run's flow — is a row on the rail that
 * RunSpine owns; this is what hangs under it. Provisioning leads the flow
 * because that is the order the platform works in: the dispatch predicate is
 * run-level, so no build session starts while any gate is open.
 */
export function ProvisioningGates({
  projectName,
  gates,
  /** The stage's own state, which decides the tone of the way out. */
  state,
}: {
  projectName: string;
  /** Every gate in the milestone, resolved ones included — a provisioned
   *  connection is as much a part of how a version came to exist as a merged
   *  pull request. */
  gates: TaskView[];
  state: StageState;
}) {
  const rows = gateRows(gates);
  const needsAction = gatesNeedAction(gates);

  return (
      <Stack spacing={1}>
        {rows.map((row) => (
          <Stack
            key={row.gate.issueNumber}
            direction="row"
            spacing={1}
            sx={{ alignItems: "center", flexWrap: "wrap", rowGap: 0.5 }}
          >
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ fontVariantNumeric: "tabular-nums", flexShrink: 0 }}
            >
              #{row.gate.issueNumber}
            </Typography>
            <Typography
              component="a"
              href={row.gate.issueUrl}
              target="_blank"
              rel="noreferrer"
              variant="body2"
              sx={{
                color: "text.primary",
                textDecoration: "none",
                "&:hover": { textDecoration: "underline" },
              }}
            >
              {gateSubject(row.gate.title)}
            </Typography>
            <StatusChip
              label={row.label}
              tone={stageTone(row.state)}
              appearance="soft"
              dot={row.state !== "done"}
            />
          </Stack>
        ))}
        {needsAction && (
          <Box>
            <LinkButton
              size="small"
              variant="outlined"
              color={state === "failed" ? "error" : "warning"}
              to="/projects/$projectName/builds"
              params={{ projectName }}
              search={{ connections: "open" }}
            >
              Configure connections
            </LinkButton>
          </Box>
        )}
      </Stack>
  );
}
