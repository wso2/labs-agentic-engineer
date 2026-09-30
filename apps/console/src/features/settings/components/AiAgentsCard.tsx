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
  Radio,
  RadioGroup,
  Typography,
} from "@wso2/oxygen-ui";
import { Bot } from "@wso2/oxygen-ui-icons-react";
import type { components } from "../../../generated/aep-api";
import {
  SUBSCRIPTION_TOKEN_PREFIX,
  formatRuns,
  formatsRunning,
  lastChange,
  runtimeAvailable,
} from "../aiSettings";
import { useAiSettings } from "../hooks/useAiSettings";
import { MaskedCredential, SecretField } from "./CredentialField";
import { ModelConnectionRow } from "./ModelConnectionRow";
import { SreAgentModelRow } from "./SreAgentModelRow";

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
    description: "Open-source coding agent. Works with either API format.",
  },
];

/**
 * Who last changed the card, and when. The timestamp is optional on the wire,
 * and printing an Invalid Date would be worse than leaving it out.
 */
function changedLine(updatedAt: string | null, updatedBy: string | null): string {
  const who = updatedBy ? ` by ${updatedBy}` : "";
  const at = updatedAt ? new Date(updatedAt) : undefined;
  if (!at || Number.isNaN(at.getTime())) return `Changed${who}`;
  return `Changed ${at.toLocaleString()}${who}`;
}

/**
 * How the organization's agents run: the one model connection every agent
 * uses (format, URL, key, model), and the coding agent, with an optional
 * Claude subscription that only Claude Code on Anthropic's API can bill. One
 * Save sends whatever changed; `aiSettings.ts` decides which `/config`
 * sections that is.
 *
 * `onboarding` is the wizard's use of the same card: Save reads "Continue",
 * and the wizard advances once the save (which probes the connection) lands.
 * The wizard frames and explains the step itself, so the card drops its own
 * frame, header and footer there.
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
  const changed = lastChange(config);

  const body = (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
      <ModelConnectionRow
        // A newly saved connection closes the replace field.
        key={saved.connection?.updatedAt ?? "none"}
        ai={ai}
        onboarding={onboarding}
      />

      {!onboarding && <SreAgentModelRow config={config} />}

      <Box sx={{ borderTop: onboarding ? 0 : 1, borderColor: "divider", pt: onboarding ? 0 : 3 }}>
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
            const runsFormat = formatRuns(saved, draft, r.value);
            const reason = !available
              ? UNAVAILABLE_REASON
              : !runsFormat
                ? `Needs the ${formatsRunning(saved.formats, r.value).join(" or ")} format.`
                : r.description;
            return (
              <Tile key={r.value} selected={selected} muted={available && !runsFormat}>
                <FormControlLabel
                  value={r.value}
                  disabled={busy || !available || !runsFormat}
                  control={<Radio />}
                  label={
                    <Box>
                      <Typography variant="body1" fontWeight={600}>
                        {r.label}
                      </Typography>
                      <Typography variant="body2" color="text.secondary">
                        {reason}
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
                {r.value === "opencode" && selected && ai.removesSubscription && !ai.runtimeMoved && (
                  <Alert severity="warning" sx={{ mt: 1.5 }}>
                    Saving deletes the stored Claude subscription token.
                    OpenCode bills coding to the API key.
                  </Alert>
                )}
              </Tile>
            );
          })}
        </RadioGroup>
        {/* In onboarding nobody chose the default runtime, so there is no
            choice of theirs to say was moved. */}
        {ai.runtimeMoved && !onboarding && (
          <Alert severity="warning" sx={{ mt: 1.5 }} role="status">
            Coding moved to{" "}
            {RUNTIMES.find((r) => r.value === draft.runtime)?.label ?? draft.runtime}:{" "}
            {RUNTIMES.find((r) => r.value === saved.runtime)?.label ?? saved.runtime} speaks
            only the {formatsRunning(saved.formats, saved.runtime).join(" or ")} format.
            {ai.removesSubscription && " Saving deletes the stored Claude subscription token."}
          </Alert>
        )}
        {ai.error?.field === "runtime" && (
          <Alert severity="error" sx={{ mt: 1.5 }}>
            {ai.error.message}
          </Alert>
        )}
      </Box>

      {/* A missing field only holds Save back (the empty field says what is
          missing); a format no coding agent here can run needs saying. */}
      {ai.problem?.field === "connection" && (
        <Alert severity="warning">{ai.problem.message}</Alert>
      )}

      {ai.error?.field === "card" && (
        <Alert severity="error">{ai.error.message}</Alert>
      )}

      <Box sx={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 1.5 }}>
        <Button variant="contained" onClick={ai.save} disabled={!ai.canSave}>
          {busy ? "Saving…" : onboarding ? "Continue" : "Save"}
        </Button>
        {ai.dirty && !onboarding && (
          <Button onClick={ai.discard} disabled={busy}>
            Discard changes
          </Button>
        )}
        {ai.justSaved && !ai.dirty && (
          <Typography variant="body2" color="success.main" role="status">
            Saved. Agents use it from their next step; coding runs in flight keep theirs.
          </Typography>
        )}
        {!onboarding && (changed.at || changed.by) && (
          <Typography variant="body2" color="text.secondary" sx={{ ml: "auto" }}>
            {changedLine(changed.at, changed.by)}
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
          {saved.connection ? (
            <Chip label="ready" size="small" color="success" />
          ) : (
            <Chip label="not connected" size="small" color="warning" />
          )}
        </Box>
        <Divider sx={{ mb: 3 }} />
        {body}
      </CardContent>
    </Card>
  );
}

function Tile({
  selected,
  muted,
  children,
}: {
  selected: boolean;
  /** The tile cannot be chosen with the draft's format; it stays visible, dimmed. */
  muted: boolean;
  children: ReactNode;
}) {
  return (
    <Box
      sx={{
        border: 1,
        borderColor: selected ? "primary.main" : "divider",
        outline: selected ? 1 : 0,
        outlineColor: "primary.main",
        borderRadius: 1,
        p: 1.5,
        bgcolor: muted ? "action.hover" : undefined,
      }}
    >
      {children}
    </Box>
  );
}

/**
 * The Claude subscription, inside the Claude Code tile because only Claude
 * Code can bill one, and only on a connection that takes one
 * (`capabilities.claudeSubscription`). An optional token field, not a switch:
 * leaving it empty bills coding to the connection's key. Keyed by the stored
 * token, so a newly stored token closes the replace field.
 */
function SubscriptionControl({ ai }: { ai: ReturnType<typeof useAiSettings> }) {
  const { saved, draft } = ai;
  const [replacing, setReplacing] = useState(false);
  const busy = ai.saving;
  const stored = saved.subscription;

  const frame = {
    borderTop: 1,
    borderColor: "divider",
    borderTopStyle: "dashed",
    mt: 1.5,
    pt: 1.5,
    display: "flex",
    flexDirection: "column",
    gap: 1,
  } as const;

  if (!ai.subscriptionOffered) {
    // Only said once the draft's connection is known not to take one.
    if (!ai.view) return null;
    return (
      <Box sx={frame}>
        <Typography variant="body2" color="text.secondary">
          A Claude subscription token works only on Anthropic&apos;s own API.
        </Typography>
      </Box>
    );
  }

  const serverError = ai.error?.field === "subscription" ? ai.error.message : undefined;
  const tokenError =
    serverError ?? (ai.problem?.field === "subscription" ? ai.problem.message : undefined);
  const showStored = stored !== null && !replacing && !draft.removeToken;

  return (
    <Box sx={frame}>
      <Typography variant="body2" fontWeight={500}>
        Claude subscription token{" "}
        <Typography component="span" variant="body2" color="text.secondary">
          (optional)
        </Typography>
      </Typography>

      {showStored && (
        <>
          <MaskedCredential
            preview={`${stored.keyPrefix}••••••${stored.keyLast4}`}
            onReplace={() => setReplacing(true)}
            disabled={busy}
          >
            <Button
              size="small"
              color="error"
              disabled={busy}
              onClick={() => ai.change({ removeToken: true, token: "" })}
            >
              Remove
            </Button>
          </MaskedCredential>
          <Typography variant="body2" color="text.secondary">
            Coding bills your Claude plan. Other agents use the API key.
          </Typography>
          {stored.validationError && (
            <Alert severity="warning">{stored.validationError}</Alert>
          )}
        </>
      )}

      {!showStored && !draft.removeToken && (
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
                To bill coding to your Claude plan instead of the API key, run{" "}
                <code>claude setup-token</code> and paste the token. Leave empty
                to use the API key.
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

      {draft.removeToken && (
        <>
          <Typography variant="body2" color="warning.dark" role="status">
            Saving deletes the stored token; coding goes back to the API key.
          </Typography>
          <Button
            size="small"
            sx={{ alignSelf: "flex-start" }}
            disabled={busy}
            onClick={() => ai.change({ removeToken: false })}
          >
            Keep the current token
          </Button>
        </>
      )}
    </Box>
  );
}
