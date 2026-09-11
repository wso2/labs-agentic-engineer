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

import { useEffect, useState } from "react";
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
  Divider,
  FormControlLabel,
  IconButton,
  InputAdornment,
  Radio,
  RadioGroup,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import { Eye, EyeOff } from "@wso2/oxygen-ui-icons-react";
import type { components } from "../../../generated/aep-api";
import {
  useConnectCodingAnthropic,
  useRemoveCodingAnthropic,
} from "../api/queries";

type LLMProjection = components["schemas"]["LLMProjection"];

/**
 * The coding agent's key: an OVERRIDE on the organization's Anthropic key,
 * rendered inside the Coding agent card because it is a coding-agent setting
 * (ADR-0016) — it only means anything relative to the org key it overrides,
 * which is why every label names that key rather than pointing at a position.
 *
 * "Reuse" is the ABSENCE of a coding key, not a stored setting — so the radio is
 * local state and nothing is written until a button is pressed. That matches the
 * rest of the card (writes are explicit) and, more importantly, means a stray
 * click on "Reuse" cannot discard a secret: removing a key that exists is routed
 * through a confirm, because although nothing breaks (coding runs simply fall
 * back to the key above), the key itself cannot be read back and would have to
 * be re-fetched from Anthropic.
 */
export function CodingAgentKeySection({
  codingLlm,
}: {
  codingLlm: LLMProjection | null;
}) {
  const hasKey = codingLlm !== null;
  const [wantsSeparate, setWantsSeparate] = useState(hasKey);
  const [apiKey, setApiKey] = useState("");
  const [showKey, setShowKey] = useState(false);
  const [removeOpen, setRemoveOpen] = useState(false);

  const save = useConnectCodingAnthropic();
  const remove = useRemoveCodingAnthropic();

  // Follow the server after a write lands: saving selects "separate", removing
  // falls back to "reuse". Without this the radio would keep showing whatever
  // was clicked last, even if the mutation was rejected.
  useEffect(() => setWantsSeparate(hasKey), [hasKey]);

  const chooseMode = (value: string) => {
    if (value === "separate") {
      setWantsSeparate(true);
      return;
    }
    // Flipping back to reuse discards a stored key — confirm first, and leave
    // the radio where it is until the removal actually succeeds.
    if (hasKey) {
      setRemoveOpen(true);
      return;
    }
    setWantsSeparate(false);
  };

  const submit = () => {
    save.mutate(apiKey, { onSuccess: () => setApiKey("") });
  };

  const confirmRemove = () => {
    remove.mutate(undefined, { onSuccess: () => setRemoveOpen(false) });
  };

  return (
    <>
      <Divider sx={{ my: 3 }} />

      <Typography variant="subtitle2" sx={{ mb: 0.5 }}>
        Coding agent key
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
        Bill the coding agent to a separate credential — either another
        Anthropic API key, or a token from <code>claude setup-token</code> to
        bill a Claude subscription instead of API credits. Everything else —
        requirements, architecture, and task generation — keeps using the
        organization&apos;s Anthropic key either way.
      </Typography>

      <RadioGroup
        value={wantsSeparate ? "separate" : "reuse"}
        onChange={(e) => chooseMode(e.target.value)}
      >
        <FormControlLabel
          value="reuse"
          control={<Radio />}
          label="Reuse the organization's Anthropic key"
        />
        <FormControlLabel
          value="separate"
          control={<Radio />}
          label="Use a separate key"
        />
      </RadioGroup>

      {wantsSeparate && (
        <Box sx={{ display: "flex", flexDirection: "column", gap: 2, mt: 2 }}>
          {codingLlm && (
            <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
              <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                <Typography variant="body2" fontFamily="monospace">
                  {codingLlm.keyPrefix}•••••••••{codingLlm.keyLast4}
                </Typography>
                {/* Which of the two it is decides what gets billed, so it is
                    stated rather than left to be inferred from the prefix. */}
                <Chip
                  size="small"
                  variant="outlined"
                  label={
                    codingLlm.credentialKind === "oauth_token"
                      ? "Claude subscription"
                      : "API key"
                  }
                />
              </Box>
              <Typography variant="body2" color="text.secondary">
                Connected {new Date(codingLlm.connectedAt).toLocaleString()}
              </Typography>
              {codingLlm.validationError && (
                <Alert severity="warning">{codingLlm.validationError}</Alert>
              )}
            </Box>
          )}

          <TextField
            label={codingLlm ? "Replace coding agent key" : "Coding agent key or token"}
            placeholder="sk-ant-..."
            type={showKey ? "text" : "password"}
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            fullWidth
            slotProps={{
              input: {
                endAdornment: (
                  <InputAdornment position="end">
                    <IconButton
                      aria-label={
                        showKey
                          ? "hide coding agent key"
                          : "show coding agent key"
                      }
                      onClick={() => setShowKey((v) => !v)}
                      edge="end"
                    >
                      {showKey ? <EyeOff size={18} /> : <Eye size={18} />}
                    </IconButton>
                  </InputAdornment>
                ),
              },
            }}
          />

          {save.isError && <Alert severity="error">{save.error.message}</Alert>}

          <Button
            variant="contained"
            onClick={submit}
            disabled={!apiKey || save.isPending}
            sx={{ alignSelf: "flex-start" }}
          >
            {save.isPending
              ? "Validating…"
              : codingLlm
                ? "Replace key"
                : "Save key"}
          </Button>
        </Box>
      )}

      <Dialog
        open={removeOpen}
        onClose={() => setRemoveOpen(false)}
        maxWidth="xs"
        fullWidth
      >
        <DialogTitle>Remove the coding agent key?</DialogTitle>
        <DialogContent>
          <DialogContentText>
            The coding agent will go back to using the organization&apos;s
            Anthropic key. This key cannot be recovered — you would need a new
            one from the Anthropic console.
          </DialogContentText>
          {remove.isError && (
            <Alert severity="error" sx={{ mt: 2 }}>
              {remove.error.message}
            </Alert>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setRemoveOpen(false)}>Cancel</Button>
          <Button
            color="error"
            variant="contained"
            onClick={confirmRemove}
            disabled={remove.isPending}
          >
            Remove
          </Button>
        </DialogActions>
      </Dialog>
    </>
  );
}
