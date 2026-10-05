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

import { useState } from "react";
import {
  Alert,
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from "@wso2/oxygen-ui";
import { Check } from "@wso2/oxygen-ui-icons-react";
import {
  FORMAT_LABELS,
  checkStatus,
  formatOption,
  hostOf,
  infoLines,
  untestedInfoLines,
  type InfoLine,
  type LLMFormat,
} from "../aiSettings";
import type { useAiSettings } from "../hooks/useAiSettings";
import { CredentialField, SecretField } from "./CredentialField";

type AiSettingsState = ReturnType<typeof useAiSettings>;

/**
 * The org's model connection, the first section of the AI agents card: the
 * API format, base URL, key and model, Test connection, and what the
 * connection supports.
 *
 * Every field is part of the card's draft and goes out with the card's one
 * Save; the server probes the connection before writing anything, and a
 * refusal comes back here, on the field it names. Test connection runs the
 * same probe without saving. Disconnecting is the exception: it is
 * destructive, so it is confirmed and sent on its own.
 *
 * `onboarding` stacks the fields in one column and drops the section's own
 * heading, since the wizard explains the step above.
 */
export function ModelConnectionRow({
  ai,
  onboarding,
}: {
  ai: AiSettingsState;
  onboarding: boolean;
}) {
  const { saved, draft } = ai;
  const busy = ai.saving;
  const stored = saved.connection;
  const host = hostOf(draft.baseURL);
  const [replacing, setReplacing] = useState(false);
  const [disconnectOpen, setDisconnectOpen] = useState(false);

  const fieldError = (field: "apiKey" | "baseURL" | "model") =>
    ai.error?.field === field ? ai.error.message : undefined;

  const askForKey = ai.keyRequired || replacing;
  const keyHelp =
    stored !== null && ai.keyRequired && host
      ? `A new host needs its own key: the saved key is never sent to ${host}.`
      : "Checked against the endpoint when you test or save.";

  const defaults = formatOption(saved.formats, draft.kind);
  const note = FORMAT_LABELS[draft.kind].modelNote;
  const modelHelp = defaults
    ? `Default for this format: ${defaults.defaultModel}${note ? ` (${note})` : ""}. Use the exact model ID your provider documents.`
    : "Use the exact model ID your provider documents.";

  const lines = ai.view
    ? infoLines(ai.view.capabilities, ai.view.priced, ai.view.model, ai.view.host)
    : untestedInfoLines(host);

  const canTest =
    !busy &&
    !ai.testing &&
    draft.baseURL.trim() !== "" &&
    draft.model.trim() !== "" &&
    (!ai.keyRequired || draft.apiKey.trim() !== "");

  const columns = onboarding ? "1fr" : { xs: "1fr", md: "1fr 1fr" };

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
      {!onboarding && (
        <Box>
          <Typography variant="subtitle2" component="h3">
            Model connection
          </Typography>
          <Typography variant="body2" color="text.secondary">
            Every agent uses this connection and model: requirements, design,
            task planning and coding.
          </Typography>
        </Box>
      )}

      <Box sx={{ display: "grid", gridTemplateColumns: columns, gap: 2, alignItems: "start" }}>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 0.75 }}>
          <Typography variant="body2" fontWeight={500} id="llm-format-label">
            API format
          </Typography>
          <ToggleButtonGroup
            exclusive
            fullWidth
            aria-labelledby="llm-format-label"
            value={draft.kind}
            disabled={busy}
            onChange={(_, kind: LLMFormat | null) => {
              if (kind) ai.chooseFormat(kind);
            }}
          >
            {saved.formats.map((f) => (
              <ToggleButton key={f.kind} value={f.kind} sx={{ textTransform: "none" }}>
                {FORMAT_LABELS[f.kind].label}
              </ToggleButton>
            ))}
          </ToggleButtonGroup>
        </Box>

        <TextField
          label="Base URL"
          type="url"
          placeholder="https://ollama.com/v1"
          value={draft.baseURL}
          onChange={(e) => ai.change({ baseURL: e.target.value })}
          disabled={busy}
          error={fieldError("baseURL") !== undefined}
          helperText={fieldError("baseURL")}
          fullWidth
          slotProps={{ htmlInput: { spellCheck: false, style: { fontFamily: "monospace" } } }}
        />

        <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
          {!askForKey && stored ? (
            <>
              <CredentialField
                label="API key"
                set
                onReplace={() => setReplacing(true)}
                disabled={busy}
              >
                <Button
                  size="small"
                  color="error"
                  onClick={() => setDisconnectOpen(true)}
                  disabled={busy}
                >
                  Disconnect
                </Button>
              </CredentialField>
              <Typography variant="body2" color="text.secondary">
                Connected {new Date(stored.connectedAt).toLocaleString()}
              </Typography>
              {fieldError("apiKey") && (
                <Alert severity="error">{fieldError("apiKey")}</Alert>
              )}
            </>
          ) : (
            <>
              <SecretField
                label={replacing && !ai.keyRequired ? "New API key" : "API key"}
                placeholder="Your provider's API key"
                value={draft.apiKey}
                onChange={(apiKey) => ai.change({ apiKey })}
                disabled={busy}
                error={fieldError("apiKey")}
                helperText={keyHelp}
                noun="key"
              />
              {replacing && !ai.keyRequired && (
                <Button
                  size="small"
                  sx={{ alignSelf: "flex-start" }}
                  disabled={busy}
                  onClick={() => {
                    ai.change({ apiKey: "" });
                    setReplacing(false);
                  }}
                >
                  Keep the current key
                </Button>
              )}
            </>
          )}
        </Box>

        <TextField
          label="Model"
          value={draft.model}
          onChange={(e) => ai.change({ model: e.target.value })}
          disabled={busy}
          error={fieldError("model") !== undefined}
          helperText={fieldError("model") ?? modelHelp}
          fullWidth
          slotProps={{ htmlInput: { spellCheck: false, style: { fontFamily: "monospace" } } }}
        />
      </Box>

      <Box sx={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 1.5 }}>
        <Button variant="outlined" onClick={ai.testConnection} disabled={!canTest}>
          {ai.testing ? "Testing…" : "Test connection"}
        </Button>
        <TestStatus ai={ai} />
      </Box>

      <InfoBox lines={lines} />

      <Dialog
        open={disconnectOpen}
        onClose={() => setDisconnectOpen(false)}
        maxWidth="xs"
        fullWidth
      >
        <DialogTitle>Disconnect the model connection?</DialogTitle>
        <DialogContent>
          <DialogContentText>
            Every agent stops until a new connection is saved.
            {/* The server removes the subscription with the connection it depends on. */}
            {saved.subscription !== null &&
              " The stored Claude subscription token is deleted as well."}
          </DialogContentText>
          {ai.disconnectError && (
            <Alert severity="error" sx={{ mt: 2 }}>
              {ai.disconnectError}
            </Alert>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDisconnectOpen(false)}>Cancel</Button>
          <Button
            color="error"
            variant="contained"
            onClick={() => ai.disconnect(() => setDisconnectOpen(false))}
            disabled={ai.disconnecting}
          >
            Disconnect
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}

/**
 * Beside Test connection: what the latest probe found, a refusal that is not
 * about one field, or "Not connected" when the refusal sits on a field.
 */
function TestStatus({ ai }: { ai: AiSettingsState }) {
  const error = ai.error;
  if (error?.field === "connection") {
    return (
      <Typography variant="body2" color="error.main" role="alert">
        {error.message}
      </Typography>
    );
  }
  if (error && (error.field === "apiKey" || error.field === "baseURL" || error.field === "model")) {
    return (
      <Typography variant="body2" color="error.main">
        Not connected
      </Typography>
    );
  }
  if (!ai.check) return null;
  const status = checkStatus(ai.check);
  return (
    <Typography
      variant="body2"
      role="status"
      color={status.severity === "success" ? "success.main" : "warning.dark"}
      sx={{ display: "flex", alignItems: "center", gap: 0.75 }}
    >
      {status.severity === "success" && <Check size={16} aria-hidden />}
      {status.text}
    </Typography>
  );
}

function InfoBox({ lines }: { lines: InfoLine[] }) {
  return (
    <Box
      component="ul"
      aria-label="What this connection means"
      sx={{
        m: 0,
        px: 2,
        py: 1.5,
        borderRadius: 1,
        bgcolor: "action.hover",
        display: "flex",
        flexDirection: "column",
        gap: 1,
        listStyle: "none",
      }}
    >
      {lines.map((line) => (
        <Typography key={line.text} component="li" variant="body2" sx={{ display: "flex", gap: 1 }}>
          <span aria-hidden>·</span>
          <span>
            <Emphasized line={line} />
          </span>
        </Typography>
      ))}
    </Box>
  );
}

function Emphasized({ line }: { line: InfoLine }) {
  const at = line.strong ? line.text.indexOf(line.strong) : -1;
  if (!line.strong || at < 0) return <>{line.text}</>;
  return (
    <>
      {line.text.slice(0, at)}
      <strong>{line.strong}</strong>
      {line.text.slice(at + line.strong.length)}
    </>
  );
}
