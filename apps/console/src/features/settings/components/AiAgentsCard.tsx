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
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  Divider,
  FormControlLabel,
  MenuItem,
  Radio,
  RadioGroup,
  Switch,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import { Bot } from "@wso2/oxygen-ui-icons-react";
import type { components } from "../../../generated/aep-api";
import {
  MODELS,
  SUBSCRIPTION_TOKEN_PREFIX,
  runtimeAvailable,
  type AiSettings,
} from "../aiSettings";
import { useAiSettings } from "../hooks/useAiSettings";
import { AnthropicKeyRow } from "./AnthropicKeyRow";
import { MaskedCredential, SecretField } from "./CredentialField";

type ConfigProjection = components["schemas"]["ConfigProjection"];
type AgentRuntime = components["schemas"]["AgentRuntime"];

// One constant: the card's name is expected to change.
const CARD_TITLE = "AI agents";

/** Why a runtime's tile is disabled: the installation has no runner image for it. */
const UNAVAILABLE_REASON = "Not available on this installation.";

const RUNTIMES: { value: AgentRuntime; label: string; description: string }[] = [
  {
    value: "claude-code",
    label: "Claude Code",
    description: "Anthropic's coding agent.",
  },
  {
    value: "opencode",
    label: "OpenCode",
    description: "Open-source coding agent. Uses the API key.",
  },
];

/**
 * Who last changed the model or coding agent, and when. The timestamp is
 * optional on the wire, and printing an Invalid Date would be worse than
 * leaving it out.
 */
function changedLine(updatedAt: string | null, updatedBy: string | null): string {
  const who = updatedBy ? ` by ${updatedBy}` : "";
  const at = updatedAt ? new Date(updatedAt) : undefined;
  if (!at || Number.isNaN(at.getTime())) return `Changed${who}`;
  return `Changed ${at.toLocaleString()}${who}`;
}

/**
 * How the organization's agents run: the model every agent uses, the Anthropic
 * API key they call it with, and the coding agent, with an optional Claude
 * subscription that only Claude Code can bill. One Save sends whatever
 * changed; `aiSettings.ts` decides which `/config` sections that is.
 *
 * `onboarding` is the wizard's use of the same card: Save reads "Continue" and
 * waits for an API key, since the wizard exists to get one and a save without
 * it would leave the organization where it started. The wizard frames and
 * explains the step itself, so the card drops its own frame, header and the
 * key row's explanation there.
 */
export function AiAgentsCard({
  config,
  onboarding = false,
}: {
  config: ConfigProjection;
  onboarding?: boolean;
}) {
  const ai = useAiSettings(config);
  const { saved, draft } = ai;
  const busy = ai.saving;
  const keyMissing = saved.apiKey === null && draft.apiKey.trim() === "";
  const canSave = ai.canSave && !(onboarding && keyMissing);

  const body = (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
      <TextField
        select
        fullWidth
        label="Model"
        value={draft.model}
        disabled={busy}
        onChange={(e) =>
          ai.change({ model: e.target.value as AiSettings["model"] })
        }
        helperText="Every agent uses this model, from requirements to coding. Agents pick up a change from their next step; coding runs already in flight keep their model."
      >
        {MODELS.map((m) => (
          <MenuItem key={m.value} value={m.value}>
            {m.label}
          </MenuItem>
        ))}
      </TextField>

      <AnthropicKeyRow
        // A newly stored key closes the replace field.
        key={saved.apiKey?.connectedAt ?? "none"}
        stored={saved.apiKey}
        value={draft.apiKey}
        onChange={(apiKey) => ai.change({ apiKey })}
        error={ai.error?.field === "apiKey" ? ai.error.message : undefined}
        disabled={busy}
        hasSubscription={saved.subscription !== null}
        onDisconnect={ai.disconnectKey}
        disconnecting={ai.disconnecting}
        disconnectError={ai.disconnectError}
        explained={!onboarding}
      />

      <Box>
        <Typography
          variant="subtitle2"
          component="h3"
          id="coding-agent-label"
          sx={{ mb: 1 }}
        >
          Coding agent
        </Typography>
        <RadioGroup
          aria-labelledby="coding-agent-label"
          value={draft.runtime}
          onChange={(e) => ai.change({ runtime: e.target.value as AgentRuntime })}
          sx={{
            display: "grid",
            gridTemplateColumns: { xs: "1fr", sm: "1fr 1fr" },
            gap: 1.5,
          }}
        >
          {RUNTIMES.map((r) => {
            const selected = draft.runtime === r.value;
            const available = runtimeAvailable(saved, r.value);
            return (
              <Tile key={r.value} selected={selected}>
                <FormControlLabel
                  value={r.value}
                  disabled={busy || !available}
                  control={<Radio />}
                  label={
                    <Box>
                      <Typography variant="body1" fontWeight={600}>
                        {r.label}
                      </Typography>
                      <Typography variant="body2" color="text.secondary">
                        {available ? r.description : UNAVAILABLE_REASON}
                      </Typography>
                    </Box>
                  }
                  sx={{ alignItems: "flex-start", m: 0 }}
                />
                {selected && !available && (
                  <Alert severity="error" sx={{ mt: 1.5 }}>
                    {r.label} is not available on this installation, so every
                    coding run fails. Choose another coding agent and save.
                  </Alert>
                )}
                {r.value === "claude-code" && selected && (
                  <SubscriptionControl
                    key={
                      saved.subscription
                        ? `${saved.subscription.keyPrefix}${saved.subscription.keyLast4}`
                        : "none"
                    }
                    ai={ai}
                  />
                )}
                {r.value === "opencode" && selected && ai.removesSubscription && (
                  <Alert severity="warning" sx={{ mt: 1.5 }}>
                    Saving deletes the stored Claude subscription token.
                    OpenCode bills coding to the API key.
                  </Alert>
                )}
              </Tile>
            );
          })}
        </RadioGroup>
        {ai.error?.field === "runtime" && (
          <Alert severity="error" sx={{ mt: 1.5 }}>
            {ai.error.message}
          </Alert>
        )}
      </Box>

      {saved.updatedBy && (
        <Typography variant="body2" color="text.secondary">
          {changedLine(saved.updatedAt, saved.updatedBy)}
        </Typography>
      )}

      {ai.error?.field === "card" && (
        <Alert severity="error">{ai.error.message}</Alert>
      )}

      <Box sx={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 1.5 }}>
        <Button variant="contained" onClick={ai.save} disabled={!canSave}>
          {busy ? "Saving…" : onboarding ? "Continue" : "Save"}
        </Button>
        {ai.dirty && (
          <Button onClick={ai.discard} disabled={busy}>
            Discard changes
          </Button>
        )}
        {ai.justSaved && !ai.dirty && (
          <Typography variant="body2" color="success.main" role="status">
            Saved.
          </Typography>
        )}
      </Box>
    </Box>
  );

  // The wizard supplies its own frame and explanation around the card.
  if (onboarding) return body;

  return (
    <Card variant="outlined">
      <CardContent sx={{ p: 3 }}>
        <Box sx={{ display: "flex", alignItems: "center", gap: 1.5, mb: 2 }}>
          <Bot size={22} />
          <Typography variant="h6" component="h2">
            {CARD_TITLE}
          </Typography>
          {saved.apiKey ? (
            <Chip label="ready" size="small" color="success" />
          ) : (
            <Chip label="no API key" size="small" color="warning" />
          )}
        </Box>
        <Divider sx={{ mb: 3 }} />
        {body}
      </CardContent>
    </Card>
  );
}

function Tile({ selected, children }: { selected: boolean; children: ReactNode }) {
  return (
    <Box
      sx={{
        border: 1,
        borderColor: selected ? "primary.main" : "divider",
        outline: selected ? 1 : 0,
        outlineColor: "primary.main",
        borderRadius: 1,
        p: 1.5,
      }}
    >
      {children}
    </Box>
  );
}

/**
 * The Claude subscription, inside the Claude Code tile because only Claude
 * Code can bill one. Keyed by the stored token, so a newly stored token closes
 * the replace field.
 */
function SubscriptionControl({ ai }: { ai: ReturnType<typeof useAiSettings> }) {
  const { saved, draft } = ai;
  const [replacing, setReplacing] = useState(false);
  const busy = ai.saving;
  const stored = saved.subscription;
  const askForToken = draft.billToSubscription && (stored === null || replacing);

  const serverError =
    ai.error?.field === "subscription" ? ai.error.message : undefined;
  const tokenError =
    serverError ??
    (draft.token.trim() !== "" || !ai.canAddSubscription ? ai.problem : undefined);

  return (
    <Box
      sx={{
        borderTop: 1,
        borderColor: "divider",
        borderTopStyle: "dashed",
        mt: 1.5,
        pt: 1.5,
        display: "flex",
        flexDirection: "column",
        gap: 1,
      }}
    >
      <FormControlLabel
        control={
          <Switch
            checked={draft.billToSubscription}
            disabled={busy || (!draft.billToSubscription && !ai.canAddSubscription)}
            onChange={(e) => {
              setReplacing(false);
              ai.change({ billToSubscription: e.target.checked, token: "" });
            }}
          />
        }
        label="Bill coding to a Claude subscription"
        sx={{ m: 0 }}
      />

      {!draft.billToSubscription && (
        <Typography variant="body2" color="text.secondary">
          {ai.canAddSubscription ? (
            <>
              Uses a token from <code>claude setup-token</code>. Other agents
              keep using the API key.
            </>
          ) : (
            "Add the Anthropic API key first."
          )}
        </Typography>
      )}

      {draft.billToSubscription && stored && !replacing && (
        <MaskedCredential stored={stored} onReplace={() => setReplacing(true)} disabled={busy} />
      )}
      {draft.billToSubscription && stored?.validationError && (
        <Alert severity="warning">{stored.validationError}</Alert>
      )}

      {askForToken && (
        <>
          <SecretField
            label={stored ? "New subscription token" : "Subscription token"}
            placeholder={`${SUBSCRIPTION_TOKEN_PREFIX}01-…`}
            value={draft.token}
            onChange={(token) => ai.change({ token })}
            disabled={busy}
            error={tokenError}
            helperText={
              <>
                Run <code>claude setup-token</code> and paste the token. Other
                agents keep using the API key.
              </>
            }
            noun="token"
          />
          {stored && (
            <Button
              size="small"
              sx={{ alignSelf: "flex-start" }}
              disabled={busy}
              onClick={() => {
                setReplacing(false);
                ai.change({ token: "" });
              }}
            >
              Keep the current token
            </Button>
          )}
        </>
      )}

      {ai.removesSubscription && (
        <Alert severity="warning">
          Saving deletes the stored Claude subscription token.
        </Alert>
      )}
    </Box>
  );
}
