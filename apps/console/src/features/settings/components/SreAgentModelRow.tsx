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
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";
import { ApiRequestError } from "../../../api/errors";
import { hostOf, refusedField, type AiField } from "../aiSettings";
import { sreRowState, sreStatusLabel } from "../sreAgent";
import { useClearSreModel, useSaveSreModel } from "../api/queries";
import { MaskedCredential, SecretField } from "./CredentialField";

type ConfigProjection = components["schemas"]["ConfigProjection"];

/** The form's draft: a fresh or edited SRE model, not yet saved. */
interface Draft {
  baseURL: string;
  apiKey: string;
  model: string;
}

const EMPTY_DRAFT: Draft = { baseURL: "", apiKey: "", model: "" };

/**
 * The SRE agent's model, under the org's model connection: inherited from it
 * when the org connection can run the SRE agent, overridden by its own
 * OpenAI-compatible/Bearer connection, or unavailable with why. A save rides
 * its own PATCH (`sreLlm`), separate from the AI agents card's one Save, so
 * setting or clearing it takes effect on its own.
 *
 * Not rendered in onboarding: the wizard has no SRE agent to configure yet.
 */
export function SreAgentModelRow({ config }: { config: ConfigProjection }) {
  const [draft, setDraft] = useState<Draft | null>(null);
  const [removing, setRemoving] = useState(false);
  const saveSreModel = useSaveSreModel();
  const clearSreModel = useClearSreModel();

  if (config.sreAgent === null) return null;
  const sreAgent = config.sreAgent;
  const row = sreRowState(config);
  const status = sreStatusLabel(sreAgent.status, sreAgent.reason);

  const busy = saveSreModel.isPending;
  const saveError = saveSreModel.isError ? (saveSreModel.error as unknown) : null;
  const errorField: AiField | undefined =
    saveError instanceof ApiRequestError ? refusedField(saveError.fields, saveError.code) : undefined;
  const errorMessage = saveError instanceof ApiRequestError ? saveError.message : undefined;
  const fieldError = (name: "apiKey" | "baseURL" | "model") =>
    errorField === name ? errorMessage : undefined;
  const generalError = errorField === "connection" || errorField === "card" ? errorMessage : undefined;

  // A changed host needs its own key, as the org connection's does; the
  // stored key is never sent to another host.
  const keyRequired =
    config.sreLlm === null || (draft !== null && hostOf(draft.baseURL) !== hostOf(config.sreLlm.baseURL));
  const canSave =
    draft !== null &&
    !busy &&
    draft.baseURL.trim() !== "" &&
    draft.model.trim() !== "" &&
    (!keyRequired || draft.apiKey.trim() !== "");

  function openFresh() {
    saveSreModel.reset();
    setDraft({ ...EMPTY_DRAFT });
  }

  function openEdit() {
    if (config.sreLlm === null) return;
    saveSreModel.reset();
    setDraft({ baseURL: config.sreLlm.baseURL, apiKey: "", model: config.sreLlm.model });
  }

  function save() {
    if (!draft) return;
    const apiKey = draft.apiKey.trim();
    saveSreModel.mutate(
      { baseURL: draft.baseURL.trim(), model: draft.model.trim(), ...(apiKey !== "" ? { apiKey } : {}) },
      { onSuccess: () => setDraft(null) },
    );
  }

  function remove() {
    clearSreModel.mutate(undefined, { onSuccess: () => setRemoving(false) });
  }

  return (
    <Box
      sx={{
        display: "flex",
        flexDirection: "column",
        gap: 1.5,
        borderTop: 1,
        borderColor: "divider",
        pt: 3,
      }}
    >
      <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
        <Typography variant="subtitle2" component="h3">
          SRE agent model
        </Typography>
        <Chip
          label={status.text}
          size="small"
          color={status.severity === "success" ? "success" : status.severity === "error" ? "error" : "default"}
        />
      </Box>
      <Typography variant="body2" color="text.secondary">
        The model the OpenChoreo SRE agent uses for root-cause analysis.
      </Typography>

      {draft === null && row.kind === "inherited" && (
        <Box sx={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 1.5 }}>
          <Typography variant="body2">
            Uses the organization&apos;s model connection ({row.model} @ {row.host})
          </Typography>
          <Button size="small" onClick={openFresh}>
            Override
          </Button>
        </Box>
      )}

      {draft === null && row.kind === "override" && (
        <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
          <MaskedCredential preview={row.keyPreview} disabled={busy}>
            <Button size="small" onClick={openEdit} disabled={busy}>
              Edit
            </Button>
            <Button size="small" color="error" onClick={() => setRemoving(true)} disabled={busy}>
              Remove
            </Button>
          </MaskedCredential>
          <Typography variant="body2" color="text.secondary">
            {row.model} @ {row.host}
          </Typography>
        </Box>
      )}

      {draft === null && row.kind === "unavailable" && (
        <Box sx={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 1.5 }}>
          <Typography variant="body2" color="text.secondary">
            {row.reason}
          </Typography>
          <Button size="small" variant="outlined" onClick={openFresh}>
            Set SRE model
          </Button>
        </Box>
      )}

      {draft !== null && (
        <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
          <TextField
            label="Base URL"
            type="url"
            placeholder="https://api.openai.com/v1"
            value={draft.baseURL}
            onChange={(e) => setDraft({ ...draft, baseURL: e.target.value })}
            disabled={busy}
            error={fieldError("baseURL") !== undefined}
            helperText={fieldError("baseURL")}
            fullWidth
            slotProps={{ htmlInput: { spellCheck: false, style: { fontFamily: "monospace" } } }}
          />
          <SecretField
            label="API key"
            placeholder="Your provider's API key"
            value={draft.apiKey}
            onChange={(apiKey) => setDraft({ ...draft, apiKey })}
            disabled={busy}
            error={fieldError("apiKey")}
            helperText={
              keyRequired
                ? "Checked against the endpoint when you save."
                : "Leave empty to keep the current key."
            }
            noun="key"
          />
          <TextField
            label="Model"
            value={draft.model}
            onChange={(e) => setDraft({ ...draft, model: e.target.value })}
            disabled={busy}
            error={fieldError("model") !== undefined}
            helperText={fieldError("model")}
            fullWidth
            slotProps={{ htmlInput: { spellCheck: false, style: { fontFamily: "monospace" } } }}
          />
          {generalError && <Alert severity="error">{generalError}</Alert>}
          <Box sx={{ display: "flex", gap: 1.5 }}>
            <Button variant="contained" onClick={save} disabled={!canSave}>
              {busy ? "Saving…" : "Save"}
            </Button>
            <Button
              onClick={() => {
                saveSreModel.reset();
                setDraft(null);
              }}
              disabled={busy}
            >
              Cancel
            </Button>
          </Box>
        </Box>
      )}

      <Dialog open={removing} onClose={() => setRemoving(false)} maxWidth="xs" fullWidth>
        <DialogTitle>Remove the SRE model?</DialogTitle>
        <DialogContent>
          <DialogContentText>
            The SRE agent falls back to the organization&apos;s model connection when it can run on it,
            and is otherwise left without a model.
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setRemoving(false)}>Cancel</Button>
          <Button color="error" variant="contained" onClick={remove} disabled={clearSreModel.isPending}>
            Remove
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}
