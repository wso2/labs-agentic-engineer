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
  Card,
  CardContent,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Divider,
  IconButton,
  InputAdornment,
  TextField,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import { ExternalLink, Eye, EyeOff, Key } from "@wso2/oxygen-ui-icons-react";
import { useHasPermission } from "../../../auth/permissions";
import type { components } from "../../../generated/aep-api";
import { useConnectAnthropic, useDisconnectAnthropic } from "../api/queries";
import { CodingAgentKeySection } from "./CodingAgentKeySection";

type LLMProjection = components["schemas"]["LLMProjection"];

const NO_PERMISSION_TOOLTIP =
  "You don't have permission to configure the Anthropic key.";

export function AnthropicCredentialCard({
  llm,
  codingLlm,
}: {
  llm: LLMProjection | null;
  codingLlm: LLMProjection | null;
}) {
  const hasModelConfig = useHasPermission("ae:model-config");
  const [apiKey, setApiKey] = useState("");
  const [showKey, setShowKey] = useState(false);
  const [disconnectOpen, setDisconnectOpen] = useState(false);

  const connect = useConnectAnthropic();
  const disconnect = useDisconnectAnthropic();

  const connected = llm !== null;

  const submit = () => {
    connect.mutate(apiKey, {
      onSuccess: () => setApiKey(""),
    });
  };

  const confirmDisconnect = () => {
    disconnect.mutate(undefined, {
      onSuccess: () => setDisconnectOpen(false),
    });
  };

  return (
    <Card variant="outlined">
      <CardContent sx={{ p: 3 }}>
        <Box sx={{ display: "flex", alignItems: "center", gap: 1.5, mb: 2 }}>
          <Key size={22} />
          <Typography variant="h6">Anthropic</Typography>
          {connected ? (
            <Chip label={llm.status} size="small" color="success" />
          ) : (
            <Chip label="not connected" size="small" color="warning" />
          )}
        </Box>
        <Divider sx={{ mb: 3 }} />

        {!hasModelConfig && (
          <Alert severity="warning" sx={{ mb: 3 }}>
            You don't have permission to change these settings.
          </Alert>
        )}

        <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
          Requirements, architecture, and task generation run on this key, and
          so does the coding agent unless you give it one of its own below.
          There is no platform-provided fallback, so agents cannot run until a
          key is configured here.
        </Typography>

        {connected && (
          <Box sx={{ display: "flex", flexDirection: "column", gap: 1, mb: 3 }}>
            <Typography variant="body2" fontFamily="monospace">
              {llm.keyPrefix}•••••••••{llm.keyLast4}
            </Typography>
            <Typography variant="body2" color="text.secondary">
              Connected {new Date(llm.connectedAt).toLocaleString()}
              {llm.lastValidatedAt &&
                ` · last validated ${new Date(llm.lastValidatedAt).toLocaleString()}`}
            </Typography>
            {llm.validationError && (
              <Alert severity="warning">{llm.validationError}</Alert>
            )}
          </Box>
        )}

        <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
          <TextField
            label={connected ? "Replace API key" : "API key"}
            placeholder="sk-ant-..."
            type={showKey ? "text" : "password"}
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            fullWidth
            disabled={!hasModelConfig}
            slotProps={{
              input: {
                endAdornment: (
                  <InputAdornment position="end">
                    <IconButton
                      aria-label={showKey ? "hide key" : "show key"}
                      onClick={() => setShowKey((v) => !v)}
                      edge="end"
                      disabled={!hasModelConfig}
                    >
                      {showKey ? <EyeOff size={18} /> : <Eye size={18} />}
                    </IconButton>
                  </InputAdornment>
                ),
              },
            }}
          />
          <Button
            variant="text"
            size="small"
            href="https://console.anthropic.com/settings/keys"
            target="_blank"
            rel="noreferrer"
            endIcon={<ExternalLink size={14} />}
            sx={{ alignSelf: "flex-start" }}
          >
            Get an API key
          </Button>
          {connect.isError && (
            <Alert severity="error">{connect.error.message}</Alert>
          )}
          <Box
            sx={{
              display: "flex",
              flexWrap: "wrap",
              gap: 1,
              justifyContent: "space-between",
            }}
          >
            <Tooltip title={hasModelConfig ? "" : NO_PERMISSION_TOOLTIP}>
              <span>
                <Button
                  variant="contained"
                  onClick={submit}
                  disabled={!apiKey || connect.isPending || !hasModelConfig}
                >
                  {connect.isPending
                    ? "Validating…"
                    : connected
                      ? "Replace key"
                      : "Connect"}
                </Button>
              </span>
            </Tooltip>
            {connected && (
              <Tooltip title={hasModelConfig ? "" : NO_PERMISSION_TOOLTIP}>
                <span>
                  <Button
                    color="error"
                    variant="outlined"
                    onClick={() => setDisconnectOpen(true)}
                    disabled={!hasModelConfig}
                  >
                    Disconnect
                  </Button>
                </span>
              </Tooltip>
            )}
          </Box>
        </Box>

        {/* The coding-agent key overrides the key above, so it is only
            offered once there is a key to override — the server rejects it
            otherwise. */}
        {connected && <CodingAgentKeySection codingLlm={codingLlm} />}
      </CardContent>

      <Dialog
        open={disconnectOpen}
        onClose={() => setDisconnectOpen(false)}
        maxWidth="xs"
        fullWidth
      >
        <DialogTitle>Disconnect Anthropic?</DialogTitle>
        <DialogContent>
          <DialogContentText>
            The platform and coding agents will lose access to Claude until a
            new key is connected.
          </DialogContentText>
          {disconnect.isError && (
            <Alert severity="error" sx={{ mt: 2 }}>
              {disconnect.error.message}
            </Alert>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDisconnectOpen(false)}>Cancel</Button>
          <Tooltip title={hasModelConfig ? "" : NO_PERMISSION_TOOLTIP}>
            <span>
              <Button
                color="error"
                variant="contained"
                onClick={confirmDisconnect}
                disabled={disconnect.isPending || !hasModelConfig}
              >
                Disconnect
              </Button>
            </span>
          </Tooltip>
        </DialogActions>
      </Dialog>
    </Card>
  );
}
