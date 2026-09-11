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
  Card,
  CardContent,
  Chip,
  Divider,
  MenuItem,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import { Bot } from "@wso2/oxygen-ui-icons-react";
import type { components } from "../../../generated/aep-api";
import { useSetCodingAgent } from "../api/queries";
import { CodingAgentKeySection } from "./CodingAgentKeySection";

type CodingAgentProjection = components["schemas"]["CodingAgentProjection"];
type AgentRuntime = components["schemas"]["AgentRuntime"];
type CodingAgentModel = components["schemas"]["CodingAgentModel"];
type LLMProjection = components["schemas"]["LLMProjection"];

/**
 * Membership of the contract's `AgentRuntime` enum is NOT availability: the
 * enum carries every runtime the design covers so a client can render the
 * choice, while the platform ships an adapter for only one of them and the API
 * rejects the others by name. Hiding the unavailable one would leave a reader
 * guessing whether it exists; offering it would leave them holding a rejection.
 * So it is listed, disabled, and told why.
 */
const RUNTIMES: {
  value: AgentRuntime;
  label: string;
  unavailableReason?: string;
}[] = [
  { value: "claude-code", label: "Claude Code" },
  {
    value: "opencode",
    label: "OpenCode",
    unavailableReason: "no adapter on this platform yet",
  },
];

/**
 * Deliberately narrower than the list the runtime can serve. A cycle's cost is
 * stamped from a per-model rate table and the stamp is all-or-nothing, so a
 * model with no rate row would blank the cost of the whole cycle — not just its
 * own share. A model appears here only once the platform can price it.
 */
const MODELS: { value: CodingAgentModel; label: string }[] = [
  { value: "claude-sonnet-5", label: "Claude Sonnet 5" },
  { value: "claude-haiku-4-5", label: "Claude Haiku 4.5" },
];

function isSelectable(runtime: AgentRuntime): boolean {
  return RUNTIMES.some(
    (r) => r.value === runtime && r.unavailableReason === undefined,
  );
}

/**
 * The organization's coding-agent settings: which runtime and model its coding
 * runs use, plus the credential those runs bill (ADR-0016 — the key is a
 * coding-agent setting, so it lives here rather than beside the org's default
 * Anthropic key).
 *
 * Runtime and model are ALWAYS present — every org has an effective pair
 * whether or not anyone has opened this card. `updatedBy` is what separates
 * "nobody has ever chosen" from "somebody chose these very values", and the
 * card says which one the reader is looking at rather than implying a choice
 * that was never made.
 *
 * Each select writes on change, sending only the field that moved: the patch's
 * two fields are independently optional, so restating the other would let a
 * stale read overwrite it.
 */
/**
 * Who last changed the setting, and when — omitting the "when" when there isn't
 * one rather than printing it wrong.
 *
 * `new Date(undefined ?? "")` is an Invalid Date, and `toLocaleString()` on one
 * renders the literal words "Invalid Date" into the sentence. The timestamp is
 * optional on the wire (a row can be written by a path that does not stamp it),
 * so the missing case is reachable, and "Changed by admin" is honest where
 * "Changed Invalid Date by admin" is not.
 */
function changedLine(updatedAt: string | null | undefined, updatedBy: string | null | undefined): string {
  const who = updatedBy ? ` by ${updatedBy}` : "";
  const at = updatedAt ? new Date(updatedAt) : undefined;
  if (!at || Number.isNaN(at.getTime())) return `Changed${who}`;
  return `Changed ${at.toLocaleString()}${who}`;
}

export function CodingAgentCard({
  codingAgent,
  codingLlm,
  llmConnected,
}: {
  codingAgent: CodingAgentProjection;
  codingLlm: LLMProjection | null;
  llmConnected: boolean;
}) {
  const save = useSetCodingAgent();
  const onPlatformDefaults = !codingAgent.updatedBy;

  return (
    <Card variant="outlined">
      <CardContent sx={{ p: 3 }}>
        <Box sx={{ display: "flex", alignItems: "center", gap: 1.5, mb: 2 }}>
          <Bot size={22} />
          <Typography variant="h6">Coding agent</Typography>
          {onPlatformDefaults && (
            <Chip label="platform defaults" size="small" variant="outlined" />
          )}
        </Box>
        <Divider sx={{ mb: 3 }} />

        <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
          The runtime and model this organization&apos;s coding runs use. A
          change applies from the <strong>next cycle</strong> — a run already in
          flight keeps the runtime and model it was launched with.
        </Typography>

        <Box
          sx={{
            display: "grid",
            gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" },
            gap: 3,
          }}
        >
          <TextField
            select
            fullWidth
            label="Runtime"
            value={codingAgent.runtime}
            disabled={save.isPending}
            onChange={(e) => {
              // A disabled MenuItem is only kept unclickable by CSS, which is
              // not a guarantee — an unavailable runtime must never reach the
              // API, which would reject it and leave the reader holding an
              // error for a choice the card said they could not make.
              const next = e.target.value as AgentRuntime;
              if (!isSelectable(next)) return;
              save.mutate({ runtime: next });
            }}
            helperText="OpenCode is listed because the contract carries it, but the platform ships no adapter for it — the API rejects it."
          >
            {RUNTIMES.map((r) => (
              <MenuItem
                key={r.value}
                value={r.value}
                disabled={r.unavailableReason !== undefined}
              >
                {r.unavailableReason
                  ? `${r.label} — ${r.unavailableReason}`
                  : r.label}
              </MenuItem>
            ))}
          </TextField>

          <TextField
            select
            fullWidth
            label="Model"
            value={codingAgent.model}
            disabled={save.isPending}
            onChange={(e) =>
              save.mutate({ model: e.target.value as CodingAgentModel })
            }
            helperText="Only models the platform holds a cost rate for are offered — a run whose model cannot be priced would leave the whole cycle's cost blank."
          >
            {MODELS.map((m) => (
              <MenuItem key={m.value} value={m.value}>
                {m.label}
              </MenuItem>
            ))}
          </TextField>
        </Box>

        <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
          {onPlatformDefaults
            ? "Nobody has changed this yet, so the organization runs on the platform's defaults."
            : changedLine(codingAgent.updatedAt, codingAgent.updatedBy)}
        </Typography>

        {save.isError && (
          <Alert severity="error" sx={{ mt: 2 }}>
            {save.error.message}
          </Alert>
        )}

        {/* The credential the coding agent bills is an OVERRIDE on the org's
            Anthropic key, so it is only offered once there is a key to
            override — the server rejects it otherwise. */}
        {llmConnected && <CodingAgentKeySection codingLlm={codingLlm} />}
      </CardContent>
    </Card>
  );
}
